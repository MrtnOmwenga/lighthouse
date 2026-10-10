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
//
// It can also follow a visitor into a demo. The site's edge adds this script to the demo's page
// (the demo's own code doesn't change), with `data-site` (this site's address) and `data-here`
// (the demo's name). There it asks through /_guide/ask, and its links lead back to the site.
(() => {
  if (window.top !== window) return; // never inside a frame: a demo may show several of itself
  const config = document.currentScript?.dataset ?? {};
  const SITE = config.site || ''; // set only inside a demo
  const HERE = config.here || '';
  const ASK = SITE ? '/_guide/ask' : '/api/guide';

  // Arriving from the site with the assistant in use: the launch page adds "#guide=…" to the
  // demo's address, carrying the switch and the conversation. It is read once and removed.
  const carried = /^#guide=([A-Za-z0-9_-]{2,40})$/.exec(location.hash)?.[1];
  if (carried) {
    try {
      localStorage.setItem('lh_guide', '1');
      if (carried !== 'on' && !sessionStorage.getItem('lh_guide_talk')) sessionStorage.setItem('lh_guide_talk', JSON.stringify({ id: carried, turns: [], open: true, greeted: true, carried: true }));
    } catch { /* storage unavailable: it stays off here */ }
    history.replaceState(history.state, '', location.pathname + location.search);
  }

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
  if (config.css) document.head.append(Object.assign(document.createElement('link'), { rel: 'stylesheet', href: config.css }));

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
  const note = el('p', { className: 'guide-note' }, 'An AI. It answers from Martin’s own notes and code, and says where from. Questions are kept for 90 days. ', el('a', { href: `${SITE}/privacy#assistant`, textContent: 'Privacy' }));
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
    if (SITE) return false; // in a demo, places are on the site: a link there, never a jump
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
    const link = el('a', { href: SITE + route, className: 'guide-place', textContent: label });
    if (SITE) Object.assign(link, { target: '_blank', rel: 'noopener' }); // the demo stays open
    else link.addEventListener('click', (e) => { if (go(route)) e.preventDefault(); });
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
      item.append(el('p', { className: 'guide-next' }, 'Read more: ', el('a', { href: SITE + STORIES[next.project], textContent: next.project === 'background' ? 'About Martin' : `the ${NAMES[next.project]} story` })));
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
    const words = talk.carried
      ? `I’ve come along from Martin’s site, and I still have our conversation. Ask me about anything you see here in ${HERE || 'the demo'}.`
      : SITE
        ? `I’m Martin’s assistant: an AI that knows how ${HERE || 'this demo'} is built. Ask me about anything you see here.`
        : 'I’m Martin’s assistant: an AI that knows his projects, his code and his background closely. Ask me anything about them, or start here.';
    const box = el('div', { className: 'guide-starters' }, el('p', { textContent: words }));
    for (const q of SITE ? [] : STARTERS) {
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
      const res = await fetch(ASK, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, credentials: 'omit',
        body: JSON.stringify({ message: question, ...(talk.id ? { conversation: talk.id } : {}), ...(config.viewing ? { viewing: config.viewing } : {}) }),
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
    const lead = !SITE && turn.kind === 'answer' && innerWidth > 560 && turn.sources.find((s) => typeof s.route === 'string' && PLACE.test(s.route));
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
  if (SITE) return; // what follows is about this site's own pages

  // Going from a launch page into a demo the assistant can follow into: the demo's address
  // carries the switch, and the conversation if there is one. A demo it doesn't follow into says
  // why, once, in this window.
  const launch = document.querySelector('[data-launch]');
  if (launch?.hasAttribute('data-guide-follows')) {
    const carry = (url) => `${url.split('#')[0]}#guide=${talk.id || 'on'}`;
    window.lighthouseGuide = { carry };
    for (const a of document.querySelectorAll('a[data-open], a[data-tour-link]')) a.addEventListener('click', () => { a.href = carry(a.href); });
  }
  const absent = launch?.dataset.guideAbsent;
  if (absent && !talk.turns.some((t) => t.text === absent)) {
    const turn = { from: 'guide', kind: 'note', text: absent, sources: [], further_reading: null };
    talk.turns.push(turn);
    keep();
    if (!panel.hidden) { log.querySelector('.guide-starters')?.remove(); show(turn); }
  }

  // Unasked offers, on a project's page. Two readers are worth interrupting, once, gently:
  // one who is skimming (well down the page soon after arriving, or scrolling fast) is offered the page in
  // three points; one who has stayed on a part is offered that part in plainer words. What is shown
  // was written ahead of time and comes back without any model being asked.
  // One offer a page, none after a "no" in this tab, none while the window is open or someone is typing.
  const project = /^\/projects\/([a-z0-9-]+)$/.exec(location.pathname)?.[1];
  const quiet = () => !panel.hidden || document.querySelector('.guide-hello') || /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName || '');
  if (project && !talk.noOffers && 'IntersectionObserver' in window) {
    const arrived = Date.now();
    let offered = false;
    const SKIM_WITHIN = 45_000, SKIM_DEPTH = 0.6, FAST = 2.5, DWELL_FOR = 25_000;

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

    // Skimming: well down the page soon after arriving, or more than two and a half screens
    // gone by in five seconds at any time.
    const passed = []; // [when, where] of recent scrolls
    addEventListener('scroll', () => {
      const now = Date.now();
      passed.push([now, scrollY]);
      while (now - passed[0][0] > 5000) passed.shift();
      const depth = (scrollY + innerHeight) / document.documentElement.scrollHeight;
      const soon = now - arrived < SKIM_WITHIN && depth > SKIM_DEPTH;
      const fast = scrollY - passed[0][1] > FAST * innerHeight;
      if (soon || fast) offer(`site/${project}`, 'Skimming? I can give you this page in three short points.', 'Show me');
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
      // Someone reading doesn't move: a minute without a movement still counts as there.
      const present = document.visibilityState === 'visible' && Date.now() - lastActive < 60_000;
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
