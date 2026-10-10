// The edge in front of Cloud Run. Each hostname maps to a service's *.run.app origin (ORIGINS);
// the request is forwarded as is (WebSocket upgrades included), plus:
//   X-Client-IP      the visitor's address, which Cloudflare knows and the origin otherwise wouldn't
//   X-Forwarded-Host the hostname the visitor used
//   X-Edge-Secret    proof the request came through here. Each host has its own (EDGE_SECRETS),
//                    so one service can't use what it receives to pass as the edge to another.
// Headers a visitor sends with those names are replaced, never passed through.
//
// Nothing a visitor sends reaches a path under /internal/: those are for the edge's own scheduled
// calls (CRON: host → paths), which give each service a clock that runs while it is scaled to
// zero. A timer inside the service doesn't.
//
// It also keeps a copy of Lighthouse's public pages (PAGE_HOSTS). A copy less than a minute old
// is served without waking the origin; when the origin fails or can't be reached, the last copy
// (up to a day old) is served instead, marked as saved. Nothing personal is ever kept: only
// anonymous GET requests for the public pages, and only answers that are 200 and set no cookie.

const STATIC = /\.(?:js|mjs|css|map|png|jpe?g|gif|webp|avif|svg|ico|woff2?|ttf)$/i;
const PAGES = /^\/(?:|projects(?:\/[a-z0-9-]+)?|about|status|privacy|robots\.txt|sitemap\.xml|api\/status)$/;
const FRESH_MS = 60_000; // how long a copy is served without asking the origin
const KEEP_SECONDS = 86_400; // how long a copy is kept for when the origin fails
const HOUR = 3600, YEAR = 31_536_000;

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    const origins = JSON.parse(env.ORIGINS);
    const origin = origins[url.hostname];
    if (!origin || url.pathname.startsWith("/internal/")) return new Response("Not found", { status: 404 });

    // The site's assistant is a separate service (on AWS). Its one path is sent there, with that
    // service's own secret and the visitor's address, and nothing else of the request: no cookies,
    // so a signed-in session never leaves this site.
    // It can follow a visitor into a demo (guide.demos, by host). The demo's own code doesn't
    // change: its page leaves here with one script tag added (see withGuide), the script and its
    // styles are served from this site under /_guide/, and questions go to /_guide/ask. To the
    // demo's content security policy all of that is the demo's own origin.
    const guide = JSON.parse(env.GUIDE ?? "null");
    if (guide && url.hostname === guide.host && url.pathname === guide.path) return toGuide(request, guide, env);
    const demo = guide?.demos?.[url.hostname]; // { name, viewing }
    if (demo && url.pathname.startsWith("/_guide/")) {
      if (url.pathname === "/_guide/ask") return toGuide(request, guide, env);
      const file = GUIDE_FILES[url.pathname];
      if (!file || (request.method !== "GET" && request.method !== "HEAD")) return new Response("Not found", { status: 404 });
      return fetch(new URL(file, origins[guide.host]), { method: request.method, cf: { cacheEverything: true, cacheTtlByStatus: { "200-299": HOUR, "400-599": 0 } } });
    }

    const target = new URL(url.pathname + url.search, origin);
    const headers = new Headers(request.headers);
    headers.set("X-Client-IP", request.headers.get("CF-Connecting-IP") ?? "");
    headers.set("X-Forwarded-Host", url.hostname);
    headers.set("X-Forwarded-Proto", "https");
    headers.delete("X-Edge-Secret");
    const secret = JSON.parse(env.EDGE_SECRETS)[url.hostname];
    if (secret) headers.set("X-Edge-Secret", secret);

    const toOrigin = (extra = {}) => fetch(target, {
      method: request.method,
      headers,
      body: request.body,
      redirect: "manual", // redirects go back to the browser, which follows them via the edge
      ...extra,
    });

    if (isPublicPage(request, url, env)) return asSent(await page(url, toOrigin, ctx));

    // Static files are cached at the edge, so repeat downloads never reach Google (whose free
    // tier includes only 1 GB a month of outbound data): for a year when the address carries a
    // fingerprint of the contents (?v=) or is a font, otherwise for an hour. Everything else,
    // including every API call and WebSocket, goes to the origin every time.
    if ((request.method === "GET" || request.method === "HEAD") && STATIC.test(url.pathname)) {
      const ttl = url.searchParams.has("v") || url.pathname.includes("/fonts/") ? YEAR : HOUR;
      return toOrigin({ cf: { cacheEverything: true, cacheTtlByStatus: { "200-299": ttl, "400-599": 0 } } });
    }
    const answer = await toOrigin();
    return asSent(demo && request.method === "GET" ? withGuide(answer, guide, demo) : answer);
  },

  // The scheduled calls. One failing doesn't stop the others; a failure is logged and the next
  // hour tries again.
  async scheduled(event, env, ctx) {
    const origins = JSON.parse(env.ORIGINS);
    const secrets = JSON.parse(env.EDGE_SECRETS);
    const calls = Object.entries(JSON.parse(env.CRON ?? "{}")).flatMap(([host, paths]) => paths.map(async (path) => {
      try {
        const res = await fetch(new URL(path, origins[host]), { method: "POST", headers: { "X-Edge-Secret": secrets[host] ?? "" } });
        if (!res.ok) console.error(`${host}${path}: HTTP ${res.status}`);
      } catch (err) {
        console.error(`${host}${path}: ${err.message}`);
      }
    }));
    ctx.waitUntil(Promise.all(calls));
    await Promise.all(calls);
  },
};

