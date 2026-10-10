// Run with: node --test deploy/cloudrun/edge/worker.test.js
import assert from "node:assert/strict";
import { beforeEach, test } from "node:test";
import worker from "./worker.js";

const env = {
  ORIGINS: JSON.stringify({ "site.example": "https://site.run.app", "demo.site.example": "https://demo.run.app" }),
  PAGE_HOSTS: JSON.stringify(["site.example"]),
  EDGE_SECRETS: JSON.stringify({ "site.example": "s3cret", "demo.site.example": "demo-s3cret", guide: "guide-s3cret" }),
  CRON: JSON.stringify({ "demo.site.example": ["/internal/housekeeping"] }),
  GUIDE: JSON.stringify({ host: "site.example", path: "/api/guide", origin: "https://guide.lambda.example", demos: { "demo.site.example": { name: "Demo", viewing: "demo/07-demo#what-does-a-visitor-see" } } }),
};
const html = (text) => new Response(`<html><body><main>${text}</main></body></html>`, { status: 200, headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "public, max-age=30" } });

let calls, origin, store, now;
const realNow = Date.now;
beforeEach(() => {
  calls = [];
  store = new Map();
  now = 1_700_000_000_000;
  Date.now = () => now;
  globalThis.caches = { default: {
    match: async (req) => store.get(req.url)?.clone(),
    put: async (req, res) => { store.set(req.url, res); },
  } };
  globalThis.fetch = async (target, init) => { calls.push({ url: String(target), method: init.method, headers: new Headers(init.headers), cf: init.cf, body: init.body }); return origin(String(target)); };
  origin = () => html("v1");
});
const ask = async (url, headers = {}) => {
  const pending = [];
  const res = await worker.fetch(new Request(url, { headers }), env, { waitUntil: (p) => pending.push(p) });
  await Promise.all(pending);
  return res;
};

test("each host gets its own secret, a forged one is dropped, and a host with none gets none", async () => {
  await ask("https://site.example/api/monitors", { "X-Edge-Secret": "forged" });
  await ask("https://demo.site.example/x", { "X-Edge-Secret": "forged" });
  assert.equal(calls[0].headers.get("X-Edge-Secret"), "s3cret");
  assert.equal(calls[1].headers.get("X-Edge-Secret"), "demo-s3cret");
  assert.equal((await ask("https://other.example/")).status, 404);

  const plain = { ...env, EDGE_SECRETS: JSON.stringify({ "site.example": "s3cret" }) };
  await worker.fetch(new Request("https://demo.site.example/x", { headers: { "X-Edge-Secret": "forged" } }), plain, { waitUntil() {} });
  assert.equal(calls[2].headers.get("X-Edge-Secret"), null);
});

test("no visitor reaches an internal path; the edge's scheduled call does, with that host's secret", async () => {
  const res = await ask("https://demo.site.example/internal/housekeeping", { "X-Edge-Secret": "demo-s3cret" });
  assert.equal(res.status, 404);
  assert.equal(calls.length, 0);

  origin = () => new Response(null, { status: 204 });
  const pending = [];
  await worker.scheduled({}, env, { waitUntil: (p) => pending.push(p) });
  await Promise.all(pending);
  assert.deepEqual(calls.map((c) => [c.method, c.url, c.headers.get("X-Edge-Secret")]), [["POST", "https://demo.run.app/internal/housekeeping", "demo-s3cret"]]);
});

test("a scheduled call that fails doesn't throw: the next hour tries again", async () => {
  origin = () => { throw new Error("unreachable"); };
  await worker.scheduled({}, env, { waitUntil() {} });
  origin = () => new Response(null, { status: 500 });
  await worker.scheduled({}, env, { waitUntil() {} });
});

