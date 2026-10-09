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
    await expect(page.getByRole('button', { name: 'Ask the guide' })).toHaveCount(0);
    await page.goto('/?guide=on');
    await expect(page).toHaveURL(/\/$/);
    await expect(page.getByRole('button', { name: 'Ask the guide' })).toBeVisible();
    await page.goto('/about?guide=off');
    await expect(page.getByRole('button', { name: 'Ask the guide' })).toHaveCount(0);
  });

  test('offers itself once on the front page, answers with its sources, and shows replies as text only', async ({ page }) => {
    const asked: unknown[] = [];
    await page.route('**/api/guide', async (route) => {
      asked.push(route.request().postDataJSON());
      await route.fulfill({ json: reply });
    });
    await page.goto('/?guide=on');
    const offer = page.getByRole('complementary', { name: 'An offer from the guide' });
    await expect(offer).toContainText('an AI assistant');
    await offer.getByRole('button', { name: 'Ask a question' }).click();

    const panel = page.getByRole('dialog', { name: /The guide/ });
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
    const again = page.getByRole('dialog', { name: /The guide/ });
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
    await expect(page.getByRole('complementary', { name: 'An offer from the guide' })).toHaveCount(0);
  });

  test('when its service is away, it says so and the page carries on', async ({ page }) => {
    await page.route('**/api/guide', (route) => route.abort());
    await page.goto('/projects?guide=on');
    await page.getByRole('button', { name: 'Ask the guide' }).click();
    const panel = page.getByRole('dialog', { name: /The guide/ });
    await panel.getByRole('button', { name: 'What has Martin built?' }).click();
    await expect(panel).toContainText('I can’t answer right now');
    await expect(page.getByRole('link', { name: 'Projects' }).first()).toBeVisible();
  });
});