const GUIDE_MAX_BYTES = 8_000;

// A demo's page, with the assistant's script added at the end of its body. The script does nothing
// unless the visitor has the assistant switched on, and never runs inside a frame.
function withGuide(response, guide, demo) {
  if (!(response.headers.get("Content-Type") ?? "").startsWith("text/html") || typeof HTMLRewriter === "undefined") return response;
  const attr = (v) => String(v ?? "").replace(/[^A-Za-z0-9 ./#:-]/g, "");
  const tag = `<script src="/_guide/guide.js" data-css="/_guide/guide.css" data-site="https://${attr(guide.host)}" data-here="${attr(demo.name)}" data-viewing="${attr(demo.viewing)}" defer></script>`;
  return new HTMLRewriter().on("body", { element(body) { body.append(tag, { html: true }); } }).transform(response);
}

const GUIDE_FILES = { "/_guide/guide.js": "/static/guide.js", "/_guide/guide.css": "/static/guide.css" };
const RESTING = { kind: "limit", limit: "unavailable", text: "I can't answer right now. The project pages have everything I'd draw on.", sources: [], further_reading: null };

async function toGuide(request, guide, env) {
  const json = (status, body) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", "Cache-Control": "no-store" } });
  if (request.method !== "POST") return json(405, { error: "POST only" });
  // Only this site's own pages may ask: a browser on another site sends that site's Origin.
  // (The path was matched on one of the assistant's hosts, so that host's own pages are the ones meant.)
  if (request.headers.get("Origin") !== `https://${new URL(request.url).hostname}`) return json(403, { error: "not from this site" });
  const body = await request.text();
  if (body.length > GUIDE_MAX_BYTES) return json(413, { error: "too large" });
  try {
    // The service's address may carry a path of its own (an API Gateway stage), which is kept.
    const answer = await fetch(guide.origin.replace(/\/+$/, "") + guide.path, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Edge-Secret": JSON.parse(env.EDGE_SECRETS).guide ?? "",
        "X-Client-IP": request.headers.get("CF-Connecting-IP") ?? "",
      },
      body,
    });
    // Switched off, throttled, blocked by its firewall or failing: to the visitor it is resting.
    // (A 4xx is the service's own verdict on the request, and is passed on.)
    if (answer.status === 429 || answer.status === 403 || answer.status >= 500) return json(200, RESTING);
    return json(answer.status, await answer.json());
  } catch {
    // The assistant being away must never look like the site being broken.
    return json(200, RESTING);
  }
}

