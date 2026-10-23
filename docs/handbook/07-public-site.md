---
title: "The public site"
project: lighthouse
topics: [server-side-rendering, html-template, yaml, content, status-page, caching, svg, progressive-enhancement, cold-start, singleflight]
sources:
  - internal/site/site.go
  - internal/site/ready.go
  - internal/status/status.go
  - internal/status/sparkline.go
  - internal/web/site.go
  - internal/web/pages.go
  - internal/web/templates/layout.html
  - internal/web/static/launch.js
  - internal/web/server.go
  - deploy/site/site.yaml
  - Dockerfile
  - internal/web/statuscache.go
  - internal/web/static/status.js
  - deploy/cloudrun/edge/worker.js
verified: 2026-10-07
---

# The public site

The pages anyone can read: the front page, the projects and their stories, the about page, the
systems (status) page, and the launch pages that open a demo. They are laid out like a newspaper,
and the figures on them come from Lighthouse's own checks.

## The words used on this page

- **Template:** a page with blanks; the server fills the blanks with data and sends the result.
- **Escaping:** turning characters that mean something in HTML (`<`, `"`) into harmless text, so
  data can't become markup or script.
- **Progressive enhancement:** the page works as plain HTML, and JavaScript only adds comfort on
  top.
- **Cold start:** the wait while a sleeping service starts up for its first request.
- **Cache-Control:** a header telling browsers and caches how long they may reuse a response.
- **SVG:** a picture described as shapes in text, which the browser draws.

## How is a page produced?

Go's `html/template` fills templates that are embedded in the binary (`templates/*.html`). A
handler gathers the data, and `render` (`pages.go`) fills the template **into a buffer first** and
only then sends it. If the template fails halfway, the visitor gets a clean error page, never half
a page with a 200 status.

`html/template` escapes every value according to where it lands (text, an attribute, a link), so
content can't inject markup. One value is deliberately not escaped: the response-time chart, which
the server builds itself out of numbers only (`sparkline.go`).

## Where does the content come from?

Not from the database and not from the code: from a folder of YAML files (`internal/site/site.go`).

```
site.yaml            the profile and the list of projects
stories/<slug>.yaml  one long write-up per project
media/               the portrait and the CV
```

- The folder ships inside the container image (`COPY deploy/site /site` in the Dockerfile), so the
  content is versioned, reviewed and released exactly like code.
- It is **validated at startup**, strictly: an unknown key is an error (typos surface), every
  problem is listed at once, links must be real `http(s)` addresses, and a named file must exist.
  A mistake stops the release at the smoke test, not on a visitor's screen.
- A story has a fixed shape: the problem, the solution, **decisions** (the choice, why, and what
  it costs), testing, and limits. The validator refuses a story without a problem, a solution and
  at least one decision.
- A test loads the shipped content and renders every page (`TestTheShippedSiteIsValid`,
  `TestTheShippedSiteRenders`).

## How do live figures get onto the pages?

Each project in `site.yaml` can name a monitor by its slug. When a page is built, the server builds
the public status (`status.Build`) and joins the two (`cards` in `internal/web/site.go`): the
project's card then shows "● Live · 99.98% uptime" or "● Down". So the portfolio's claims about
its projects are measurements, not text someone typed.

The public status is built once and shared by every page for a few seconds (`statuscache.go`),
and concurrent requests share one build. If it can't be rebuilt (the database is unreachable),
the last one built is used; with none at all, the portfolio pages **still render**, without the
figures. The reading matter doesn't depend on the database.

## What does the status page show, and how is it worked out?

`status.Build` runs inside the owner's tenant and reads only **public** monitors, public incidents
and public updates:

- uptime over 24 hours, 7 days and 90 days, and median and 95th-percentile response times;
- one bar per day for 90 days (green from 99.5%, amber from 95%, red below, grey for no data);
- a response-time chart for the last 24 hours;
- ongoing incidents, and those resolved in the last 14 days.

The overall line: every monitor down is a **major outage**; any monitor down or any open incident
is **degraded**; otherwise **operational**.

Two details:

- **Uptime is rounded down** to two decimals (`Percent`), so 99.999% shows as 99.99%, never as
  100%. Only a perfect record displays as perfect.
- **A private incident looks exactly like a missing one** on the public incident page: same 404,
  same words.

When the status can't be rebuilt, the page shows the last one that was, with a line saying its
figures couldn't be refreshed and when they were measured. It answers 503 only if nothing has
been built since the instance started.

The chart is an SVG drawn on the server, so the status page works without JavaScript. With it, a
small script refreshes the figures in place every minute while the tab is visible; without it,
the page reloads itself. The same
data is available as JSON at `/api/status`, readable from any site.

## What happens when a visitor opens a demo?

The demos sleep when idle, and waking one takes seconds. Sending a visitor straight to a blank,
loading tab would lose them. The launch page (`/go/<slug>`) uses the wait:

