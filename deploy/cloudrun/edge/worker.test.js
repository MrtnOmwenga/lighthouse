// Run with: node --test deploy/cloudrun/edge/worker.test.js
import assert from "node:assert/strict";
import { beforeEach, test } from "node:test";
import worker from "./worker.js";

const env = {
  ORIGINS: JSON.stringify({ "site.example": "https://site.run.app", "demo.site.example": "https://demo.run.app" }),
  PAGE_HOSTS: JSON.stringify(["site.example"]),
  EDGE_SECRETS: JSON.stringify({ "site.example": "s3cret", "demo.site.example": "demo-s3cret", guide: "guide-s3cret" }),
  CRON: JSON.stringify({ "demo.site.example": ["/internal/housekeeping"] }),
  GUIDE: JSON.stringify({ host: "site.example", path: "/api/guide", origin: "https://guide.lambda.example" }),
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
