import { expect, test } from '@playwright/test';

// The public site's counting script: what a reader goes on to do is recorded by kind, and a tag
// on the link is sent once and then taken out of the address.
test('following a link to GitHub is counted, and a link\'s tag is read and removed', async ({ page }) => {
  await page.route('https://github.com/**', (route) => route.abort());
  const view = page.waitForRequest((r) => r.url().endsWith('/api/a/view'));
  await page.goto('/?ref=acme-12');
  expect(JSON.parse((await view).postData() ?? '{}')).toMatchObject({ path: '/', ref: 'acme-12' });
  await expect(page).toHaveURL(/\/$/);

  const event = page.waitForRequest((r) => r.url().endsWith('/api/a/event'));
  await page.getByRole('link', { name: 'GitHub' }).first().click({ noWaitAfter: true });
  expect(JSON.parse((await event).postData() ?? '{}')).toMatchObject({ name: 'outbound_github' });
});

// The guide: the assistant's window on the public site. Its service is elsewhere, so it is
// stood in for here; what is tested is this site's side of it.
test.describe('the guide', () => {
  const reply = {
    conversation: 'c'.repeat(24), kind: 'answer',
    text: 'Each section is its own document.\n<img src=x onerror="document.title=\'pwned\'">',
    sources: [{ id: 'redacted/05-redaction#why', project: 'redacted', page: 'Sections', heading: 'Why is a section a separate document?' }],
    further_reading: { id: 'redacted/05-redaction#how', project: 'redacted', page: 'Sections', heading: 'How are words hidden?' },
  };

  test('is absent until switched on in this browser, and the switch leaves the address clean', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('button', { name: 'Ask Martin’s assistant' })).toHaveCount(0);
    await page.goto('/?guide=on');
    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByRole('button', { name: 'Ask Martin’s assistant' })).toBeVisible();
    await page.goto('/about?guide=off');
    await expect(page.getByRole('button', { name: 'Ask Martin’s assistant' })).toHaveCount(0);
  });

  test('offers itself once on the front page, answers with its sources, and shows replies as text only', async ({ page }) => {
    const asked: unknown[] = [];
    await page.route('**/api/guide', async (route) => {
      asked.push(route.request().postDataJSON());
      await route.fulfill({ json: reply });
    });
    await page.goto('/?guide=on');
    const offer = page.getByRole('complementary', { name: 'An offer from Martin’s assistant' });
    await expect(offer).toContainText('I’m Martin’s assistant');
    await offer.getByRole('button', { name: 'Ask a question' }).click();

    const panel = page.getByRole('dialog', { name: 'Martin’s assistant' });
    await expect(panel).toContainText('Questions are kept for 90 days');
    await panel.getByLabel('Your question').fill('How does Redacted hide text?');
    await panel.getByLabel('Your question').press('Enter');
    await expect(panel.getByRole('list', { name: 'Where this comes from' })).toContainText('Redacted · Why is a section a separate document?');
    await expect(panel.getByRole('link', { name: 'the Redacted story' })).toHaveAttribute('href', '/projects/redacted');
    // Markup in a reply is shown, not run.
    await expect(panel).toContainText('<img src=x onerror=');
    await expect(panel.locator('img')).toHaveCount(0);
    expect(await page.title()).not.toBe('pwned');
    expect(asked).toEqual([{ message: 'How does Redacted hide text?' }]);

    // The conversation follows the reader to another page, and continues under the same id.
    await panel.getByRole('link', { name: 'the Redacted story' }).click();
    await expect(page).toHaveURL(/\/projects\/redacted$/);
    const again = page.getByRole('dialog', { name: 'Martin’s assistant' });
    await expect(again).toContainText('Each section is its own document.');
    await again.getByLabel('Your question').fill('And the audit log?');
    await again.getByRole('button', { name: 'Ask', exact: true }).click();
    await expect.poll(() => asked.length).toBe(2);
    expect(asked[1]).toEqual({ message: 'And the audit log?', conversation: 'c'.repeat(24) });

    // Closed with Escape; the front page doesn't offer a second time in this tab.
    await again.getByLabel('Your question').press('Escape');
    await expect(again).toBeHidden();
    await page.goto('/');
    await page.waitForTimeout(2000);
    await expect(page.getByRole('complementary', { name: 'An offer from Martin’s assistant' })).toHaveCount(0);
  });

  test('takes the reader to the heading an answer comes from, and marks it; refuses an address that isn\'t this site\'s', async ({ page }) => {
    await page.route('**/api/guide', (route) => route.fulfill({ json: {
      conversation: 'd'.repeat(24), kind: 'answer', text: 'Six decisions shaped it.',
      sources: [
        { id: 'site/redacted#decisions', project: 'redacted', page: 'Redacted', heading: 'Key decisions', route: '/projects/redacted#decisions' },
        { id: 'x', project: 'redacted', page: 'Redacted', heading: 'Elsewhere', route: 'https://evil.example/#x' },
        { id: 'y', project: 'redacted', page: 'Redacted', heading: 'Script', route: 'javascript:alert(1)' },
      ],
      further_reading: { id: 'site/redacted#limits', project: 'redacted', page: 'Redacted', heading: 'What it does not do', route: '/projects/redacted#limits' },
    } }));
    await page.goto('/about?guide=on');
    await page.getByRole('button', { name: 'Ask Martin’s assistant' }).click();
    const panel = page.getByRole('dialog', { name: 'Martin’s assistant' });
    await panel.getByLabel('Your question').fill('What were the key decisions in Redacted?');
    await panel.getByLabel('Your question').press('Enter');
    // The answer takes the reader to the first place it comes from, with the window still open.
    await expect(page).toHaveURL(/\/projects\/redacted#decisions$/);
    const heading = page.locator('#decisions');
    await expect(heading).toHaveClass(/guide-point/);
    await expect(heading).toBeInViewport();
    const sources = page.getByRole('dialog', { name: 'Martin’s assistant' }).getByRole('list', { name: 'Where this comes from' });
    // Only the address on this site became a link; the others are shown as plain words.
    await expect(sources.getByRole('link')).toHaveCount(1);
    await expect(sources).toContainText('Redacted · Elsewhere');

    // Already on that page: the next place is reached without leaving it.
    await page.getByRole('dialog', { name: 'Martin’s assistant' }).getByRole('link', { name: /What it does not do/ }).click();
    await expect(page).toHaveURL(/\/projects\/redacted#limits$/);
    await expect(page.locator('#limits')).toHaveClass(/guide-point/);
  });

  test('a reader who races down a page long after arriving is offered it too; a slow one is left alone', async ({ page }) => {
    await page.clock.install();
    await page.goto('/projects/lighthouse?guide=on');
    await page.clock.fastForward('02:00'); // long past "soon after arriving"
    const offer = page.getByRole('complementary', { name: 'An offer from Martin’s assistant' });
    // One screen at a time, with a pause between: reading, not skimming.
    for (let i = 1; i <= 2; i += 1) {
      await page.evaluate((n) => window.scrollTo(0, n * innerHeight), i);
      await page.clock.fastForward('00:08');
    }
    await expect(offer).toHaveCount(0);
    // Three screens in a moment.
    for (let i = 0; i < 4; i += 1) {
      await page.evaluate(() => window.scrollTo(0, scrollY + innerHeight));
      await page.clock.runFor(200);
    }
    await expect(offer).toContainText('Skimming?');
  });

  test('offers the page in three points to a reader who is skimming, once, and takes no for an answer', async ({ page }) => {
    const asked: unknown[] = [];
    await page.route('**/api/guide', async (route) => {
      asked.push(route.request().postDataJSON());
      await route.fulfill({ json: { kind: 'note', id: 'site/redacted', note: { kind: 'overview', project: 'redacted', route: '/projects/redacted', problem: 'Hidden text must stay hidden.', built: 'An editor that never sends it.', how: 'Each section is its own document.' } } });
    });
    await page.goto('/projects/redacted?guide=on');
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
    const offer = page.getByRole('complementary', { name: 'An offer from Martin’s assistant' });
    await expect(offer).toContainText('Skimming?');
    await offer.getByRole('button', { name: 'Show me' }).click();
    const panel = page.getByRole('dialog', { name: 'Martin’s assistant' });
    await expect(panel).toContainText('The problem: Hidden text must stay hidden.');
    await expect(panel).toContainText('How it works: Each section is its own document.');
    expect(asked).toEqual([{ note: 'site/redacted' }]); // written ahead of time: no question was sent to a model

    // One offer a page; and after a "no" on another page, none for the rest of the tab.
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
    await expect(offer).toHaveCount(0);
    await page.getByRole('dialog', { name: 'Martin’s assistant' }).getByRole('button', { name: 'Close' }).click();
    await page.goto('/projects/ghostchat');
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
    await offer.getByRole('button', { name: 'No thanks' }).click();
    await page.goto('/projects/lighthouse');
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
    await page.waitForTimeout(1500);
    await expect(offer).toHaveCount(0);
  });

  test('offers a way to reach him, with this site\'s own links, when the service says a visitor is weighing him up', async ({ page }) => {
    await page.route('**/api/guide', (route) => route.fulfill({ json: { conversation: 'e'.repeat(24), kind: 'answer', text: 'Two years as tech lead.', sources: [], further_reading: null, offer: 'cv' } }));
    await page.goto('/about?guide=on');
    await page.getByRole('button', { name: 'Ask Martin’s assistant' }).click();
    const panel = page.getByRole('dialog', { name: 'Martin’s assistant' });
    await panel.getByLabel('Your question').fill('What is his experience?');
    await panel.getByLabel('Your question').press('Enter');
    await expect(panel).toContainText('Two years as tech lead.');
    // The CV when the site links one, otherwise his email: either way an address taken from this page.
    const cvLink = page.locator('.masthead a[href$=".pdf"]');
    const cv = (await cvLink.count()) ? await cvLink.getAttribute('href') : null;
    const mail = await page.locator('.foot a[href^="mailto:"]').first().getAttribute('href');
    const link = panel.getByRole('link', { name: cv ? 'Download Martin’s CV' : 'Write to Martin' });
    await expect(link).toHaveAttribute('href', cv ?? mail ?? 'missing');
  });

  test('when its service is away, it says so and the page carries on', async ({ page }) => {
    await page.route('**/api/guide', (route) => route.abort());
    await page.goto('/projects?guide=on');
    await page.getByRole('button', { name: 'Ask Martin’s assistant' }).click();
    const panel = page.getByRole('dialog', { name: 'Martin’s assistant' });
    await panel.getByRole('button', { name: 'What is Martin strongest at?' }).click();
    await expect(panel).toContainText('I can’t answer right now');
    await expect(page.getByRole('link', { name: 'Projects' }).first()).toBeVisible();
  });
});

