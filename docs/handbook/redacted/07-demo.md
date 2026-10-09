---
title: "The demo and its guided tours"
project: redacted
topics: [demo, react, iframes, guided-tour, tiptap, product-thinking, ephemeral-tenants]
sources:
  - src/demo/demo.service.ts
  - src/demo/demo.controller.ts
  - src/demo/briefing.ts
  - web/src/Room.tsx
  - web/src/Pane.tsx
  - web/src/tour.ts
  - web/src/Tour.tsx
  - web/src/automation.ts
  - web/src/session.ts
  - test/browser/redacted.spec.ts
  - test/browser/tour.spec.ts
verified: 2026-10-09
---

# The demo and its guided tours

An access-control API is invisible: its best work is a request that gets "403". *Redacted* is the
part that makes it something a person can watch. Four agents have the same mission briefing open,
and each sees a different document.

## The words used on this page

- **Pane:** one agent's view of the briefing, one of four on screen.
- **iframe:** a web page embedded inside another, with its own script environment.
- **Guided tour:** a sequence of captions that walks a visitor through the demo.
- **Ephemeral:** created for one use and thrown away.

## What does a visitor see?

A fictional intelligence agency, and one document, *Operation NIGHTJAR*, with four sections at
four classification levels and a few words inside them marked higher still
(`src/demo/briefing.ts`).

| Agent | Role | Division | Clearance | So they see |
|---|---|---|---|---|
| The Director | Organization admin | none | Top secret | Everything, and the controls |
| The Analyst | Editor | Operations | Secret | All but the last section, and can write |
| The Intern | Viewer | Operations | Unclassified | One section, with a word barred; the rest as black blocks |
| The Liaison | Editor | Intelligence | Top secret | Nothing at all, until the briefing is shared with them |

The cast is chosen so that each rule has someone it visibly applies to. The Liaison is the
important one: the highest clearance on screen and no access, because clearance and access are
separate questions ([Part 5](05-redaction.md)).

The visitor plays the Director: lower the Analyst's clearance while they type, select words and
classify them, share the briefing with the Liaison, ask any agent "Why can I see this?".

## Is any of it faked?

No, and the design is built to make that checkable.

- **Each visitor gets a real organization** (`DemoService.create`): two departments, four
  members, a project, a document and its sections, created through the same tables and policies as
  any tenant. The characters are ordinary members.
- **Each pane is an iframe** loading the page as one agent, with that agent's own token and its
  own WebSocket connections. The panes share no state in the page; the Intern's pane changes
  because the server told it to. A browser test loads the Intern's pane alone and records every
  byte it receives.
- **Every effect is the real API.** Lowering a clearance is the Director's own `PATCH /members`.

The four tokens are issued when the demo starts and last as long as it does, because the
characters have no passwords to sign in with (each has a hash of a random secret nobody knows).

## What happens to all those organizations?

Each is marked as a demo and deleted after two hours by `demo_cleanup`, a database function: the
application's role can't otherwise delete an organization ([Part 2](02-tenants-and-database.md)).
Deleting the organization takes everything in it. Starting a demo shares the tight rate limit
that sign-in has: ten a minute per address.

## How do the guided tours work?

There are two (`web/src/tour.ts`), chosen on the landing page.

**"Play it for me"** performs seven steps itself, with a caption for each and the relevant panes
highlighted:

1. Four agents, one briefing.
2. The Intern sees black bars.
3. The Analyst starts typing a sentence.
4. The Director lowers the Analyst's clearance: the typing stops mid-sentence and the section
   blacks out.
5. The Director classifies a few words: they turn into a bar on the Intern's screen.
6. The Director shares the briefing: it appears for the Liaison.
7. Everything is in the audit log, and the chain verifies.

**"Guide me"** asks the visitor to do four of those things, and moves on when each has happened.

Three decisions make the tours trustworthy instead of a slideshow:

