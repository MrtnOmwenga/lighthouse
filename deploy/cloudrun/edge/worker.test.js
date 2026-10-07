// Run with: node --test deploy/cloudrun/edge/worker.test.js
import assert from "node:assert/strict";
import { beforeEach, test } from "node:test";
import worker from "./worker.js";

const env = {
  ORIGINS: JSON.stringify({ "site.example": "https://site.run.app", "demo.site.example": "https://demo.run.app" }),
  SECRET_HOSTS: JSON.stringify(["site.example"]),
  PAGE_HOSTS: JSON.stringify(["site.example"]),
  EDGE_SECRET: "s3cret",
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
  globalThis.fetch = async (target, init) => { calls.push({ url: String(target), headers: init.headers, cf: init.cf }); return origin(String(target)); };
  origin = () => html("v1");
});
const ask = async (url, headers = {}) => {
  const pending = [];
  const res = await worker.fetch(new Request(url, { headers }), env, { waitUntil: (p) => pending.push(p) });
  await Promise.all(pending);
  return res;
};

test("the secret reaches only the host that checks it, and a forged one is dropped", async () => {
  await ask("https://site.example/api/monitors", { "X-Edge-Secret": "forged" });
  await ask("https://demo.site.example/x", { "X-Edge-Secret": "forged" });
  assert.equal(calls[0].headers.get("X-Edge-Secret"), "s3cret");
  assert.equal(calls[1].headers.get("X-Edge-Secret"), null);
  assert.equal((await ask("https://other.example/")).status, 404);
});

test("a public page is served from its copy for a minute, then fetched again", async () => {
  const first = await ask("https://site.example/projects/lighthouse?ref=acme-12");
  assert.equal(first.headers.get("X-Edge-Cache"), "miss");
  const second = await ask("https://site.example/projects/lighthouse");
  assert.equal(second.headers.get("X-Edge-Cache"), "hit");
  assert.equal(second.headers.get("Cache-Control"), "public, max-age=30"); // the origin's, not the edge's
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
    assert.equal(res.headers.get("Cache-Control"), "no-store");
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