test("a public page is served from its copy for a minute, then fetched again", async () => {
  const first = await ask("https://site.example/projects/lighthouse?ref=acme-12");
  assert.equal(first.headers.get("X-Edge-Cache"), "miss");
  const second = await ask("https://site.example/projects/lighthouse");
  assert.equal(second.headers.get("X-Edge-Cache"), "hit");
  assert.equal(second.headers.get("Cache-Control"), "public, max-age=30, no-transform"); // the origin's, not the edge's
  assert.equal(second.headers.get("X-Edge-Saved-At"), null);
  assert.match(await second.text(), /v1/);
  assert.equal(calls.length, 1);

  now += 61_000;
  origin = () => html("v2");
  const third = await ask("https://site.example/projects/lighthouse");
  assert.equal(third.headers.get("X-Edge-Cache"), "miss");
  assert.match(await third.text(), /v2/);
  assert.equal(calls.length, 2);
});

test("when the origin fails, the last copy is served and says so", async () => {
  await ask("https://site.example/status");
  now += 10 * 60_000;
  for (const failure of [() => new Response("boom", { status: 503 }), () => { throw new Error("unreachable"); }]) {
    origin = failure;
    const res = await ask("https://site.example/status");
    assert.equal(res.status, 200);
    assert.equal(res.headers.get("X-Edge-Cache"), "stale");
    assert.equal(res.headers.get("Cache-Control"), "no-store, no-transform");
    const body = await res.text();
    assert.match(body, /<body>\n<p class="edge-stale" role="status">The site isn't responding right now\. This is a copy saved at 2023-11-14 22:13 UTC/);
    assert.match(body, /v1/);
  }
  // With no copy, the failure is passed on as it is.
  assert.equal((await ask("https://site.example/about")).status, 502);
});

test("nothing personal or changeable is kept", async () => {
  // A signed-in request, a console page, an API call and a write all go to the origin every time.
  for (const [url, headers] of [
    ["https://site.example/", { Cookie: "__Host-lh_session=abc" }],
    ["https://site.example/console/", {}],
    ["https://site.example/api/monitors", {}],
    ["https://demo.site.example/", {}],
  ]) {
    await ask(url, headers);
    await ask(url, headers);
  }
  assert.equal(calls.length, 8);
  assert.equal(store.size, 0);

  // An answer that sets a cookie, or isn't a 200, is never kept.
  origin = () => new Response("x", { status: 200, headers: { "Set-Cookie": "a=b" } });
  await ask("https://site.example/");
  origin = () => new Response("gone", { status: 404 });
  await ask("https://site.example/projects/nope");
  assert.equal(store.size, 0);
});

test("static files are kept for an hour, or a year when fingerprinted", async () => {
  await ask("https://site.example/static/style.css");
  await ask("https://site.example/static/style.css?v=abc123");
  await ask("https://site.example/static/fonts/newsreader.woff2");
  assert.deepEqual(calls.map((c) => c.cf.cacheTtlByStatus["200-299"]), [3600, 31_536_000, 31_536_000]);
});

test.after(() => { Date.now = realNow; });

test("pages leave marked no-transform, so nothing rewrites them on the way out; other answers are untouched", async () => {
  const page = await ask("https://site.example/about");
  assert.equal(page.headers.get("Cache-Control"), "public, max-age=30, no-transform");
  assert.equal((await ask("https://site.example/about")).headers.get("Cache-Control"), "public, max-age=30, no-transform"); // from the copy too

  origin = () => new Response("<html></html>", { status: 200, headers: { "Content-Type": "text/html" } });
  assert.equal((await ask("https://demo.site.example/")).headers.get("Cache-Control"), "no-transform");
  origin = () => new Response("<html></html>", { status: 200, headers: { "Content-Type": "text/html", "Cache-Control": "no-store, no-transform" } });
  assert.equal((await ask("https://demo.site.example/")).headers.get("Cache-Control"), "no-store, no-transform");

  const json = new Response("{}", { status: 200, headers: { "Content-Type": "application/json", "Cache-Control": "no-store" } });
  origin = () => json;
  const answer = await ask("https://demo.site.example/api/x");
  assert.equal(answer, json); // the very same response object: a WebSocket upgrade must pass through whole
});

test("a question for the assistant goes to its service with that service's secret, the visitor's address, and nothing else", async () => {
  origin = () => new Response(JSON.stringify({ kind: "answer", text: "Yes." }), { status: 200, headers: { "Content-Type": "application/json" } });
  const res = await worker.fetch(new Request("https://site.example/api/guide", {
    method: "POST", body: JSON.stringify({ message: "What is Redacted?" }),
    headers: { Origin: "https://site.example", Cookie: "__Host-lh_session=owner", "CF-Connecting-IP": "203.0.113.7", "X-Edge-Secret": "forged", Authorization: "Bearer x" },
  }), env, { waitUntil() {} });
  assert.deepEqual(await res.json(), { kind: "answer", text: "Yes." });
  assert.equal(res.headers.get("Cache-Control"), "no-store");
  const [call] = calls;
  assert.equal(call.url, "https://guide.lambda.example/api/guide");
  assert.equal(call.body, JSON.stringify({ message: "What is Redacted?" }));
  assert.deepEqual([...call.headers.keys()].sort(), ["content-type", "x-client-ip", "x-edge-secret"]); // no cookie, no authorization
  assert.equal(call.headers.get("X-Edge-Secret"), "guide-s3cret");
  assert.equal(call.headers.get("X-Client-IP"), "203.0.113.7");
});

test("the assistant's path takes only posts from this site's own pages, of a sane size, and only on its host", async () => {
  origin = () => new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
  const post = (headers, body = "{}", host = "site.example") => worker.fetch(new Request(`https://${host}/api/guide`, { method: "POST", body, headers }), env, { waitUntil() {} });
  assert.equal((await worker.fetch(new Request("https://site.example/api/guide", { headers: { Origin: "https://site.example" } }), env, { waitUntil() {} })).status, 405);
  assert.equal((await post({ Origin: "https://evil.example" })).status, 403);
  assert.equal((await post({})).status, 403);
  assert.equal((await post({ Origin: "https://site.example" }, "x".repeat(9000))).status, 413);
  assert.equal(calls.length, 0); // none of those reached the service
  await post({ Origin: "https://demo.site.example" }, "{}", "demo.site.example"); // another host: an ordinary path of that host
  assert.equal(calls[0].url, "https://demo.run.app/api/guide");
});

test("when the assistant's service can't be reached, the visitor is told so, plainly, with a 200", async () => {
  origin = () => { throw new Error("unreachable"); };
  const res = await worker.fetch(new Request("https://site.example/api/guide", { method: "POST", body: "{}", headers: { Origin: "https://site.example" } }), env, { waitUntil() {} });
  assert.equal(res.status, 200);
  assert.equal((await res.json()).kind, "limit");
});

test("the assistant's service may sit under a path of its own, and one that is off or blocked is resting, not broken", async () => {
  const staged = { ...env, GUIDE: JSON.stringify({ host: "site.example", path: "/api/guide", origin: "https://abc.execute-api.example/live/" }) };
  const post = () => worker.fetch(new Request("https://site.example/api/guide", { method: "POST", body: "{}", headers: { Origin: "https://site.example" } }), staged, { waitUntil() {} });
  origin = () => new Response(JSON.stringify({ kind: "answer", text: "Yes." }), { status: 200 });
  assert.equal((await (await post()).json()).kind, "answer");
  assert.equal(calls[0].url, "https://abc.execute-api.example/live/api/guide");
  for (const status of [429, 403, 500, 502, 504]) {
    origin = () => new Response(JSON.stringify({ Reason: "ConcurrentInvocationLimitExceeded", message: "Forbidden" }), { status });
    const res = await post();
    assert.equal(res.status, 200);
    assert.deepEqual([(await res.json()).kind, status], ["limit", status]);
  }
  origin = () => new Response(JSON.stringify({ error: "too large" }), { status: 413 });
  assert.equal((await post()).status, 413); // the service's own verdict on a request is passed on
});

test("the assistant follows a visitor into a demo: its files come from the site, and questions go to its service", async () => {
  const ctx = { waitUntil() {} };
  origin = () => new Response("// the script", { status: 200, headers: { "Content-Type": "text/javascript" } });
  const script = await worker.fetch(new Request("https://demo.site.example/_guide/guide.js", { headers: { Cookie: "session=1" } }), env, ctx);
  assert.equal(await script.text(), "// the script");
  assert.equal(calls[0].url, "https://site.run.app/static/guide.js"); // the site's copy, not the demo's
  assert.equal(calls[0].headers.get("Cookie"), null);
  assert.equal(calls[0].headers.get("X-Edge-Secret"), "s3cret"); // the site's secret, or the site would refuse the edge
  assert.equal(calls[0].headers.get("X-Forwarded-Host"), "site.example");
  await worker.fetch(new Request("https://demo.site.example/_guide/guide.css"), env, ctx);
  assert.equal(calls[1].url, "https://site.run.app/static/guide.css");
  for (const path of ["/_guide/../static/style.css", "/_guide/other.js", "/_guide/"]) {
    assert.equal((await worker.fetch(new Request(`https://demo.site.example${path}`), env, ctx)).status === 200 && calls.length > 2 && calls.at(-1).url.startsWith("https://site.run.app"), false, path);
  }

  calls.length = 0;
  origin = () => new Response(JSON.stringify({ kind: "answer", text: "Yes." }), { status: 200 });
  const ask = (headers) => worker.fetch(new Request("https://demo.site.example/_guide/ask", { method: "POST", body: "{}", headers }), env, ctx);
  assert.equal((await ask({ Origin: "https://site.example" })).status, 403); // each host's own pages only
  assert.equal((await ask({ Origin: "https://demo.site.example", Cookie: "session=1" })).status, 200);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "https://guide.lambda.example/api/guide");
  assert.equal(calls[0].headers.get("X-Edge-Secret"), "guide-s3cret");
  assert.equal(calls[0].headers.get("Cookie"), null);

  // On the site itself /_guide/ is nothing special, and on a host that isn't a demo neither is it.
  calls.length = 0;
  origin = () => new Response("x", { status: 404 });
  await worker.fetch(new Request("https://site.example/_guide/guide.js"), env, ctx);
  assert.equal(calls[0].url, "https://site.run.app/_guide/guide.js");
});