// Pages leave the edge as the origin wrote them. Cloudflare otherwise edits HTML on its way out
// (it was inserting its own analytics script into every page), and "no-transform" is the standard
// way to refuse that. The sites promise visitors what is collected and by whom, and GhostChat
// publishes a hash of every file it serves; neither holds if a third party rewrites the page.
function asSent(response) {
  if (!(response.headers.get("Content-Type") ?? "").includes("text/html")) return response; // WebSocket upgrades included
  const cacheControl = response.headers.get("Cache-Control") ?? "";
  if (/\bno-transform\b/.test(cacheControl)) return response;
  const out = new Response(response.body, response);
  out.headers.set("Cache-Control", cacheControl ? `${cacheControl}, no-transform` : "no-transform");
  return out;
}

// A public page asked for anonymously: no session cookie, so nothing in the answer is anyone's.
function isPublicPage(request, url, env) {
  if (request.method !== "GET" || !PAGES.test(url.pathname)) return false;
  if (!JSON.parse(env.PAGE_HOSTS ?? "[]").includes(url.hostname)) return false;
  return !/lh_session=/.test(request.headers.get("Cookie") ?? "");
}

async function page(url, toOrigin, ctx) {
  const cache = caches.default;
  // One copy per page, whatever the query string: a ?ref= tag is read by the page's script, and
  // doesn't change what the server sends.
  const key = new Request(url.origin + url.pathname, { method: "GET" });
  const saved = await cache.match(key);
  const savedAt = saved ? Number(saved.headers.get("X-Edge-Saved-At")) : 0;
  if (saved && Date.now() - savedAt < FRESH_MS) return fromCopy(saved, "hit");

  let answer = null;
  try {
    answer = await toOrigin();
  } catch {
    // Unreachable: treated like a failure below.
  }
  if (answer && answer.status === 200 && !answer.headers.has("Set-Cookie")) {
    const copy = new Response(answer.clone().body, answer);
    copy.headers.set("X-Edge-Origin-Cache-Control", answer.headers.get("Cache-Control") ?? "");
    copy.headers.set("X-Edge-Saved-At", String(Date.now()));
    copy.headers.set("Cache-Control", `public, max-age=${KEEP_SECONDS}`); // how long the edge keeps it
    ctx.waitUntil(cache.put(key, copy));
    const out = new Response(answer.body, answer);
    out.headers.set("X-Edge-Cache", "miss");
    return out;
  }
  if (saved && (!answer || answer.status >= 500)) return stale(saved, savedAt);
  return answer ?? new Response("The site isn't responding right now.", { status: 502, headers: { "Content-Type": "text/plain; charset=utf-8" } });
}

// A copy served in place of the origin's answer, with the origin's own caching instructions.
function fromCopy(saved, how) {
  const out = new Response(saved.body, saved);
  restore(out.headers, saved.headers.get("X-Edge-Origin-Cache-Control"));
  out.headers.set("X-Edge-Cache", how);
  return out;
}

// The last copy, when the origin has failed: said plainly on the page itself, and not to be kept
// by anyone downstream.
async function stale(saved, savedAt) {
  const when = new Date(savedAt).toISOString().slice(0, 16).replace("T", " ") + " UTC";
  const type = saved.headers.get("Content-Type") ?? "";
  let body = saved.body;
  if (type.includes("text/html")) {
    const note = `<p class="edge-stale" role="status">The site isn't responding right now. This is a copy saved at ${when}; the figures on it may be out of date.</p>`;
    body = (await saved.text()).replace("<body>", "<body>\n" + note);
  }
  const out = new Response(body, saved);
  restore(out.headers, "no-store");
  out.headers.set("X-Edge-Cache", "stale");
  out.headers.set("X-Edge-Saved", when);
  return out;
}

function restore(headers, cacheControl) {
  headers.delete("X-Edge-Origin-Cache-Control");
  headers.delete("X-Edge-Saved-At");
  if (cacheControl) headers.set("Cache-Control", cacheControl);
  else headers.delete("Cache-Control");
}
