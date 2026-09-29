import { test, expect } from '@playwright/test';

test('switches between Chinese and English without changing the project', async ({ page }, testInfo) => {
  await page.goto('/');
  await expect(page.getByRole('button', { name: '已同步', exact: true })).toBeVisible();
  const source = await page.getByLabel('Go 源码').inputValue();

  const toEnglish = page.getByRole('button', { name: '切换语言' });
  await expect(toEnglish).toHaveText('English');
  await toEnglish.click();
  await expect(page.getByRole('button', { name: 'Change language' })).toHaveText('中文');
  await expect(page.getByRole('button', { name: 'Synced', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open example' })).toBeVisible();
  await expect(page.locator('.operation-node.send')).toContainText('Send');
  await page.locator('.operation-node.send').click();
  await expect(page.getByLabel('Value to send')).toHaveValue('42');
  await expect(page.getByLabel('Go source')).toHaveValue(source);
  await page.getByRole('button', { name: 'Help' }).click();
  await expect(page.getByRole('dialog', { name: 'Help' })).toContainText('From source to graph and back again.');
  await page.getByRole('button', { name: 'Close help' }).click();
  await page.getByRole('button', { name: 'Validate build' }).click();
  await expect(page.locator('.diagnostics-header')).toContainText('Build passed');
  await page.screenshot({ path: testInfo.outputPath('english.png') });

  await page.reload();
  await expect(page.getByRole('button', { name: 'Change language' })).toHaveText('中文');
  await expect(page.getByRole('button', { name: 'Synced', exact: true })).toBeVisible();
  await expect(page.getByLabel('Go source')).toHaveValue(source);

  await page.getByRole('button', { name: 'Change language' }).click();
  await expect(page.getByRole('button', { name: '切换语言' })).toHaveText('English');
  await expect(page.getByRole('button', { name: '已同步', exact: true })).toBeVisible();
  await expect(page.getByLabel('Go 源码')).toHaveValue(source);
});