test("a demo's page leaves with the assistant's script added, and nothing else of the demo's is touched", async () => {
  // Cloudflare's HTMLRewriter, as far as this uses it: append to the end of <body>.
  globalThis.HTMLRewriter = class {
    on(selector, handlers) { this.selector = selector; this.handlers = handlers; return this; }
    transform(response) {
      let added = "";
      this.handlers.element({ append: (markup, options) => { added = options.html ? markup : ""; } });
      const body = new ReadableStream({ async start(controller) {
        controller.enqueue(new TextEncoder().encode((await response.text()).replace("</body>", `${added}</body>`)));
        controller.close();
      } });
      return new Response(body, response);
    }
  };
  try {
    origin = () => new Response("<html><body><div id=root></div></body></html>", { status: 200, headers: { "Content-Type": "text/html; charset=utf-8" } });
    const page = await worker.fetch(new Request("https://demo.site.example/"), env, { waitUntil() {} });
    const text = await page.text();
    assert.match(text, /<script src="\/_guide\/guide\.js" data-css="\/_guide\/guide\.css" data-site="https:\/\/site\.example" data-here="Demo" data-viewing="demo\/07-demo#what-does-a-visitor-see" defer><\/script><\/body>/);

    // Not HTML, not a GET, or not a demo: passed through as it came.
    const json = new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    origin = () => json;
    assert.equal(await worker.fetch(new Request("https://demo.site.example/api/x"), env, { waitUntil() {} }), json);
    origin = () => new Response("<html><body></body></html>", { status: 200, headers: { "Content-Type": "text/html" } });
    const posted = await worker.fetch(new Request("https://demo.site.example/", { method: "POST", body: "x" }), env, { waitUntil() {} });
    assert.equal(await posted.text(), "<html><body></body></html>");
    const site = await worker.fetch(new Request("https://site.example/console/"), env, { waitUntil() {} });
    assert.equal(await site.text(), "<html><body></body></html>"); // the site loads its own copy, in its own pages
  } finally {
    delete globalThis.HTMLRewriter;
  }
});

