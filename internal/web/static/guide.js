// Martin's assistant: an AI that answers questions about his projects and background.
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
  const STARTERS = ['What is Martin strongest at?', 'What has he built, and which project should I look at first?', 'How does Redacted keep classified text from readers without clearance?'];
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
  const note = el('p', { className: 'guide-note' }, 'An AI. It answers from Martin’s own notes and code, and says where from. Questions are kept for 90 days. ', el('a', { href: '/privacy#assistant', textContent: 'Privacy' }));
  const panel = el('section', { className: 'guide-panel', hidden: true },
    el('header', {}, el('div', {}, el('strong', { textContent: 'Martin’s assistant' }), el('span', { textContent: 'An AI that knows his work closely' })), close),
    log, form, note);
  panel.setAttribute('role', 'dialog');
  panel.setAttribute('aria-label', 'Martin’s assistant');
  const launcher = el('button', { type: 'button', className: 'guide-launcher', textContent: 'Ask Martin’s assistant' });
  launcher.setAttribute('aria-haspopup', 'dialog');

  // Taking the reader to a heading on one of this site's pages, and marking it for a moment so the
  // eye lands on it. The address is always one of this site's own: a path and an anchor.
  const PLACE = /^\/(?:projects\/[a-z0-9-]+(?:\/architecture)?|about)#[a-z0-9-]+$/;
  const POINT = 'lh_guide_point';
  const point = (id) => {
    const target = document.getElementById(id);
    if (!target) return;
    const calm = matchMedia('(prefers-reduced-motion: reduce)').matches;
    target.scrollIntoView({ behavior: calm ? 'auto' : 'smooth', block: 'start' });
    // On a narrow screen the window would cover what is being pointed at: it steps aside.
    if (innerWidth <= 560 && !panel.hidden) open(false);
    target.classList.add('guide-point');
    // A page that can do more than be scrolled to (an architecture diagram) listens for this.
    target.dispatchEvent(new CustomEvent('guide:point', { bubbles: true }));
    setTimeout(() => target.classList.remove('guide-point'), 6000);
  };
  // Going to a place: on this page it is pointed at; on another, the page is opened and points at
  // it on arrival, with this window still open and the conversation in it.
  const go = (route) => {
    const [path, id] = route.split('#');
    if (path === location.pathname) {
      history.replaceState(history.state, '', route);
      point(id);
      return true;
    }
    try { sessionStorage.setItem(POINT, id); } catch { /* arrives at the top of the page instead */ }
    return false;
  };
  const place = (route, label) => {
    const link = el('a', { href: route, className: 'guide-place', textContent: label });
    link.addEventListener('click', (e) => { if (go(route)) e.preventDefault(); });
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
    // An offer of the CV or a way to write. The addresses are this page's own links, found on the
    // page: the service only ever says which of the two to offer.
    const cv = document.querySelector('.masthead a[href$=".pdf"]');
    const mail = document.querySelector('.foot a[href^="mailto:"]');
    // The CV if that was offered and the site links one; otherwise a way to write.
    const handoff = turn.offer === 'cv' && cv ? [cv, 'Download Martin’s CV'] : turn.offer && mail ? [mail, 'Write to Martin'] : null;
    if (handoff) {
      item.append(el('p', { className: 'guide-next' }, el('a', { href: handoff[0].getAttribute('href'), className: 'button', textContent: handoff[1] })));
    }
    log.append(item);
    log.scrollTop = log.scrollHeight;
    return item;
  };

  const starters = () => {
    const box = el('div', { className: 'guide-starters' }, el('p', { textContent: 'I’m Martin’s assistant: an AI that knows his projects, his code and his background closely. Ask me anything about them, or start here.' }));
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
    const turn = { from: 'guide', kind: reply.kind, text: reply.text, sources: reply.sources || [], further_reading: reply.further_reading || null, offer: reply.offer || null };
    talk.turns.push(turn);
    show(turn);
    keep();
    // An answer takes the reader to where it comes from: the first of its sources that is a place
    // on this site. Not on a narrow screen, where this window would have to close to show it.
    const lead = turn.kind === 'answer' && innerWidth > 560 && turn.sources.find((s) => typeof s.route === 'string' && PLACE.test(s.route));
    if (lead && !go(lead.route)) location.assign(lead.route);
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

  // Unasked offers, on a project's page. Two readers are worth interrupting, once, gently:
  // one who is skimming (most of the page gone by in the first half minute) is offered the page in
  // three points; one who has stayed on a part is offered that part in plainer words. What is shown
  // was written ahead of time and comes back without any model being asked.
  // One offer a page, none after a "no" in this tab, none while the window is open or someone is typing.
  const project = /^\/projects\/([a-z0-9-]+)$/.exec(location.pathname)?.[1];
  const quiet = () => !panel.hidden || document.querySelector('.guide-hello') || /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName || '');
  if (project && !talk.noOffers && 'IntersectionObserver' in window) {
    const arrived = Date.now();
    let offered = false;
    const SKIM_WITHIN = 30_000, SKIM_DEPTH = 0.7, DWELL_FOR = 35_000;

    const fetchNote = async (id) => {
      const res = await fetch('/api/guide', { method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'omit', body: JSON.stringify({ note: id }) });
      const body = await res.json();
      if (!res.ok || !body.note) throw new Error('no note');
      return body.note;
    };
    const offer = (id, words, yesLabel) => {
      if (offered || quiet()) return;
      offered = true;
      const yes = el('button', { type: 'button', className: 'button primary', textContent: yesLabel });
      const no = el('button', { type: 'button', className: 'button', textContent: 'No thanks' });
      const card = el('aside', { className: 'guide-hello' }, el('p', { textContent: words }), el('div', { className: 'guide-hello-actions' }, yes, no));
      card.setAttribute('aria-label', 'An offer from Martin’s assistant');
      no.addEventListener('click', () => { card.remove(); talk.noOffers = true; keep(); });
      yes.addEventListener('click', async () => {
        card.remove();
        open(true);
        log.querySelector('.guide-starters')?.remove();
        let turn;
        try {
          const note = await fetchNote(id);
          const text = note.kind === 'overview'
            ? `This page in three points.\n\nThe problem: ${note.problem}\n\nWhat Martin built: ${note.built}\n\nHow it works: ${note.how}`
            : `“${note.heading}” in plainer words.\n\n${note.text}`;
          turn = { from: 'guide', kind: 'note', text, sources: [], further_reading: null };
        } catch {
          turn = { from: 'guide', kind: 'limit', text: 'I can’t fetch that right now. Ask me anything about this page instead.', sources: [], further_reading: null };
        }
        talk.turns.push(turn);
        show(turn);
        keep();
      });
      document.body.append(card);
    };

    // Skimming: far down the page soon after arriving.
    addEventListener('scroll', () => {
      const depth = (scrollY + innerHeight) / document.documentElement.scrollHeight;
      if (Date.now() - arrived < SKIM_WITHIN && depth > SKIM_DEPTH) offer(`site/${project}`, 'Skimming? I can give you this page in three short points.', 'Show me');
    }, { passive: true });

    // Dwelling: the same part's heading has been the one in view for a while, with the reader still there.
    const parts = ['problem', 'solution', 'decisions', 'testing', 'limits'].map((id) => document.getElementById(id)).filter(Boolean);
    let current = null, since = 0, lastActive = Date.now();
    for (const e of ['scroll', 'pointermove', 'keydown', 'touchstart']) addEventListener(e, () => { lastActive = Date.now(); }, { passive: true });
    const watch = new IntersectionObserver((entries) => {
      for (const entry of entries) if (entry.isIntersecting && entry.target !== current) { current = entry.target; since = Date.now(); }
    }, { rootMargin: '0px 0px -55% 0px' });
    parts.forEach((p) => watch.observe(p));
    setInterval(() => {
      const present = document.visibilityState === 'visible' && Date.now() - lastActive < 20_000;
      if (current && present && Date.now() - since > DWELL_FOR && Date.now() - arrived > SKIM_WITHIN) {
        offer(`site/${project}#${current.id}`, `Want “${current.textContent.trim()}” in plainer words?`, 'Yes, explain it');
      }
    }, 2000);
  }

  // On the front page, once per tab: say what this is and offer. Never opens by itself.
  if (location.pathname === '/' && !talk.greeted && !talk.open) {
    talk.greeted = true;
    keep();
    const yes = el('button', { type: 'button', className: 'button primary', textContent: 'Ask a question' });
    const no = el('button', { type: 'button', className: 'button', textContent: 'Not now' });
    const hello = el('aside', { className: 'guide-hello' },
      el('p', {}, el('strong', { textContent: 'Hello. ' }), 'I’m Martin’s assistant: an AI that knows his projects and background closely. Can I show you around, or answer a question?'),
      el('div', { className: 'guide-hello-actions' }, yes, no));
    hello.setAttribute('aria-label', 'An offer from Martin’s assistant');
    yes.addEventListener('click', () => { hello.remove(); open(true); });
    no.addEventListener('click', () => hello.remove());
    setTimeout(() => document.body.append(hello), 1500);
  }
})();
