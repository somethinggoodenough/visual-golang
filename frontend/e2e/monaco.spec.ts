import { test, expect } from '@playwright/test';
import { fillSource, sourceValue } from './source-editor';

async function boot(page: import('@playwright/test').Page) {
  await page.goto('/');
  await expect(page.locator('.operation-node.send')).toContainText('42');
}

test('gofmt is undoable, stays a draft, and Ctrl+Enter applies the result', async ({ page }) => {
  await boot(page);
  const compact = 'package main\r\nimport "fmt"\r\n// 中文😀\r\nfunc main(){fmt.Println(7)}';
  const imported = page.waitForResponse(response => response.url().endsWith('/api/import'));
  await page.getByLabel('导入 Go 文件').setInputFiles({ name: 'main.go', mimeType: 'text/plain', buffer: Buffer.from(compact) });
  expect((await (await imported).json()).status).toBe('editable');
  await expect.poll(() => sourceValue(page)).toBe(compact);
  const formatted = page.waitForResponse(response => response.url().endsWith('/api/format'));
  await page.locator('.detail-panel .monaco-editor textarea').focus();
  await page.locator('.detail-panel .monaco-editor textarea').press('Shift+Alt+f');
  const result = await (await formatted).json();
  expect(result.status).toBe('ok');
  await expect.poll(() => sourceValue(page)).toBe(result.source);
  await expect(page.locator('.operation-node.print')).toContainText('7');
  await expect(page.getByRole('button', { name: '应用代码修改', exact: true })).toBeVisible();
  const editor = page.locator('.detail-panel .monaco-editor textarea');
  await editor.press('ControlOrMeta+z');
  await expect.poll(() => sourceValue(page)).toBe(compact);
  await editor.press('ControlOrMeta+Shift+z');
  await expect.poll(() => sourceValue(page)).toBe(result.source);
  await editor.press('ControlOrMeta+Enter');
  await expect(page.getByRole('button', { name: '已同步', exact: true })).toBeVisible();
  await expect(page.locator('.operation-node.print')).toContainText('7');
});

test('invalid formatting preserves source; Unicode diagnostic markers target the correct column', async ({ page }) => {
  await boot(page);
  const invalid = 'package main\nfunc main( {';
  await fillSource(page, invalid);
  await page.getByRole('button', { name: '格式化 Go', exact: true }).click();
  await expect(page.locator('.diagnostics-header')).toContainText('格式化失败');
  await expect.poll(() => sourceValue(page)).toBe(invalid);

  const source = 'package main\nfunc main() { /* 中文😀 */ missing() }';
  await fillSource(page, source);
  await page.locator('.detail-panel .monaco-editor textarea').press('ControlOrMeta+Enter');
  await expect(page.locator('.diagnostic.error')).toBeVisible();
  const markers = await page.evaluate(async () => {
    const modulePath = '/src/editor/monaco.ts';
    const { monaco } = await import(modulePath);
    return monaco.editor.getModelMarkers({ owner: 'go-canvas' });
  });
  expect(markers[0].startLineNumber).toBe(2);
  expect(markers[0].startColumn).toBe(source.split('\n')[1].indexOf('missing') + 1);
  await expect(page.locator('.operation-node.send')).toContainText('42');
});

test('a late formatting response cannot replace newly typed code', async ({ page }) => {
  await boot(page);
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  let received!: () => void;
  const pending = new Promise<void>(resolve => { received = resolve; });
  await page.route('**/api/format', async route => {
    const response = await route.fetch();
    received(); await gate; await route.fulfill({ response });
  });
  await fillSource(page, 'package main\nfunc main(){return}');
  await page.getByRole('button', { name: '格式化 Go', exact: true }).click();
  await pending;
  const newer = 'package main\nfunc main(){ /* newer */ }';
  await fillSource(page, newer);
  const done = page.waitForResponse(response => response.url().endsWith('/api/format'));
  release(); await done;
  await expect.poll(() => sourceValue(page)).toBe(newer);
});

test('bracket completion, find/replace and graph-draft read-only mode work', async ({ page }) => {
  await boot(page);
  await fillSource(page, '');
  await page.keyboard.type('(');
  await expect.poll(() => sourceValue(page)).toBe('()');
  await fillSource(page, 'package main\nfunc main(){ /* alpha */ }');
  const editor = page.locator('.detail-panel .monaco-editor textarea').first();
  await editor.press('ControlOrMeta+h');
  const find = page.locator('.find-widget');
  await expect(find).toBeVisible();
  await find.getByRole('textbox', { name: 'Find', exact: true }).fill('alpha');
  await find.getByRole('textbox', { name: 'Replace', exact: true }).fill('beta');
  await find.getByRole('button', { name: /Replace All/ }).click();
  await expect.poll(() => sourceValue(page)).toContain('beta');
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: '放弃草稿', exact: true }).click();
  await page.locator('.operation-node.send').click();
  await page.getByLabel('发送值', { exact: true }).fill('8');
  await page.getByRole('button', { name: '源码', exact: true }).click();
  await expect(page.getByRole('button', { name: '格式化 Go', exact: true })).toBeDisabled();
  const before = await sourceValue(page);
  await editor.focus();
  await page.keyboard.type('should not insert');
  await expect.poll(() => sourceValue(page)).toBe(before);
});