1. The page shows a short introduction to the project, a few slides that advance by themselves.
2. Meanwhile `launch.js` asks `/api/projects/<slug>/ready` every 1.5 seconds. Each question makes
   Lighthouse request the demo's health address, and that request is what wakes the demo.
3. When the demo answers, the page says it's ready and opens it once the introduction ends, or at
   once if the visitor skips. After three minutes it says so and offers to open anyway.

Without JavaScript the same page shows every slide in order and a plain "Open" link.

**Why can't a crowd turn this into an attack on the demos?** Anyone can ask "is it ready?", so
`Readiness` (`ready.go`) keeps each answer for two seconds and makes simultaneous questions about
one project share **one** probe (`singleflight`). A thousand visitors asking at once cause one
request to the demo, not a thousand.

## How is caching used?

In three layers.

**Browsers** are told how long they may reuse a response:

| What | Browser may reuse it for |
|---|---|
| Front page, projects | 30 seconds |
| A story | 60 seconds |
| Status page and `/api/status` | 15 seconds |
| About | 5 minutes |
| Stylesheet and scripts, by fingerprinted address; fonts | 1 year |
| Portrait, CV | 1 day |
| "Is it ready?" | Never |

**The edge** keeps a copy of each public page: served for a minute without waking the app, and
served as a marked "saved copy" for up to a day when the site isn't responding (see the Cloud Run
and edge page).

**The server** builds the public status once every few seconds, not once per page.

Pages reference their stylesheet and scripts with a fingerprint of the contents
(`/static/style.css?v=349ae5…`, the `asset` template function), so they can be kept for a year and
a new version is still picked up the moment it ships. Nothing personalised is ever marked
cacheable.

## How are the pages worded?

Headlines say what a thing is; the newspaper-style teaser sits in the small line above. The front
page leads with the role and its scope, then six delivered outcomes and four working habits,
ahead of any list of tools.

That order follows an analysis of about 15,000 remote job postings: communication, customers,
shipping, leading people, testing and ownership each appear in more postings than any technology.
The earlier wording led with a stack list and headlines like "Exposed: inside the intelligence
agency's briefing room", which were memorable and didn't say what the project was; in the first
week most visitors left from the front page.

## What does a shared link look like?

Every public page carries Open Graph tags (a title, a description, a 1200×630 preview image) and
its canonical address, so a link pasted into LinkedIn, Slack or a chat shows a card. There is a
`robots.txt` (the console, the API and the launch pages, which wake demos, are excluded) and a
sitemap.

## What else is worth knowing?

- **Fonts are served from the site itself,** not from a font service, so no visitor's address is
  sent to a third party and the strict Content-Security-Policy needs no exceptions.
- **The only scripts** are the analytics script (39 lines) and the launch page's. Everything else
  is HTML and CSS.
- **Accessibility:** a skip link, `aria-current` on the navigation, status as words plus a symbol,
  charts with text labels, and reduced-motion respected.

## Known gaps

- **Changing a word needs a release.** Content ships in the image, which keeps it reviewed and
  versioned, and means a typo fix runs the whole pipeline.
- **One preview image for every page.** Each project's link shows the same card.
- **A saved copy can be up to a day old.** It says so at the top of the page.
- **Readiness answers are remembered per instance,** in memory.
- **No dark colour scheme.**
- **Whether the new wording works isn't measured yet.** It needs a few weeks of clean numbers:
  what share of engaged readers go on from the front page to a project or a demo.

## Questions and answers

**Why server-rendered templates rather than a JavaScript framework for the site?**
The pages are documents: read once, shared as links, and they should load fast, work without
JavaScript and be readable by search engines and link previewers. A few hundred lines of templates
do that with nothing to download but HTML and CSS.

**Why keep the content in YAML files rather than in the database or the templates?**
Out of the templates so that writing doesn't mean editing markup; out of the database so that
content is versioned, reviewed and deployed like code, with no admin screen to build and secure.
Strict validation at startup catches mistakes before a visitor can see them.

**How does a project card know the project is up?**
The project names a monitor's slug; the page joins the content with the public status built from
that monitor's checks.

**What happens to the front page if the database is down?**
It renders, with the last figures built or none. The status page shows the last status built and
says so. If the whole app is down, the edge serves its last copy of each page, marked as saved.

**Why does uptime round down?**
So an imperfect record never displays as 100%. Rounding to nearest would turn 99.996% into
"100.00%", which would be a false claim on a page whose whole point is honest figures.

**Why a launch page instead of linking straight to the demo?**
The demo may be asleep and take several seconds to start. The launch page wakes it and uses the
wait to explain what the visitor is about to see.

**What stops the "is it ready?" endpoint being used to hammer a demo?**
Answers are cached for two seconds and concurrent questions share one probe, so the demo sees at
most one health request every two seconds however many people ask.

**Could content inject a script into a page?**
No. The template engine escapes every value for its context, and the Content-Security-Policy
would refuse an inline script anyway. The one unescaped value is a chart built from numbers.
