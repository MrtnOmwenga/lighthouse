---
title: "Privacy-friendly analytics"
project: lighthouse
topics: [analytics, privacy, hmac, pseudonymisation, gdpr, campaign-tags, engaged-time]
sources:
  - internal/analytics/analytics.go
  - internal/store/analytics.go
  - internal/store/migrations/00002_analytics.sql
  - internal/web/analytics.go
  - internal/web/static/a.js
  - internal/web/templates/privacy.html
verified: 2026-10-06
---

# Privacy-friendly analytics

Lighthouse counts its own readers: which pages and projects are read, for how long, where visitors
came from, and which demos they opened. It does this without cookies, without storing anything in
the browser, without sending data to another company, and without keeping IP addresses.

## The words used on this page

- **Page view:** one page loaded by one visitor.
- **Unique visitor:** one person counted once, however many pages they view.
- **Referrer:** the site a visitor came from, which browsers report.
- **Campaign tag:** a label put on a link by whoever shares it (`?ref=newsletter`), saying which
  link brought the visitor.
- **Salt:** a random secret mixed into a hash so the result can't be reproduced without it.
- **HMAC:** a hash computed with a secret key. Without the key, the output can't be recomputed or
  matched to its input.
- **Pseudonym:** a stand-in identifier that isn't the person's real identity.
- **Engaged time:** how long a page was actually being read, as opposed to left open.

## What is recorded for a visit?

One row per page view (`page_views`, in `00002_analytics.sql`):

| Recorded | Example | Not recorded |
|---|---|---|
| The page, from a fixed list of the site's own pages | `/projects/redacted` | Arbitrary addresses or query strings |
| When | a timestamp | |
| The day's pseudonym for the visitor | 32 hex characters | The IP address, the full browser string |
| Device class | phone, tablet or desktop | Browser version, screen size, anything finer |
| The referring site's name only | `linkedin.com` | The full referring address |
| A campaign tag, if the link had one | `toggl` | |
| Engaged seconds | 95 | |

Plus events on a view: a demo became ready, a demo was opened, the introduction was skipped.

Everything is validated against a fixed shape before it is stored (a known page, a short tag of
letters and digits, a host name), and the database repeats those checks, so free text from a
browser never reaches the tables.

## How can it count unique visitors without cookies?

A cookie would recognise a returning browser by giving it an id to keep. Instead, the server
computes an id from things every request already carries, in `Visitor` (`analytics.go`):

```
pseudonym = HMAC-SHA256( today's salt,  IP address + browser name )
```

- The same visitor gets the same pseudonym on every page they view **today**, so their views can
  be counted as one person and followed across pages.
- The **salt** is 32 random bytes generated in the database for each day
  (`lighthouse_daily_salt`). The application's database role can't read the salts table at all;
  it can only ask for today's salt, and asking deletes older ones.
- When the day ends, that salt is gone. Tomorrow the same visitor gets a different pseudonym, so
  nobody can be followed from day to day, and yesterday's pseudonyms can no longer be linked to an
  address even by someone holding the whole database.
- The IP address is used in memory for this calculation and never written anywhere.

## Why an HMAC with a salt, and not a plain hash of the IP?

There are only about four billion IPv4 addresses. Someone holding a table of plain hashes could
hash every possible address in minutes and look each one up. With a secret salt they can't
compute the hashes at all, and once the salt is deleted, nobody can.

## Who is never counted?

- Browsers that send **Global Privacy Control** or **Do Not Track**: the page's script does
  nothing for them, and the server ignores them if a request arrives anyway.
