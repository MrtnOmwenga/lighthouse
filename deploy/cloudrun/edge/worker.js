// The edge in front of Cloud Run. Each hostname maps to a service's *.run.app origin (ORIGINS);
// the request is forwarded as is (WebSocket upgrades included), plus:
//   X-Client-IP      the visitor's address, which Cloudflare knows and the origin otherwise wouldn't
//   X-Forwarded-Host the hostname the visitor used
//   X-Edge-Secret    proof the request came through here, sent only to the hosts that check it
//                    (SECRET_HOSTS: Lighthouse, which refuses requests without it)
// Headers a visitor sends with those names are replaced, never passed through.
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
    if (!origin) return new Response("Not found", { status: 404 });

    const target = new URL(url.pathname + url.search, origin);
    const headers = new Headers(request.headers);
    headers.set("X-Client-IP", request.headers.get("CF-Connecting-IP") ?? "");
    headers.set("X-Forwarded-Host", url.hostname);
    headers.set("X-Forwarded-Proto", "https");
    // The secret goes only to the origins that verify it: sending it to the others would hand
    // them the means to pass as the edge.
    headers.delete("X-Edge-Secret");
    if (JSON.parse(env.SECRET_HOSTS).includes(url.hostname)) headers.set("X-Edge-Secret", env.EDGE_SECRET);

    const toOrigin = (extra = {}) => fetch(target, {
      method: request.method,
      headers,
      body: request.body,
      redirect: "manual", // redirects go back to the browser, which follows them via the edge
      ...extra,
    });

    if (isPublicPage(request, url, env)) return page(url, toOrigin, ctx);

    // Static files are cached at the edge, so repeat downloads never reach Google (whose free
    // tier includes only 1 GB a month of outbound data): for a year when the address carries a
    // fingerprint of the contents (?v=) or is a font, otherwise for an hour. Everything else,
    // including every API call and WebSocket, goes to the origin every time.
    if ((request.method === "GET" || request.method === "HEAD") && STATIC.test(url.pathname)) {
      const ttl = url.searchParams.has("v") || url.pathname.includes("/fonts/") ? YEAR : HOUR;
      return toOrigin({ cf: { cacheEverything: true, cacheTtlByStatus: { "200-299": ttl, "400-599": 0 } } });
    }
    return toOrigin();
  },
};

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
