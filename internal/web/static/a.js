// Visit counting, without cookies or storage. See /privacy for what is recorded and why.
(() => {
  // Respect the browser's "don't track me" signals before doing anything at all.
  if (navigator.globalPrivacyControl || navigator.doNotTrack === '1') {
    window.lighthouse = { track() {} };
    return;
  }
  const id = crypto.randomUUID ? crypto.randomUUID()
    : ([1e7] + -1e3 + -4e3 + -8e3 + -1e11).replace(/[018]/g, (c) => (c ^ (crypto.getRandomValues(new Uint8Array(1))[0] & (15 >> (c / 4)))).toString(16));
  const send = (path, body) => fetch(path, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
    keepalive: true, credentials: 'same-origin',
  }).catch(() => {});

  // A ?ref= tag (one per job application, say) is read once, then removed from the address bar
  // so it isn't passed on if the link is shared.
  const params = new URLSearchParams(location.search);
  const ref = params.get('ref') || '';
  if (ref) {
    params.delete('ref');
    const q = params.toString();
    history.replaceState(history.state, '', location.pathname + (q ? `?${q}` : '') + location.hash);
  }
  send('/api/a/view', { id, path: location.pathname, ref, referrer: document.referrer });

  // Engaged time: a heartbeat every 15 s, only while the page is visible and someone has scrolled,
  // clicked or typed in the last minute. The server measures the gaps itself.
  let active = Date.now();
  for (const e of ['scroll', 'pointermove', 'keydown', 'touchstart', 'click']) {
    addEventListener(e, () => { active = Date.now(); }, { passive: true });
  }
  const engaged = () => document.visibilityState === 'visible' && Date.now() - active < 60_000;
  setInterval(() => { if (engaged()) send('/api/a/ping', { id }); }, 15_000);
  addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden' && Date.now() - active < 60_000) send('/api/a/ping', { id });
  });

  const track = (name) => send('/api/a/event', { id, name });
  window.lighthouse = { track };

  // What a reader goes on to do: take the CV, write an email, look at the code or the profile.
  // Only the kind of link is recorded, once per page view.
  addEventListener('click', (e) => {
    const a = e.target instanceof Element ? e.target.closest('a[href]') : null;
    if (!a) return;
    const href = a.getAttribute('href') || '';
    if (/\.pdf($|\?)/i.test(href)) track('cv_download');
    else if (href.startsWith('mailto:')) track('contact_email');
    else if (/^https:\/\/(www\.)?github\.com\//.test(href)) track('outbound_github');
    else if (/^https:\/\/([a-z]+\.)?linkedin\.com\//.test(href)) track('outbound_linkedin');
  }, { capture: true });

  // A project's story read to its end: the foot of the page comes into view, at least ten seconds
  // after arriving (so a page that fits the screen doesn't count by itself).
  const foot = /^\/projects\/[^/]+$/.test(location.pathname) && 'IntersectionObserver' in window && document.querySelector('footer.foot');
  if (foot) {
    const arrived = Date.now();
    const seen = new IntersectionObserver((entries) => {
      if (entries.some((x) => x.isIntersecting) && Date.now() - arrived >= 10_000) { track('read_to_end'); seen.disconnect(); }
    });
    seen.observe(foot);
    addEventListener('scroll', () => { seen.unobserve(foot); seen.observe(foot); }, { passive: true });
  }
})();