- **Bots**, recognised by their browser string (crawlers, link previewers, monitoring tools,
  Lighthouse's own probes).
- **The signed-in owner.**

The collection endpoints answer the same way whether or not a visit was counted, so they reveal
nothing about why one was skipped.

## How is reading time measured?

The page sends a small "still here" message every 15 seconds, but only while the tab is visible
and the visitor has scrolled, clicked or typed within the last minute (`a.js`).

The **server** does the arithmetic (`Ping` in `internal/store/analytics.go`): it adds the time
since the previous message, never more than 20 seconds per message and never more than an hour in
total. The browser doesn't report a duration, so a page left open in a background tab accrues
nothing, and nobody can inflate their reading time by sending a large number.

## How do campaign tags work?

A link such as `https://martinomwenga.com/?ref=toggl` carries a tag. On arrival the script reads
the tag, sends it with the page view, and removes it from the address bar, so the tag isn't passed
on if the visitor shares the link.

The owner's report then answers, per tag: how many visitors, when first and last seen, how many
pages, total reading time, **which pages they went on to read**, and how many demos they opened
(`RefStats`). It works by taking each visitor who arrived with the tag and gathering all their
views from that day, through the pseudonym. So one tagged link per job application shows what that
company's reader did, with no personal data involved.

## What can the public see?

The systems page shows, per project, the readers and median reading time over 30 days. Any count
below five is shown only as "fewer than 5" (`MinPublic`), so the public numbers can never reveal
that one particular person visited.

The full report (pages, projects, tags, referrers, devices) is for the signed-in owner only.

## How long is it kept?

Page views and their events are deleted after 90 days, by the same clean-up that prunes old
checks. Salts last one day.

## Is this lawful without a consent banner?

The design follows the approach privacy-focused analytics tools use (Plausible, Fathom), and the
reasoning is:

- **Nothing is stored on, or read from, the visitor's device,** so the "cookie" rules (the EU's
  ePrivacy rules) aren't triggered.
- **No personal data is kept:** the IP address is discarded, and the pseudonym can't be traced
  back or followed past the day.

It is a widely used and well-argued position, not a guarantee: regulators' views on daily-salted
pseudonyms differ by country, and during the day the pseudonym is still pseudonymised personal
data being processed. The privacy page describes exactly what happens, which is the part every
regime requires.

## Known gaps

- **Visitors are undercounted.** Everyone sharing an IP address and browser version (an office, a
  mobile carrier's shared addresses) counts as one person. A person on two networks counts as two.
- **Privacy-minded browsers are invisible.** Brave sends Global Privacy Control by default, and
  Firefox and others do when the setting is on. Those users skew technical, which is this site's
  audience.
- **No returning visitors.** By design, someone who comes back tomorrow is new. "The same reader
  came back three times this week" can't be known. A tagged link still shows visits on each day.
- **Three kinds of event only.** CV downloads, clicks out to GitHub or LinkedIn, and reading a
  story to the end aren't recorded.
- **Bots are recognised by name only,** so a bot pretending to be a browser is counted.
- **It needs JavaScript.** The pages work without it; the counting doesn't.
- **Every counted view writes to the database,** which wakes it if it was asleep.
- **Nothing tells the owner when something interesting happens;** the report has to be opened.

## Questions and answers

**What exactly identifies a visitor?**
For one day, an HMAC of their IP address and browser name under that day's secret salt. Nothing
after that.

**Could the owner find out which IP address read a page?**
No. Addresses are never stored, and the pseudonym can't be reversed, especially once its salt is
deleted at the end of the day.

**Why is the salt kept in the database rather than in the application's memory?**
So that every instance of the application computes the same pseudonym for the same visitor, and a
restart mid-day doesn't split one visitor into two. The application role can't read old salts.

**Why does the server measure reading time, instead of trusting the browser?**
A browser could report anything. The server only accepts "still here" messages and credits at most
20 seconds for each, so the figure can't be inflated and idle tabs count for nothing.

**Why remove `?ref=` from the address bar?**
So the tag describes only the person who clicked the original link. If it stayed, anyone they
forwarded the page to would be counted under the same tag.

**Why hide counts below five?**
With one or two readers, a public number could confirm that a specific person had visited ("I
sent it to them yesterday and the count went from 0 to 1").

**What would have to change to learn more about visitors?**
Anything beyond this (recognising returning visitors, identifying organisations from addresses)
means keeping more about people, so the privacy page must say so, and some of it needs consent in
the EU. The options are listed in the build list.
