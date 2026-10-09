// The guide: an assistant that answers questions about the projects and the person on this site.
// It is a separate service (github.com/MrtnOmwenga/docent); this file is only its window.
//
// Three rules it keeps on this side:
// - what it shows is always text. Replies are put on the page as text, never as markup, and
//   "further reading" is built here from a short list of this site's own pages: the service
//   sends an id, never an address.
// - it stores nothing unless you use it: the conversation is kept in this tab (sessionStorage) so
//   it survives moving between pages, and is gone when the tab closes. See /privacy.
// - the site works the same without it.
(() => {
  // A preview: off unless switched on in this browser by opening any page with ?guide=on.
  const params = new URLSearchParams(location.search);
  if (params.has('guide')) {
    try { params.get('guide') === 'on' ? localStorage.setItem('lh_guide', '1') : localStorage.removeItem('lh_guide'); } catch { /* storage unavailable */ }
    params.delete('guide');
    const q = params.toString();
    history.replaceState(history.state, '', location.pathname + (q ? `?${q}` : '') + location.hash);
  }
  let on = false;
  try { on = localStorage.getItem('lh_guide') === '1'; } catch { /* stays off */ }
  if (!on) return;

  const STORIES = { lighthouse: '/projects/lighthouse', redacted: '/projects/redacted', ghostchat: '/projects/ghostchat', 'pair-bridge': '/projects/pair-bridge', background: '/about' };
  const NAMES = { lighthouse: 'Lighthouse', redacted: 'Redacted', ghostchat: 'GhostChat', 'offline-driver': 'offline-driver', 'living-docs': 'living-docs', 'pair-bridge': 'Pairbridge', background: 'About Martin' };
  const STARTERS = ['What has Martin built?', 'How does Redacted keep classified text from readers without clearance?', 'Where does his experience stop?'];
  const WAITING = ['Looking through the handbook…', 'Reading the relevant sections…', 'Checking it against the sources…', 'Writing it up…'];
  const KEY = 'lh_guide_talk';

  const saved = (() => { try { return JSON.parse(sessionStorage.getItem(KEY) || 'null'); } catch { return null; } })();
  const talk = saved && Array.isArray(saved.turns) ? saved : { id: null, turns: [], open: false, greeted: false };
  const keep = () => { try { sessionStorage.setItem(KEY, JSON.stringify(talk)); } catch { /* lasts for this page only */ } };

  const el = (tag, props = {}, ...children) => {
    const node = Object.assign(document.createElement(tag), props);
    for (const child of children) node.append(child);
    return node;
  };

  const log = el('div', { className: 'guide-log' });
  log.setAttribute('role', 'log');
  log.setAttribute('aria-live', 'polite');
  const input = el('textarea', { rows: 2, maxLength: 1200, placeholder: 'Ask about a project, or about Martin' });
  input.setAttribute('aria-label', 'Your question');
  const send = el('button', { type: 'submit', className: 'button primary', textContent: 'Ask' });
  const form = el('form', { className: 'guide-form' }, input, send);
  const close = el('button', { type: 'button', className: 'guide-close', textContent: 'Close' });
  const note = el('p', { className: 'guide-note' }, 'An AI assistant. It answers from Martin’s own notes and says where from. Questions are kept for 90 days. ', el('a', { href: '/privacy#assistant', textContent: 'Privacy' }));
  const panel = el('section', { className: 'guide-panel', hidden: true },
    el('header', {}, el('div', {}, el('strong', { textContent: 'The guide' }), el('span', { textContent: 'Ask about the work on this site' })), close),
    log, form, note);
  panel.setAttribute('role', 'dialog');
  panel.setAttribute('aria-label', 'The guide: ask about the work on this site');
  const launcher = el('button', { type: 'button', className: 'guide-launcher', textContent: 'Ask the guide' });
  launcher.setAttribute('aria-haspopup', 'dialog');

  // Taking the reader to a heading on one of this site's pages, and marking it for a moment so the
  // eye lands on it. The address is always one of this site's own: a path and an anchor.
  const PLACE = /^\/(?:projects\/[a-z0-9-]+|about)#[a-z0-9-]+$/;
  const POINT = 'lh_guide_point';
  const point = (id) => {
    const target = document.getElementById(id);
    if (!target) return;
    const calm = matchMedia('(prefers-reduced-motion: reduce)').matches;
    target.scrollIntoView({ behavior: calm ? 'auto' : 'smooth', block: 'start' });
    // On a narrow screen the window would cover what is being pointed at: it steps aside.
    if (innerWidth <= 560 && !panel.hidden) open(false);
    target.classList.add('guide-point');
    setTimeout(() => target.classList.remove('guide-point'), 6000);
  };
  const place = (route, label) => {
    const link = el('a', { href: route, className: 'guide-place', textContent: label });
    link.addEventListener('click', (e) => {
      const [path, id] = route.split('#');
      if (path === location.pathname) {
        e.preventDefault();
        history.replaceState(history.state, '', route);
        point(id);
      } else {
        try { sessionStorage.setItem(POINT, id); } catch { /* arrives at the top of the page instead */ }
      }
    });
    return link;
  };
  try {
    const arriving = sessionStorage.getItem(POINT);
    sessionStorage.removeItem(POINT);
    if (arriving && location.hash === `#${arriving}`) setTimeout(() => point(arriving), 300);
  } catch { /* nothing to point at */ }

  const show = (turn) => {
    const item = el('div', { className: `guide-turn ${turn.from}${turn.kind ? ` ${turn.kind}` : ''}` }, el('p', { textContent: turn.text }));
    if (turn.sources && turn.sources.length) {
      const list = el('ul', { className: 'guide-sources' });
      list.setAttribute('aria-label', 'Where this comes from');
      for (const s of turn.sources) {
        const label = `${NAMES[s.project] || s.project} · ${s.heading}`;
        list.append(typeof s.route === 'string' && PLACE.test(s.route) ? el('li', {}, place(s.route, label)) : el('li', { textContent: label }));
      }
      item.append(el('p', { className: 'label', textContent: 'Where this comes from' }), list);
    }
    const next = turn.further_reading;
    if (next && typeof next.route === 'string' && PLACE.test(next.route)) {
      item.append(el('p', { className: 'guide-next' }, 'Read more: ', place(next.route, `${NAMES[next.project] || next.project} · ${next.heading}`)));
    } else if (next && STORIES[next.project]) {
      item.append(el('p', { className: 'guide-next' }, 'Read more: ', el('a', { href: STORIES[next.project], textContent: next.project === 'background' ? 'About Martin' : `the ${NAMES[next.project]} story` })));
    }
    log.append(item);
    log.scrollTop = log.scrollHeight;
    return item;
  };

  const starters = () => {
    const box = el('div', { className: 'guide-starters' }, el('p', { textContent: 'I’m the guide to this site: an AI assistant that knows Martin’s projects and background. Ask me anything about them, or start with one of these.' }));
    for (const q of STARTERS) {
      const b = el('button', { type: 'button', className: 'guide-starter', textContent: q });
      b.addEventListener('click', () => ask(q));
      box.append(b);
    }
    log.append(box);
  };

  let busy = false;
  async function ask(question) {
    question = question.trim();
    if (!question || busy) return;
    busy = true;
    send.disabled = true;
    log.querySelector('.guide-starters')?.remove();
    talk.turns.push({ from: 'you', text: question });
    show({ from: 'you', text: question });
    input.value = '';
    const waiting = show({ from: 'guide', kind: 'waiting', text: WAITING[0] });
    let step = 0;
    const tick = setInterval(() => { step = Math.min(step + 1, WAITING.length - 1); waiting.firstChild.textContent = WAITING[step]; }, 2200);
    let reply;
    try {
      const res = await fetch('/api/guide', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'omit',
        body: JSON.stringify({ message: question, ...(talk.id ? { conversation: talk.id } : {}) }),
      });
      reply = await res.json();
      if (!res.ok || typeof reply.text !== 'string') throw new Error('no reply');
    } catch {
      reply = { kind: 'limit', text: 'I can’t answer right now. The project pages have everything I’d draw on.', sources: [], further_reading: null };
    }
    clearInterval(tick);
    waiting.remove();
    if (reply.conversation) talk.id = reply.conversation;
    const turn = { from: 'guide', kind: reply.kind, text: reply.text, sources: reply.sources || [], further_reading: reply.further_reading || null };
    talk.turns.push(turn);
    show(turn);
    keep();
    busy = false;
    send.disabled = false;
    input.focus();
  }

  const open = (yes) => {
    panel.hidden = !yes;
    launcher.hidden = yes;
    launcher.setAttribute('aria-expanded', String(yes));
    talk.open = yes;
    keep();
    if (yes) {
      if (!log.childElementCount) talk.turns.length ? talk.turns.forEach(show) : starters();
      input.focus();
    } else {
      launcher.focus();
    }
  };

  form.addEventListener('submit', (e) => { e.preventDefault(); ask(input.value); });
  input.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); ask(input.value); } });
  launcher.addEventListener('click', () => open(true));
  close.addEventListener('click', () => open(false));
  panel.addEventListener('keydown', (e) => { if (e.key === 'Escape') open(false); });

  document.body.append(launcher, panel);
  if (talk.open) open(true);

  // On the front page, once per tab: say what this is and offer. Never opens by itself.
  if (location.pathname === '/' && !talk.greeted && !talk.open) {
    talk.greeted = true;
    keep();
    const yes = el('button', { type: 'button', className: 'button primary', textContent: 'Ask a question' });
    const no = el('button', { type: 'button', className: 'button', textContent: 'Not now' });
    const hello = el('aside', { className: 'guide-hello' },
      el('p', {}, el('strong', { textContent: 'Hello. ' }), 'I’m the guide to this site: an AI assistant that knows Martin’s projects and background. Have a question about the work here?'),
      el('div', { className: 'guide-hello-actions' }, yes, no));
    hello.setAttribute('aria-label', 'An offer from the guide');
    yes.addEventListener('click', () => { hello.remove(); open(true); });
    no.addEventListener('click', () => hello.remove());
    setTimeout(() => document.body.append(hello), 1500);
  }
})();
