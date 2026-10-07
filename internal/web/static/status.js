// Keeps a page's figures fresh without reloading it: every data-refresh seconds, while the tab is
// visible, the page is fetched again and its <main> replaced. A full reload would count as a new
// visit each minute, and would keep running in a tab nobody is looking at.
//
// Without JavaScript the page reloads itself instead (a <noscript> refresh).
(() => {
  const main = document.querySelector('main[data-refresh]');
  if (!main) return;
  const every = Math.max(15, Number(main.dataset.refresh) || 60) * 1000;
  let busy = false;
  async function refresh() {
    if (busy || document.visibilityState !== 'visible') return;
    busy = true;
    try {
      const res = await fetch(location.pathname, { headers: { Accept: 'text/html' }, credentials: 'same-origin' });
      if (!res.ok) return;
      const next = new DOMParser().parseFromString(await res.text(), 'text/html').querySelector('main[data-refresh]');
      if (next) main.replaceChildren(...next.childNodes);
    } catch { /* offline for a moment: keep what is shown */ } finally {
      busy = false;
    }
  }
  setInterval(refresh, every);
  // Coming back to the tab after a while: catch up at once.
  let hiddenAt = 0;
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') hiddenAt = Date.now();
    else if (hiddenAt && Date.now() - hiddenAt > every) refresh();
  });
})();
