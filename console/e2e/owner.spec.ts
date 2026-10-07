import { expect, test } from '@playwright/test';

// The owner's screens, end to end, with the development sign-in the test stack enables.
async function signIn(page: import('@playwright/test').Page) {
  await page.goto('/console/');
  await page.getByRole('button', { name: 'Development sign-in' }).click();
  await expect(page).toHaveURL(/\/console\/monitors$/);
}

test('the owner adds, tests, edits and pauses an HTTP monitor', async ({ page, baseURL }) => {
  await signIn(page);
  const name = `Lighthouse itself ${Date.now()}`;

  // An empty form is refused, with the reason beside the field.
  await page.getByRole('button', { name: 'Add a monitor' }).click();
  await page.getByRole('button', { name: 'Add and check' }).click();
  await expect(page.locator('#err-name')).toHaveText('1 to 100 characters.');

  await page.getByLabel('Name').fill(name);
  await page.getByLabel('Kind').selectOption('http');
  await page.getByLabel('URL').fill(`${baseURL}/healthz`);
  await page.getByText('When it counts as down').click();
  await page.getByLabel('Allow a private or internal address').check();
  await page.getByRole('button', { name: 'Test these settings' }).click();
  await expect(page.getByRole('status')).toContainText('Passed with status 200');

  // Saved, and checked at once: no waiting for the schedule.
  await page.getByRole('button', { name: 'Add and check' }).click();
  const row = page.getByRole('row', { name: new RegExp(name) });
  await expect(row).toContainText('● Up');

  await row.getByRole('link', { name }).click();
  await page.getByRole('button', { name: 'Edit' }).click();
  await page.getByLabel('Check every (seconds)').fill('120');
  await page.getByRole('button', { name: 'Save and check' }).click();
  await expect(page.locator('.byline')).toContainText('every 120 s');

  await page.getByRole('button', { name: 'Edit' }).click();
  await page.getByLabel("Paused: don't check it").check();
  await page.getByRole('button', { name: 'Save and check' }).click();
  await expect(page.locator('.byline')).toContainText('Paused');
  await expect(page.getByRole('button', { name: 'Check now' })).toBeDisabled();
});

test('the owner opens an incident by hand and resolves it', async ({ page }) => {
  await signIn(page);
  await page.goto('/console/incidents');
  await page.getByRole('button', { name: 'Open an incident' }).click();
  const title = `Payments are slow ${Date.now()}`;
  await page.getByLabel('Title').fill(title);
  await page.getByLabel('First update').fill('Looking into the provider.');
  await page.getByRole('button', { name: 'Open', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(title);
  await expect(page.getByText('Looking into the provider.')).toBeVisible();
  await page.getByRole('button', { name: 'resolved' }).click();
  await expect(page.locator('.byline .pill.resolved')).toBeVisible();
});

test('the readers report, and marking this browser as the owner\'s', async ({ page }) => {
  await signIn(page);
  await page.getByRole('link', { name: 'Readers' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Who read what');
  await expect(page.getByText('Engaged readers')).toBeVisible();
  const mark = page.getByLabel("Don't count visits from this browser");
  await mark.check();
  await page.reload();
  await expect(page.getByLabel("Don't count visits from this browser")).toBeChecked();
});

test('a session that ends sends the visitor back to the start, and says why', async ({ page, context }) => {
  await page.goto('/console/');
  await page.getByRole('button', { name: 'Start a sandbox →' }).click();
  await expect(page.getByRole('row', { name: /Checkout API/ })).toBeVisible();
  await context.clearCookies();
  await expect(page.getByRole('status')).toContainText('Your sandbox has ended', { timeout: 30_000 });
  await expect(page.getByRole('button', { name: 'Start a sandbox →' })).toBeVisible();
});

test('a sandbox can be put back to how it started', async ({ page }) => {
  await page.goto('/console/');
  await page.getByRole('button', { name: 'Start a sandbox →' }).click();
  page.on('dialog', (d) => d.accept());
  await page.getByRole('row', { name: /Storefront/ }).getByRole('button', { name: /Delete/ }).click();
  await expect(page.getByRole('row', { name: /Storefront/ })).toHaveCount(0);
  await page.getByRole('button', { name: 'Start over' }).click();
  await expect(page.getByRole('row', { name: /Storefront/ })).toBeVisible();
});

test('the owner sees his sessions and sign-ins, and can sign out everywhere else', async ({ page, browser }) => {
  await signIn(page);
  const elsewhere = await browser.newPage();
  await signIn(elsewhere);

  await page.getByRole('link', { name: 'Security' }).click();
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Who is signed in, and who has tried');
  await expect(page.getByText('This session')).toBeVisible();
  await expect(page.getByRole('cell', { name: 'Signed in' }).first()).toBeVisible();

  page.on('dialog', (d) => d.accept());
  await page.getByRole('button', { name: 'Sign out everywhere else' }).click();
  await expect(page.getByRole('button', { name: 'Sign out everywhere else' })).toHaveCount(0);

  // The other browser finds out on its next request, and is returned to the start.
  await expect(elsewhere.getByRole('status')).toContainText('Your session has ended', { timeout: 30_000 });
});
