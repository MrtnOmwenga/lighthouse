// Architecture pages. The server sends the parts as links in lanes and the walk-through as a
// list; this draws the wires between the parts, shows a part's details when it is chosen, and
// plays a walk-through one step at a time. Without it the page is still a complete, plain list.
(() => {
  const root = document.querySelector('[data-arch]');
  const now = document.getElementById('arch-now');
  if (!root || !now) return;

  const NS = 'http://www.w3.org/2000/svg';
  const svg = root.querySelector('.arch-wires');
  const still = matchMedia('(prefers-reduced-motion: reduce)');
  const el = (tag, props = {}, ...children) => {
    const node = Object.assign(document.createElement(tag), props);
    node.append(...children);
    return node;
  };
  const shape = (tag, attrs = {}) => {
    const node = document.createElementNS(NS, tag);
    for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
    return node;
  };

  const parts = new Map([...root.querySelectorAll('.arch-part')].map((a) => [a.dataset.part, a]));
  const name = (id) => parts.get(id)?.textContent ?? id;
  const links = [...document.querySelectorAll('.arch-links li')]
    .map((li) => ({ from: li.dataset.from, to: li.dataset.to, label: li.querySelector('.l')?.textContent ?? '' }))
    .filter((l) => parts.has(l.from) && parts.has(l.to));
  const flows = [...document.querySelectorAll('.arch-flow')].map((section) => ({
    id: section.dataset.flow,
    title: section.querySelector('h2').textContent,
    steps: [...section.querySelectorAll('.arch-steps li')].map((li) => ({
      li, from: li.dataset.from, to: li.dataset.to,
      title: li.querySelector('.t').textContent, text: li.querySelector('p').textContent,
    })),
  }));

  const kicker = now.querySelector('[data-now-kicker]');
  const body = now.querySelector('[data-now-body]');
  const back = now.querySelector('[data-back]');
  const next = now.querySelector('[data-next]');
  const play = now.querySelector('[data-play]');
  const clear = now.querySelector('[data-clear]');
  const badge = now.querySelector('[data-now-badge]');
  const progress = now.querySelector('[data-now-progress]');
  const pips = now.querySelector('[data-pips]');
  const hint = [...body.childNodes].map((n) => n.cloneNode(true));

  // What is showing: nothing, one part, or one step of a walk-through.
  let state = { kind: 'none' };
  let flow = flows[0];
  let timer = 0;
  let wires = [];
  // The marker that travels a wire carries the step's number, the same one the caption shows.
  const dot = shape('g', { class: 'arch-dot' });
  const dotNumber = shape('text', { y: 4.5 });
  dot.append(shape('circle', { r: 11 }), dotNumber);
  let travelling = 0;
  const SHORT = 18;
  const pace = () => (still.matches ? 5000 : 2600); // milliseconds a step is shown for while playing

  // ---- Drawing -------------------------------------------------------------------------------

  const box = (id) => {
    const r = parts.get(id).getBoundingClientRect();
    const o = root.getBoundingClientRect();
    return { l: r.left - o.left, t: r.top - o.top, r: r.right - o.left, b: r.bottom - o.top, w: r.width, h: r.height, cx: r.left - o.left + r.width / 2, cy: r.top - o.top + r.height / 2 };
  };

  function draw() {
    const boxes = new Map([...parts.keys()].map((id) => [id, box(id)]));
    const sameRow = (a, b) => Math.min(a.b, b.b) - Math.max(a.t, b.t) > Math.min(a.h, b.h) / 2;
    // Each wire leaves through a side of its two boxes. Wires sharing a side are spread along it,
    // ordered by where they are going, so they don't cross on the way out.
    const sides = new Map();
    const claim = (id, side, toward, wire, end) => {
      const key = `${id} ${side}`;
      if (!sides.has(key)) sides.set(key, []);
      sides.get(key).push({ toward, wire, end });
    };
    const plan = links.map((link) => {
      const a = boxes.get(link.from);
      const b = boxes.get(link.to);
      const wire = { link, a, b };
      if (sameRow(a, b)) {
        const [left, right] = a.cx < b.cx ? [a, b] : [b, a];
        const between = [...boxes.values()].some((c) => c !== a && c !== b && sameRow(c, a) && c.cx > left.cx && c.cx < right.cx);
        wire.route = between ? 'over' : 'across';
        if (between) {
          claim(link.from, 'top', b.cx, wire, 'p');
          claim(link.to, 'top', a.cx, wire, 'q');
        }
      } else {
        wire.route = 'down';
        const fromAbove = a.cy < b.cy;
        claim(link.from, fromAbove ? 'bottom' : 'top', b.cx, wire, 'p');
        claim(link.to, fromAbove ? 'top' : 'bottom', a.cx, wire, 'q');
      }
      return wire;
    });
    for (const [key, list] of sides) {
      const [id, side] = key.split(' ');
      const b = boxes.get(id);
      list.sort((x, y) => x.toward - y.toward);
      list.forEach((slot, i) => {
        slot.wire[slot.end] = { x: b.l + (b.w * (i + 1)) / (list.length + 1), y: side === 'top' ? b.t : b.b };
      });
    }

    // Wires between two rows run down, across and down again. Each gets its own height for the
    // part that runs across, so two wires in the same gap never lie on top of one another.
    const gaps = new Map();
    for (const wire of plan) {
      if (wire.route !== 'down' || Math.abs(wire.p.x - wire.q.x) < 1) continue;
      const key = `${Math.round(Math.min(wire.p.y, wire.q.y))} ${Math.round(Math.max(wire.p.y, wire.q.y))}`;
      if (!gaps.has(key)) gaps.set(key, []);
      gaps.get(key).push(wire);
    }
    for (const list of gaps.values()) {
      const upper = (w) => (w.p.y < w.q.y ? w.p : w.q);
      list.sort((x, y) => upper(x).x - upper(y).x);
      list.forEach((wire, i) => {
        const [top, bottom] = [Math.min(wire.p.y, wire.q.y) + 12, Math.max(wire.p.y, wire.q.y) - 14];
        wire.track = top + ((bottom - top) * (i + 1)) / (list.length + 1);
      });
    }

    const { width, height } = root.getBoundingClientRect();
    svg.setAttribute('viewBox', `0 0 ${width} ${height}`);
    const defs = shape('defs');
    for (const kind of ['idle', 'on', 'live']) {
      const marker = shape('marker', { id: `arch-arrow-${kind}`, viewBox: '0 0 10 10', refX: 9, refY: 5, markerWidth: 8, markerHeight: 8, orient: 'auto-start-reverse', markerUnits: 'userSpaceOnUse' });
      marker.append(shape('path', { d: 'M0 0L10 5L0 10z', class: `arch-arrow ${kind === 'idle' ? '' : kind}` }));
      defs.append(marker);
    }
    svg.replaceChildren(defs);
    const placed = [];
    wires = plan.map(({ link, a, b, route, p, q, track }) => {
      let d;
      let at;
      if (route === 'across') {
        const y = (Math.max(a.t, b.t) + Math.min(a.b, b.b)) / 2;
        const [x1, x2] = a.cx < b.cx ? [a.r, b.l] : [a.l, b.r];
        d = `M${x1} ${y}L${x2} ${y}`;
        // The gap between neighbours is narrower than most labels, so the label sits under it.
        at = { x: (x1 + x2) / 2, y: Math.max(a.b, b.b) + 16 };
      } else if (route === 'over') {
        const lift = Math.min(p.y, q.y) - 20;
        d = `M${p.x} ${p.y}V${lift}H${q.x}V${q.y}`;
        at = { x: (p.x + q.x) / 2, y: lift - 6 };
      } else if (track === undefined) {
        d = `M${p.x} ${p.y}V${q.y}`;
        at = { x: p.x, y: (p.y + q.y) / 2 + 4 };
      } else {
        d = `M${p.x} ${p.y}V${track}H${q.x}V${q.y}`;
        at = { x: (p.x + q.x) / 2, y: track - 6 };
      }
      const g = shape('g', { class: 'arch-wire' });
      const path = shape('path', { d, 'marker-end': 'url(#arch-arrow-idle)' });
      // Labels that would land on top of one another are moved down a line. Widths are estimated:
      // a hidden label can't be measured.
      const half = link.label.length * 3.4 + 4;
      at.x = Math.min(Math.max(at.x, half), width - half);
      while (placed.some((o) => Math.abs(o.y - at.y) < 14 && Math.abs(o.x - at.x) < o.half + half)) at.y += 15;
      placed.push({ ...at, half });
      const text = shape('text', { x: at.x, y: at.y });
      text.textContent = link.label;
      g.append(path, text);
      svg.append(g);
      return { ...link, g, path };
    });
    svg.append(dot);
    dot.setAttribute('visibility', 'hidden');
    paint(false);
  }

  // ---- Showing a state -----------------------------------------------------------------------

  const wireFor = (from, to) => wires.find((w) => (w.from === from && w.to === to) || (w.from === to && w.to === from));

  function travel(wire, reversed) {
    cancelAnimationFrame(travelling);
    if (!wire) return dot.setAttribute('visibility', 'hidden');
    const length = wire.path.getTotalLength();
    dotNumber.textContent = String(state.index + 1);
    if (still.matches) {
      // No travelling: the marker simply sits where the step arrives.
      const end = wire.path.getPointAtLength(reversed ? SHORT : Math.max(0, length - SHORT));
      dot.setAttribute('transform', `translate(${end.x} ${end.y})`);
      return dot.setAttribute('visibility', 'visible');
    }
    const started = performance.now();
    dot.setAttribute('visibility', 'visible');
    const frame = (t) => {
      const done = Math.min(1, (t - started) / 700);
      const eased = done < 0.5 ? 2 * done * done : 1 - (-2 * done + 2) ** 2 / 2;
      // It stops just short of the part it is going to, so the part doesn't hide it.
      const along = eased * Math.max(0, length - SHORT);
      const point = wire.path.getPointAtLength(reversed ? length - along : along);
      dot.setAttribute('transform', `translate(${point.x} ${point.y})`);
      if (done < 1) travelling = requestAnimationFrame(frame);
    };
    travelling = requestAnimationFrame(frame);
  }

  function paint(move = true) {
    const chosen = new Set();
    const near = new Set();
    let live = null;
    if (state.kind === 'part') {
      chosen.add(state.id);
      for (const l of links) {
        if (l.from === state.id) near.add(l.to);
        if (l.to === state.id) near.add(l.from);
      }
    } else if (state.kind === 'step') {
      const step = flow.steps[state.index];
      chosen.add(step.from).add(step.to);
      live = wireFor(step.from, step.to);
    }
    root.classList.toggle('focus', state.kind !== 'none');
    root.classList.toggle('live', state.kind === 'step');
    for (const [id, a] of parts) {
      a.classList.toggle('sel', chosen.has(id));
      a.classList.toggle('near', near.has(id));
      a.setAttribute('aria-pressed', String(chosen.has(id)));
    }
    for (const w of wires) {
      const on = state.kind === 'part' && (w.from === state.id || w.to === state.id);
      w.g.classList.toggle('on', on);
      w.g.classList.toggle('live', w === live);
      w.path.setAttribute('marker-end', `url(#arch-arrow-${w === live ? 'live' : on ? 'on' : 'idle'})`);
      if (w === live || on) svg.insertBefore(w.g, dot); // drawn last, so on top
    }
    for (const f of flows) {
      f.steps.forEach((s, i) => {
        const current = state.kind === 'step' && f === flow && i === state.index;
        s.li.classList.toggle('current', current);
        if (current) s.li.setAttribute('aria-current', 'step'); else s.li.removeAttribute('aria-current');
      });
    }
    if (live && move) travel(live, live.from !== flow.steps[state.index].from);
    else if (!live) travel(null);
  }

  function describe() {
    if (state.kind === 'part') {
      const about = document.getElementById(`about-${state.id}`);
      kicker.textContent = 'This part';
      const joins = el('ul');
      for (const l of links) {
        if (l.from === state.id) joins.append(el('li', { textContent: `→ ${name(l.to)}: ${l.label}` }));
        if (l.to === state.id) joins.append(el('li', { textContent: `← ${name(l.from)}: ${l.label}` }));
      }
      body.replaceChildren(...[...about.children].map((n) => n.cloneNode(true)), joins);
    } else if (state.kind === 'step') {
      const step = flow.steps[state.index];
      kicker.textContent = `Step ${state.index + 1} of ${flow.steps.length} · ${name(step.from)} → ${name(step.to)}`;
      body.replaceChildren(el('h3', { textContent: step.title }), el('p', { textContent: step.text }));
    } else {
      kicker.textContent = 'Start here';
      body.replaceChildren(...hint.map((n) => n.cloneNode(true)));
    }
    const index = state.kind === 'step' ? state.index : -1;
    badge.hidden = index < 0;
    badge.textContent = String(index + 1);
    // While it plays by itself the caption isn't read out: a screen reader would never keep up.
    body.setAttribute('aria-live', timer ? 'off' : 'polite');
    if (!next) return;
    [...pips.children].forEach((pip, i) => {
      pip.classList.toggle('current', i === index);
      if (i === index) pip.setAttribute('aria-current', 'step'); else pip.removeAttribute('aria-current');
    });
    for (const a of progress.getAnimations()) a.cancel();
    if (timer && index >= 0) progress.animate([{ transform: 'scaleX(0)' }, { transform: 'scaleX(1)' }], { duration: pace(), easing: 'linear', fill: 'forwards' });
    back.disabled = index <= 0;
    next.disabled = index >= flow.steps.length - 1;
    play.textContent = timer ? 'Pause' : 'Play';
    clear.hidden = state.kind === 'none';
  }

  // `keep` is a step taken by the page itself while playing: it doesn't stop the playing, and
  // doesn't move the reader, who may be reading something else by now.
  function show(to, { keep = false } = {}) {
    if (!keep) stop();
    state = to;
    paint();
    describe();
    // The diagram and the words about it are read together, so both are kept on screen.
    const top = root.getBoundingClientRect().top;
    const bottom = now.getBoundingClientRect().bottom;
    if (!keep && to.kind !== 'none' && (top < 0 || bottom > innerHeight)) root.scrollIntoView({ block: 'start', behavior: still.matches ? 'auto' : 'smooth' });
  }

  function stop() {
    clearInterval(timer);
    timer = 0;
  }

  function stepTo(index, options) {
    if (!flow || index < 0 || index >= flow.steps.length) return;
    show({ kind: 'step', index }, options);
  }

  // Playing goes round: after the last step it starts again, until something else is chosen.
  // Started by the page on arrival (`unasked`), it leaves the reader where they are.
  function start(unasked = false) {
    const onward = () => (state.kind === 'step' && state.index < flow.steps.length - 1 ? state.index + 1 : 0);
    stepTo(onward(), { keep: unasked });
    timer = setInterval(() => stepTo(onward(), { keep: true }), pace());
    describe();
  }

  // An address names what to show: #part-api, #flow-demote or #flow-demote-3. The assistant uses
  // the same addresses when it points at something here.
  function follow(id) {
    const part = /^part-(.+)$/.exec(id);
    if (part && parts.has(part[1])) return show({ kind: 'part', id: part[1] }), true;
    const step = /^flow-(.+?)(?:-(\d+))?$/.exec(id);
    const found = step && flows.find((f) => f.id === step[1]);
    if (!found) return false;
    flow = found;
    stepTo(step[2] ? Number(step[2]) - 1 : 0);
    return true;
  }

  // ---- Wiring it up --------------------------------------------------------------------------

  for (const [id, a] of parts) {
    a.setAttribute('role', 'button');
    a.addEventListener('click', (e) => {
      e.preventDefault();
      show(state.kind === 'part' && state.id === id ? { kind: 'none' } : { kind: 'part', id });
    });
    a.addEventListener('keydown', (e) => {
      if (e.key === ' ') { e.preventDefault(); a.click(); }
    });
  }
  for (const f of flows) {
    f.steps.forEach((s, i) => s.li.querySelector('a').addEventListener('click', (e) => {
      e.preventDefault();
      flow = f;
      stepTo(i);
    }));
  }
  if (pips && flow) {
    flow.steps.forEach((step, i) => {
      const pip = el('button', { type: 'button', className: 'arch-pip', textContent: String(i + 1) });
      pip.setAttribute('aria-label', `Step ${i + 1}: ${step.title}`);
      pip.addEventListener('click', () => stepTo(i));
      pips.append(pip);
    });
  }
  back?.addEventListener('click', () => stepTo(state.index - 1));
  next?.addEventListener('click', () => stepTo(state.kind === 'step' ? state.index + 1 : 0));
  play?.addEventListener('click', () => {
    if (timer) { stop(); describe(); } else start();
  });
  clear?.addEventListener('click', () => show({ kind: 'none' }));
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && state.kind !== 'none' && !e.defaultPrevented) show({ kind: 'none' });
  });
  addEventListener('hashchange', () => follow(location.hash.slice(1)));
  document.addEventListener('guide:point', (e) => follow(e.target.id));

  document.documentElement.classList.add('arch-ready');
  now.hidden = false;
  draw();
  describe();
  new ResizeObserver(() => draw()).observe(root);
  document.fonts?.ready.then(draw);
  // The walk-through plays by itself on arrival, unless the address asked for something in
  // particular or the reader has asked for less motion.
  if (location.hash && follow(location.hash.slice(1))) return;
  if (flow && !still.matches) start(true);
})();
