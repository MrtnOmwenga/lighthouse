// The edge in front of Cloud Run. Each hostname maps to a service's *.run.app origin (ORIGINS);
// the request is forwarded as is (WebSocket upgrades included), plus:
//   X-Client-IP      the visitor's address, which Cloudflare knows and the origin otherwise wouldn't
//   X-Forwarded-Host the hostname the visitor used
//   X-Edge-Secret    proof the request came through here, sent only to the hosts that check it
//                    (SECRET_HOSTS: Lighthouse, which refuses requests without it)
// Headers a visitor sends with those names are replaced, never passed through.

const STATIC = /\.(?:js|mjs|css|map|png|jpe?g|gif|webp|avif|svg|ico|woff2?|ttf)$/i;

export default {
  async fetch(request, env) {
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

    // Static files are cached at the edge for an hour, so repeat downloads never reach Google
    // (whose free tier includes only 1 GB a month of outbound data). Everything else, including
    // every API call and WebSocket, goes to the origin every time.
    const cacheable = (request.method === "GET" || request.method === "HEAD") && STATIC.test(url.pathname);
    return fetch(target, {
      method: request.method,
      headers,
      body: request.body,
      redirect: "manual", // redirects go back to the browser, which follows them via the edge
      ...(cacheable ? { cf: { cacheEverything: true, cacheTtlByStatus: { "200-299": 3600, "400-599": 0 } } } : {}),
    });
  },
};