- **The tour uses the same controls a person would.** Each pane exposes a small set of actions
  (`automation.ts`): type into a section a character at a time, select a phrase and classify it.
  They go through that pane's real editor and its own connection, so the server authorizes them
  like any keystroke. The tour can't do anything the agent couldn't.
- **The typing stops for the real reason.** The automated typing checks before every character
  whether the editor is still editable. When the Director demotes the Analyst, the server locks
  the connection ([Part 6](06-live-collaboration.md)), the editor becomes read-only, and the
  typing stops. Nothing in the tour says "stop here".
- **"Guide me" checks the server, not the click.** A step is done when the *effect* exists: the
  Analyst's clearance really is lower, the share really is in the list, the words really are gone
  from the Intern's page. Clicking the right button isn't enough if nothing changed.

Both tours can be started from a link (`?tour=play`), which is how Lighthouse's launch page opens
the demo straight into one.

## How are the tours tested?

`test/browser/tour.spec.ts`, in a real browser against a real server:

- "Play it for me" runs by itself, and **every effect it narrates really happens**: the test
  checks the server and the panes after each step, not the captions.
- "Guide me" waits for the visitor and moves on when they act.

`test/browser/redacted.spec.ts` covers the demo without the tour, including the test the whole
project rests on: the Intern's browser never receives the text of sections above their clearance.

## What changed after the review?

- **Expired demos are cleared when a new one starts,** as well as on a timer. A timer inside an
  idle serverless instance doesn't fire; a visitor starting a demo is a request, which does.
- **On a phone, the Director sits above one other agent,** chosen with a switcher. All four panes
  stay loaded; the tour switches to the agent each step is about, and its caption sits below the
  panes instead of over the one showing the effect.
- **"Play it for me" waits for each effect** to be on screen before it starts counting, so a slow
  connection can't make a caption move on early.
- **"Is this real?"** A panel where the visitor makes the requests and reads the server's answers
  as they arrived: what the server says each of the four tokens is, the briefing fetched with the
  Intern's token (redacted sections carry a rounded length and nothing else), and a request the
  Intern's page has no control for, refused with a 403 that then shows in the Director's log. Its
  first draft named a codename in its instructions, and the test that scans everything the
  Intern's browser receives failed: the page's own script is something the Intern downloads.
- **The surveillance log lists changes and refusals.** Who opened which classified section is in
  the chain too; the desk leaves those out of its six lines so they don't bury the changes.

## Known gaps

- **The page that hosts the panes can reach into them.** It is the same origin, which is how the
  tour drives them. The isolation that matters is at the network: each pane has its own token and
  connections. A stricter demo would put each pane on its own origin.
- **All four tokens sit in one browser's session storage,** the Director's included, and last the
  whole two hours. Acceptable for a throwaway agency, and not how a real client should hold
  tokens ([Part 3](03-signing-in.md)).
- **It demonstrates one story.** API keys, department admins, auditors and temporary shares are in
  the API and its tests, and nowhere in the demo.

## Questions and answers

**Why build a demo for an API?**
Access control is invisible when it works. A reviewer has ten minutes; a table of roles asks them
to imagine the system, and four panes reacting to one click show it.

**How do you know the demo isn't staged?**
Each pane is a separate page with its own login and connections, every action is a real API call
that the server authorizes, and a browser test records everything one pane receives and checks
the hidden text isn't in it.

**Why iframes?**
To make cheating impossible. If the four views shared one page's memory, the demo could quietly
pass state between them. Separate documents with separate tokens can only learn things from the
server.

**How does the tour type and classify?**
Through actions each pane exposes, which use that pane's own editor and connection. The server
checks them like a person's keystrokes.

**Why does the Analyst's typing stop in step four?**
The typing asks, before each character, whether the editor may still be edited. The demotion
locks the connection on the server, the editor becomes read-only, and the next character isn't
typed.

**Why is the Liaison in the cast?**
To show that clearance isn't access. They are cleared for everything and see nothing until the
document is shared with their division.

**What happens to a visitor's agency?**
It is deleted two hours after it was created, by a database function, with everything in it.
