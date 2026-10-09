// The launch page: a short, auto-advancing introduction to a demo while it wakes up, then the
// demo itself.
//
// Without JavaScript the page still works: every part is visible, one after another, and "Open"
// is a plain link.
(() => {
  const root = document.querySelector('[data-launch]');
  if (!root) return;
  document.documentElement.classList.add('js');

  const { slug, demo, name, tour } = root.dataset;
  const $ = (sel) => document.querySelector(sel);
  const slides = [...root.querySelectorAll('.slide')];
  const last = slides.length - 1;
  const band = $('[data-band]');
  const tag = $('[data-tag]');
  const state = $('[data-state]');
  const detail = $('[data-detail]');
  const clock = $('[data-clock]');
  const skip = $('[data-skip]');
  // The last part has a version for each state of the demo; one is shown at a time.
  const versions = [...root.querySelectorAll('[data-when]')];
  const say = (when) => versions.forEach((v) => { v.hidden = v.dataset.when !== when; });
  say('waiting');
  const parts = $('[data-parts]');
  const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)').matches;
  const PART_MS = 6500;

  let index = 0;
  let ready = false;
  let introDone = false;
  let autoplay = true;
  let started = Date.now();
  let partStarted = Date.now();
  let readyAt = 0;

  // One button per part, labelled with its kicker, each with a progress bar.
  parts.style.setProperty('--parts', String(slides.length));
  const buttons = slides.map((slide, i) => {
    const b = document.createElement('button');
    b.type = 'button';
    const track = document.createElement('span');
    track.className = 'track';
    const fill = document.createElement('span');
    fill.className = 'fill';
    track.append(fill);
    const label = document.createElement('span');
    label.className = 'name';
    label.textContent = slide.dataset.part;
    b.append(track, label);
    b.setAttribute('aria-label', slide.dataset.part);
    b.addEventListener('click', () => { takeControl(); show(i); });
    parts.append(b);
    return { b, fill };
  });

  function show(i) {
    index = Math.max(0, Math.min(i, last));
    slides.forEach((s, k) => {
      s.classList.toggle('active', k === index);
      s.classList.toggle('past', k < index);
      s.setAttribute('aria-hidden', String(k !== index));
    });
    buttons.forEach(({ b, fill }, k) => {
      b.setAttribute('aria-current', String(k === index));
      b.classList.toggle('done', k < index);
      fill.style.width = k < index ? '100%' : '0';
    });
    partStarted = Date.now();
    if (index === last) {
      introDone = true;
      maybeOpen();
    }
  }

  // Once someone navigates themselves, stop moving the parts under them.
  function takeControl() {
    autoplay = false;
    buttons[index].fill.style.width = '0';
  }

  const track = (event) => window.lighthouse && window.lighthouse.track(event);
  document.querySelectorAll('[data-open]').forEach((a) => a.addEventListener('click', () => track('demo_open')));

  function open(url) {
    track('demo_open');
    state.textContent = `Opening ${name}…`;
    location.assign(url);
  }

  // Open automatically only when both the demo and the visitor are ready, and there is no choice
  // to make: a demo with a guided tour waits on the last part for the visitor to pick.
  function maybeOpen() {
    if (ready && introDone && !tour) setTimeout(() => open(demo), 900);
  }

  function markReady() {
    ready = true;
    track('demo_ready');
    readyAt = Date.now();
    root.classList.add('ready');
    band.hidden = false;
    tag.textContent = `UPDATE ${elapsed(readyAt)}`;
    state.textContent = `${name} is ready.`;
    detail.textContent = 'Open it whenever you like; the introduction will wait.';
    root.classList.remove('slow');
    say('ready');
    skip.disabled = false;
    skip.classList.add('primary');
    skip.textContent = `Open ${name} →`;
    maybeOpen();
  }

  function elapsed(at) {
    const s = Math.floor((at - started) / 1000);
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
  }

  // The button beside the status: it waits with the demo, then goes straight in.
  skip.addEventListener('click', () => {
    if (!introDone) track('intro_skip');
    open(demo);
  });
  document.addEventListener('keydown', (e) => {
    if (e.target instanceof HTMLElement && e.target.closest('input, textarea')) return;
    if (e.key === 'ArrowRight') { takeControl(); show(index + 1); }
    if (e.key === 'ArrowLeft') { takeControl(); show(index - 1); }
  });

  // The clock, and the current part's progress bar, advance together.
  function frame() {
    const now = Date.now();
    if (!ready) clock.textContent = elapsed(now);
    if (autoplay && index < last) {
      const f = Math.min(1, (now - partStarted) / PART_MS);
      if (!reducedMotion) buttons[index].fill.style.width = `${(f * 100).toFixed(1)}%`;
      if (f >= 1) show(index + 1);
    }
    requestAnimationFrame(frame);
  }

  // Poll until the demo answers: quickly at first, then more patiently. After a minute say
  // so, and let the visitor try it anyway.
  let warned = false;
  async function poll() {
    try {
      const res = await fetch(`/api/projects/${encodeURIComponent(slug)}/ready`, { cache: 'no-store' });
      if (res.ok && (await res.json()).ready) return markReady();
    } catch { /* offline for a moment: keep trying */ }
    const waited = Date.now() - started;
    if (waited > 60_000 && !warned) {
      warned = true;
      root.classList.add('slow');
      tag.textContent = 'DELAYED';
      state.textContent = `${name} is taking longer than usual`;
      detail.textContent = 'You can keep waiting, or try opening it anyway.';
      say('slow');
      skip.disabled = false;
      skip.textContent = 'Open anyway';
    }
    setTimeout(poll, waited < 60_000 ? 1500 : 5000);
  }

  skip.hidden = false;
  show(0);
  requestAnimationFrame(frame);
  poll();
})();