// The assistant across the demos: it follows a visitor into Redacted, and says why it stays out of
// GhostChat.
test.describe('the guide and the demos', () => {
  test('a launch page hands the demo the switch and the conversation; GhostChat\'s says why it gets neither', async ({ page }) => {
    await page.route('**/api/projects/redacted/ready', (route) => route.fulfill({ json: { ready: true } }));
    await page.route('**/api/guide', (route) => route.fulfill({ json: { conversation: 'c'.repeat(24), kind: 'answer', text: 'It is a demo of access control.', sources: [], further_reading: null } }));
    await page.goto('/go/redacted?guide=on');
    const open = page.locator('.live-band').getByRole('link', { name: 'Open Redacted →' });
    await expect(open).toBeVisible();
    const demo = await page.locator('[data-launch]').getAttribute('data-demo');
    await page.route(`${demo}/**`, (route) => route.fulfill({ contentType: 'text/html', body: '<title>the demo</title>' }));

    await page.getByRole('button', { name: 'Ask Martin’s assistant' }).click();
    const panel = page.getByRole('dialog', { name: 'Martin’s assistant' });
    await panel.getByLabel('Your question').fill('What is Redacted?');
    await panel.getByLabel('Your question').press('Enter');
    await expect(panel).toContainText('It is a demo of access control.');
    await open.click();
    await expect(page).toHaveURL(`${demo}/#guide=${'c'.repeat(24)}`);

    await page.goto('/go/ghostchat');
    await expect(page.getByRole('dialog', { name: 'Martin’s assistant' })).toContainText('I don\'t follow you into GhostChat, and that is on purpose.');
    await expect(page.locator('[data-launch]')).not.toHaveAttribute('data-guide-follows', /.*/);
  });

  test('inside a demo it carries the conversation on, asks through the demo\'s own address, and links back to the site', async ({ page, baseURL }) => {
    const asked: unknown[] = [];
    await page.route('http://demo.test/**', async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname === '/_guide/ask') {
        asked.push(route.request().postDataJSON());
        return route.fulfill({ json: {
          conversation: 'c'.repeat(24), kind: 'answer', text: 'The bars are sent in place of the text.',
          sources: [{ id: 'site/redacted#solution', project: 'redacted', page: 'Redacted', heading: 'How it works', route: '/projects/redacted#solution' }], further_reading: null,
        } });
      }
      // As the edge does: the assistant's files come from the site, under the demo's own address.
      if (url.pathname === '/_guide/guide.js') return route.fulfill({ contentType: 'text/javascript; charset=utf-8', body: await (await page.request.get('/static/guide.js')).text() });
      if (url.pathname === '/_guide/guide.css') return route.fulfill({ contentType: 'text/css', body: await (await page.request.get('/static/guide.css')).text() });
      // What the edge sends for a demo's page: the demo's own markup, and one script tag.
      return route.fulfill({ contentType: 'text/html', body: `<!doctype html><meta charset="utf-8"><title>the demo</title><body><div id="root">demo</div><script src="/_guide/guide.js" data-css="/_guide/guide.css" data-site="${baseURL}" data-here="Redacted" data-viewing="redacted/07-demo#what-does-a-visitor-see" defer></script></body>` });
    });
    // Without the switch it does nothing at all.
    await page.goto('http://demo.test/');
    await expect(page.getByRole('button', { name: 'Ask Martin’s assistant' })).toHaveCount(0);

    await page.goto(`http://demo.test/?tour=play#guide=${'c'.repeat(24)}`);
    await expect(page).toHaveURL('http://demo.test/?tour=play'); // read once, then gone from the address
    const panel = page.getByRole('dialog', { name: 'Martin’s assistant' });
    await expect(panel).toContainText('I’ve come along from Martin’s site');
    await expect(panel).toContainText('here in Redacted');
    await panel.getByLabel('Your question').fill('Why did the text turn into bars?');
    await panel.getByLabel('Your question').press('Enter');
    await expect(panel).toContainText('The bars are sent in place of the text.');
    expect(asked).toEqual([{ message: 'Why did the text turn into bars?', conversation: 'c'.repeat(24), viewing: 'redacted/07-demo#what-does-a-visitor-see' }]);
    // A place on the site is a link that opens beside the demo, never a jump away from it.
    const source = panel.getByRole('link', { name: 'Redacted · How it works' });
    await expect(source).toHaveAttribute('href', `${baseURL}/projects/redacted#solution`);
    await expect(source).toHaveAttribute('target', '_blank');
    await expect(page).toHaveURL('http://demo.test/?tour=play');
    await expect(panel.getByRole('link', { name: 'Privacy' })).toHaveAttribute('href', `${baseURL}/privacy#assistant`);
  });
});

// A project's architecture page: parts in lanes, wires drawn between them, and a walk-through
// that lights one connection at a time.
test.describe('the architecture page', () => {
  const address = '/projects/redacted/architecture';

  test('draws the connections, and a chosen part says what it does and where its code is', async ({ page }) => {
    await page.goto(address);
    const diagram = page.locator('#diagram');
    await expect(diagram.locator('.arch-wire')).toHaveCount(12);
    const now = page.locator('#arch-now');

    await diagram.getByRole('button', { name: 'Collaboration server' }).click();
    await expect(now).toContainText('re-checks every connection when access changes');
    await expect(now.getByRole('link', { name: 'src/realtime/realtime.service.ts' })).toHaveAttribute('href', /github\.com\/MrtnOmwenga\/RBAC-API\/blob\/HEAD\/src\/realtime\/realtime\.service\.ts$/);
    await expect(now).toContainText('← Notifications: every instance hears');
    await expect(now.getByRole('link', { name: /More about this in the story/ })).toHaveAttribute('href', '/projects/redacted#decision-fail-closed-on-permission-changes');
    // Its own connections are lit and labelled; a part it doesn't touch steps back.
    await expect(diagram.locator('.arch-wire.on')).toHaveCount(6);
    await expect(page.locator('#part-realtime')).toHaveAttribute('aria-pressed', 'true');
    await expect(page.locator('#part-audit')).not.toHaveClass(/near|sel/);

    await page.keyboard.press('Escape');
    await expect(now).toContainText('Select any part of the diagram');
    await expect(diagram.locator('.arch-wire.on')).toHaveCount(0);
  });

  test('walks through a flow a step at a time, by button, by its list and by itself', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto(address);
    const now = page.locator('#arch-now');
    const next = now.getByRole('button', { name: 'Next' });
    await expect(now.getByRole('button', { name: 'Back' })).toBeDisabled();
    await next.click();
    await next.click();
    await next.click();
    await expect(now).toContainText('Step 3 of 7 · REST API → Collaboration server');
    await expect(now).toContainText('Lock first');
    await expect(page.locator('#flow-demote-3')).toHaveAttribute('aria-current', 'step');
    await expect(page.locator('#part-api')).toHaveClass(/sel/);
    await expect(page.locator('#part-realtime')).toHaveClass(/sel/);
    await expect(page.locator('#diagram .arch-wire.live')).toHaveCount(1);
    await expect(page.locator('#diagram .arch-wire.live text')).toHaveText('locks before commit');
    // The diagram and the words about it stay on screen together.
    await expect(page.locator('#diagram')).toBeInViewport();

    await now.getByRole('button', { name: 'Back' }).click();
    await expect(now).toContainText('May the Director do this?');
    await page.getByRole('link', { name: /The editor locks/ }).click();
    await expect(now).toContainText('Step 7 of 7');
    await expect(next).toBeDisabled();

    // Play starts again from the top and moves on without being asked.
    await now.getByRole('button', { name: 'Play' }).click();
    await expect(now).toContainText('Step 1 of 7');
    await expect(now.getByRole('button', { name: 'Pause' })).toBeVisible();
    await expect(now).toContainText('Step 2 of 7', { timeout: 10_000 });
    await now.getByRole('button', { name: 'Pause' }).click();
    await now.getByRole('button', { name: 'Show everything' }).click();
    await expect(page.locator('#diagram')).not.toHaveClass(/focus/);
  });

  test('plays by itself on arrival, round and round, until the reader takes over', async ({ page }) => {
    await page.goto(address);
    const now = page.locator('#arch-now');
    await expect(now).toContainText('Step 1 of 7');
    await expect(now.getByRole('button', { name: 'Pause' })).toBeVisible();
    const top = await page.evaluate(() => scrollY);
    await expect(now).toContainText('Step 3 of 7', { timeout: 15_000 });
    expect(await page.evaluate(() => scrollY)).toBe(top); // playing never moves the reader
    await expect(now).toContainText('Step 7 of 7', { timeout: 30_000 });
    await expect(now).toContainText('Step 1 of 7', { timeout: 10_000 });

    // Any choice of the reader's own stops it: a button, a step, or a part.
    await now.getByRole('button', { name: 'Next' }).click();
    await expect(now.getByRole('button', { name: 'Play' })).toBeVisible();
    await expect(now).toContainText('Step 2 of 7');
    await page.waitForTimeout(3500);
    await expect(now).toContainText('Step 2 of 7');
  });

  test('an address names a part or a step, and the assistant points with the same addresses', async ({ page }) => {
    await page.goto(`${address}#flow-demote-5`);
    await expect(page.locator('#arch-now')).toContainText('Step 5 of 7 · Notifications → Collaboration server');

    await page.route('**/api/guide', (route) => route.fulfill({ json: {
      conversation: 'f'.repeat(24), kind: 'answer', text: 'One file of plain functions decides.',
      sources: [{ id: 'arch/redacted#policy', project: 'redacted', page: 'Redacted architecture', heading: 'Policy', route: `${address}#part-policy` }],
      further_reading: null,
    } }));
    await page.goto('/projects/redacted?guide=on');
    await page.getByRole('button', { name: 'Ask Martin’s assistant' }).click();
    const panel = page.getByRole('dialog', { name: 'Martin’s assistant' });
    await panel.getByLabel('Your question').fill('Where are permissions decided?');
    await panel.getByLabel('Your question').press('Enter');
    await expect(page).toHaveURL(/\/projects\/redacted\/architecture#part-policy$/);
    await expect(page.locator('#part-policy')).toHaveClass(/sel/);
    await expect(page.locator('#arch-now')).toContainText('One file of plain functions');
  });

  test('the story and the launch page carry the same diagram, playing while it is on screen', async ({ page }) => {
    await page.goto('/projects/redacted');
    const now = page.locator('#arch-now');
    // Further down the story: it waits until the reader gets there, and rests when they leave.
    await expect(page.locator('#diagram .arch-wire')).toHaveCount(12);
    await expect(now.getByRole('button', { name: 'Play' })).toBeVisible();
    await page.locator('#diagram').scrollIntoViewIfNeeded();
    await expect(now).toContainText(/Step \d of 7/);
    await expect(now.getByRole('button', { name: 'Pause' })).toBeVisible();
    await page.evaluate(() => scrollTo(0, 0));
    await expect(now.getByRole('button', { name: 'Play' })).toBeVisible();
    // The static figure it replaces is gone, and a part leads to the decision that explains it.
    await expect(page.locator('.arch-fallback')).toBeHidden();
    await page.locator('#part-tables').click();
    await page.getByRole('link', { name: /More about this in the story/ }).click();
    await expect(page).toHaveURL(/#decision-let-the-database-enforce-tenancy$/);
    await expect(page.locator('#decision-let-the-database-enforce-tenancy')).toBeInViewport();

    await page.goto('/go/redacted');
    await expect(page.getByText('How it works', { exact: true })).toBeVisible();
    await expect(page.getByText('What to try', { exact: true })).toBeVisible();
    await expect(page.locator('#diagram .arch-wire')).toHaveCount(12);
    await expect(page.locator('#arch-now')).toContainText(/Step \d of 7/);
  });

  test('every project with a diagram draws all of its connections and walks its flow', async ({ page }) => {
    for (const [slug, wires, steps, first] of [['lighthouse', 11, 10, 'You open a page'], ['ghostchat', 8, 7, 'Keys that never left']] as const) {
      await page.emulateMedia({ reducedMotion: 'reduce' });
      await page.goto(`/projects/${slug}/architecture`);
      await expect(page.locator('#diagram .arch-wire')).toHaveCount(wires);
      await page.locator('#arch-now').getByRole('button', { name: 'Next', exact: true }).click();
      await expect(page.locator('#arch-now')).toContainText(`Step 1 of ${steps}`);
      await expect(page.locator('#arch-now')).toContainText(first);
      await expect(page.locator('#diagram .arch-wire.live')).toHaveCount(1);
    }
    await page.goto('/projects/ghostchat/architecture');
    await expect(page.locator('#arch-now')).toContainText('the walk-through: One message, from the sender to the recipient');
  });

  test('without its script it is still a complete page', async ({ browser }) => {
    const context = await browser.newContext({ javaScriptEnabled: false });
    const page = await context.newPage();
    await page.goto(address);
    await expect(page.locator('#arch-now')).toBeHidden();
    await expect(page.locator('#flow-demote-3')).toContainText('Before anything is saved');
    await expect(page.locator('#part-policy')).toHaveAttribute('href', '#about-policy');
    await expect(page.locator('#about-policy')).toContainText('One file of plain functions');
    // In the story, the plain figure stands in for the diagram.
    await page.goto('/projects/redacted');
    await expect(page.locator('#diagram')).toBeHidden();
    await expect(page.locator('.arch-fallback')).toBeVisible();
    await context.close();
  });
});

// The launch page while a demo wakes: it shows that something is starting, and the button beside
// the status goes straight in the moment the demo answers.
test('the launch page waits visibly, then opens the demo on one click', async ({ page }) => {
  let up = false;
  await page.route('**/api/projects/redacted/ready', (route) => route.fulfill({ json: { ready: up } }));
  await page.goto('/go/redacted');
  const demo = await page.locator('[data-launch]').getAttribute('data-demo');
  await page.route(`${demo}/**`, (route) => route.fulfill({ contentType: 'text/html', body: '<title>the demo</title>' }));
  const status = page.locator('.developing');
  const button = status.getByRole('button');
  await expect(button).toHaveText('Starting…');
  await expect(button).toBeDisabled();
  await expect(status.locator('.starting')).toBeVisible();
  await expect(status).toContainText('Starting Redacted');
  // The last part doesn't claim to be ready, or offer a way in, before the demo is.
  await page.getByRole('button', { name: 'Open it' }).click();
  await expect(page.getByRole('heading', { name: 'Still waking up' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Ready when you are' })).toBeHidden();
  await expect(page.locator('.slide.final').getByRole('link', { name: 'Open Redacted →' })).toBeHidden();

  up = true;
  await expect(button).toHaveText('Open Redacted →', { timeout: 10_000 });
  await expect(button).toBeEnabled();
  await expect(status).toContainText('Redacted is ready.');
  await expect(page.getByRole('heading', { name: 'Ready when you are' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Still waking up' })).toBeHidden();
  await expect(page.locator('.slide.final').getByRole('link', { name: 'Open Redacted →' })).toBeVisible();
  await button.click();
  await expect(page).toHaveTitle('the demo');
});

test('a demo that is slow to wake is called delayed, and only then offered unopened', async ({ page }) => {
  await page.clock.install();
  await page.route('**/api/projects/redacted/ready', (route) => route.fulfill({ json: { ready: false } }));
  await page.goto('/go/redacted');
  const status = page.locator('.developing');
  await expect(status.getByRole('button')).toBeDisabled();
  await page.clock.fastForward('01:05');
  await expect(status).toContainText('DELAYED');
  await expect(status.getByRole('button')).toHaveText('Open anyway');
  await expect(status.getByRole('button')).toBeEnabled();
  await page.getByRole('button', { name: 'Open it' }).click();
  await expect(page.getByRole('heading', { name: 'Taking longer than usual' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Try opening it anyway' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Ready when you are' })).toBeHidden();
});


// Accessibility, checked by machine (axe) where a machine can: names, roles, contrast, focus order.
// The assistant's window and the diagrams are the parts added last and the most interactive.
test.describe('accessibility', () => {
  const check = async (page: import('@playwright/test').Page, include?: string) => {
    const { default: AxeBuilder } = await import('@axe-core/playwright');
    let axe = new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']);
    if (include) axe = axe.include(include);
    const { violations } = await axe.analyze();
    expect(violations.map((v) => `${v.id}: ${v.help} (${v.nodes.length}) ${v.nodes[0]?.target} ${v.nodes[0]?.failureSummary ?? ""}`)).toEqual([]);
  };

  test('the assistant\'s window, open and with an answer in it', async ({ page }) => {
    await page.route('**/api/guide', (route) => route.fulfill({ json: {
      conversation: 'a'.repeat(24), kind: 'answer', text: 'Six decisions shaped it.', offer: 'cv',
      sources: [{ id: 'site/redacted#decisions', project: 'redacted', page: 'Redacted', heading: 'Key decisions', route: '/about#x' }], further_reading: null,
    } }));
    await page.goto('/about?guide=on');
    await page.getByRole('button', { name: 'Ask Martin’s assistant' }).click();
    const panel = page.getByRole('dialog', { name: 'Martin’s assistant' });
    await check(page, '.guide-panel');
    await panel.getByLabel('Your question').fill('What were the key decisions?');
    await panel.getByLabel('Your question').press('Enter');
    await expect(panel).toContainText('Six decisions shaped it.');
    await check(page, '.guide-panel');
    // Usable by keyboard alone: Escape closes it and gives the focus back to what opened it.
    await page.keyboard.press('Escape');
    await expect(page.getByRole('button', { name: 'Ask Martin’s assistant' })).toBeFocused();
  });

  test('a diagram: at rest, with a part chosen, and mid walk-through, wide and narrow', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    for (const width of [1280, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/projects/redacted/architecture');
      await check(page, 'main');
      await page.locator('#part-realtime').click();
      await check(page, '.arch-stage');
      await page.locator('#arch-now').getByRole('button', { name: 'Next', exact: true }).click();
      await check(page, '.arch-stage');
    }
  });

  test('the launch page, waiting and ready', async ({ page }) => {
    let up = false;
    await page.route('**/api/projects/redacted/ready', (route) => route.fulfill({ json: { ready: up } }));
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto('/go/redacted');
    await check(page, 'main');
    up = true;
    await expect(page.locator('.developing').getByRole('button')).toHaveText('Open Redacted →', { timeout: 10_000 });
    await check(page);
  });
});
