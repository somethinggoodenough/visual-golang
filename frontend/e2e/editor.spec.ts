import { test, expect, type Page } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import type { Project } from '../src/model/types';

async function boot(page: Page) {
  await page.goto('/');
  await expect(page.getByRole('button', { name: '已同步', exact: true })).toBeVisible();
  await expect(page.locator('.operation-node.send')).toContainText('42');
}
async function applyGraph(page: Page) {
  const response = page.waitForResponse(r => r.url().endsWith('/api/generate'));
  await page.getByRole('button', { name: '应用图形修改', exact: true }).click();
  const result = await (await response).json();
  expect(result.status, JSON.stringify(result.diagnostics)).toBe('ok');
  await expect(page.getByRole('button', { name: '已同步', exact: true })).toBeVisible();
  return result;
}
async function savedProject(page: Page): Promise<Project> {
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: '保存项目', exact: true }).click();
  const file = await download;
  return JSON.parse(await readFile((await file.path())!, 'utf8'));
}
async function importSource(page: Page, source: string) {
  const response = page.waitForResponse(r => r.url().endsWith('/api/import'));
  await page.getByLabel('导入 Go 文件').setInputFiles({ name: 'main.go', mimeType: 'text/plain', buffer: Buffer.from(source) });
  const result = await (await response).json();
  expect(result.status).toBe('editable');
  await expect(page.getByRole('button', { name: '已同步', exact: true })).toBeVisible();
  return result;
}

test('导入 → 42 改为 100 → 真实编译 → Go 往返 → 项目恢复', async ({ page }, testInfo) => {
  const errors: string[] = [];
  page.on('pageerror', error => errors.push(error.message));
  await boot(page);
  await importSource(page, await readFile(path.resolve('../examples/demo.go'), 'utf8'));
  await page.locator('.operation-node.send').click();
  await page.getByLabel('发送值', { exact: true }).fill('100');
  const generated = await applyGraph(page);
  expect(generated.source).toContain('ch <- 100');
  await page.getByRole('button', { name: '源码', exact: true }).click();
  await expect(page.getByLabel('Go 源码')).toHaveValue(generated.source);

  const compiled = page.waitForResponse(r => r.url().endsWith('/api/build'));
  await page.getByRole('button', { name: '编译验证', exact: true }).click();
  expect((await (await compiled).json()).status).toBe('ok');
  await expect(page.locator('.diagnostics-header')).toContainText('编译通过');
  const project = await savedProject(page);
  expect(project.source).toContain('ch <- 100');
  expect(project.sourceHash).toMatch(/^[0-9a-f]{64}$/);

  const goDownload = page.waitForEvent('download');
  await page.getByRole('button', { name: '导出 Go', exact: true }).click();
  const goFile = await goDownload;
  const goText = await readFile((await goFile.path())!, 'utf8');
  expect(goText).toBe(project.source);
  await importSource(page, goText);
  await expect(page.locator('.operation-node.send')).toContainText('100');

  const newResponse = page.waitForResponse(r => r.url().endsWith('/api/import'));
  await page.getByRole('button', { name: '新建', exact: true }).click();
  await newResponse;
  await expect(page.locator('.operation-node')).toHaveCount(0);
  const projectResponse = page.waitForResponse(r => r.url().endsWith('/api/project/validate'));
  await page.getByLabel('打开项目文件').setInputFiles({ name: 'main.goviz.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(project)) });
  expect((await (await projectResponse).json()).status).toBe('ok');
  await expect(page.locator('.operation-node.send')).toContainText('100');
  const restored = await savedProject(page);
  expect(restored.ir).toEqual(project.ir);
  expect(restored.layout).toEqual(project.layout);
  await page.screenshot({ path: testInfo.outputPath('workspace.png'), fullPage: true });
  expect(errors).toEqual([]);
});

test('从空项目添加、连接、排序节点并生成同样的程序', async ({ page }) => {
  await boot(page);
  await page.getByRole('button', { name: '新建', exact: true }).click();
  await expect(page.locator('.operation-node')).toHaveCount(0);
  await expect(page.getByRole('button', { name: '已同步', exact: true })).toBeVisible();
  await page.getByRole('button', { name: '创建 Channel', exact: true }).click();
  await page.getByRole('button', { name: '启动 Goroutine', exact: true }).click();
  await page.getByLabel('目标函数').selectOption({ label: 'goroutine 1' });
  await page.getByRole('button', { name: '发送', exact: true }).click();
  await page.getByLabel('目标函数').selectOption({ label: 'main' });
  await page.getByRole('button', { name: '接收', exact: true }).click();
  await page.getByRole('button', { name: '打印', exact: true }).click();
  await page.locator('.react-flow__controls-fitview').click();

  const sourceHandle = page.locator('.resource-node .react-flow__handle[data-handleid="channel"]');
  const targetHandle = page.locator('.operation-node.send .react-flow__handle[data-handleid="channel"]');
  const from = await sourceHandle.boundingBox(), to = await targetHandle.boundingBox();
  expect(from).not.toBeNull(); expect(to).not.toBeNull();
  await page.mouse.move(from!.x + from!.width / 2, from!.y + from!.height / 2);
  await page.mouse.down();
  await page.mouse.move(to!.x + to!.width / 2, to!.y + to!.height / 2, { steps: 12 });
  await page.mouse.up();
  const generated = await applyGraph(page);
  expect(generated.source).toMatch(/go func\(\)\s*\{\s*ch <- 42/);
  expect(generated.source).toContain('value := <-ch');
  expect(generated.source).toContain('fmt.Println(value)');

  await page.locator('.operation-node.make_channel').click();
  await page.getByLabel('缓冲容量').fill('1');
  expect((await applyGraph(page)).source).toContain('make(chan int, 1)');
  await page.getByRole('button', { name: '下移', exact: true }).click();
  const rejected = page.waitForResponse(r => r.url().endsWith('/api/generate'));
  await page.getByRole('button', { name: '应用图形修改', exact: true }).click();
  expect((await (await rejected).json()).status).toBe('invalid_ir');
  await expect(page.locator('.diagnostic.error').first()).toBeVisible();
  await page.getByRole('button', { name: '放弃草稿', exact: true }).click();
  await page.getByRole('button', { name: '源码', exact: true }).click();
  await expect(page.getByLabel('Go 源码')).toHaveValue(/make\(chan int, 1\)/);
});

test('布局拖动不改变源码，错误源码保留有效模型，Unicode 双向定位正确', async ({ page }) => {
  await boot(page);
  const source = 'package main\nimport "fmt"\n// 中文注释：😀\nfunc main() {\n ch := make(chan int)\n go func(){ ch <- 42 }()\n value := <-ch\n fmt.Println(value)\n}\n';
  await importSource(page, source);
  const before = await savedProject(page);
  const heading = page.locator('.function-heading').first();
  const box = await heading.boundingBox();
  await page.mouse.move(box!.x + 60, box!.y + 20); await page.mouse.down();
  await page.mouse.move(box!.x + 95, box!.y + 55, { steps: 8 }); await page.mouse.up();
  const after = await savedProject(page);
  expect(after.source).toBe(before.source);
  expect(after.programRevision).toBe(before.programRevision);
  expect(after.layout.nodes[before.ir.entryFunctionId]).not.toEqual(before.layout.nodes[before.ir.entryFunctionId]);

  await page.locator('.operation-node.send').click();
  await page.getByRole('button', { name: '源码', exact: true }).click();
  const editor = page.getByLabel('Go 源码');
  await expect.poll(() => editor.evaluate((e: HTMLTextAreaElement) => e.value.slice(e.selectionStart, e.selectionEnd))).toBe('ch <- 42');
  await editor.evaluate((e: HTMLTextAreaElement) => { const i = e.value.indexOf('fmt.Println'); e.focus(); e.setSelectionRange(i, i); e.dispatchEvent(new MouseEvent('click', { bubbles: true })); });
  await expect(page.locator('.react-flow__node.selected .operation-node.print')).toBeVisible();
  await editor.fill('package main\nfunc main() { missing() }');
  const failed = page.waitForResponse(r => r.url().endsWith('/api/import'));
  await page.getByRole('button', { name: '应用代码修改', exact: true }).click();
  expect((await (await failed).json()).status).toBe('invalid');
  await expect(page.locator('.operation-node.send')).toContainText('42');
  await expect(page.locator('.sync-bar')).toContainText('最近有效图形');
  await expect(page.getByRole('button', { name: '编译验证', exact: true })).toBeDisabled();
  await expect(editor).toHaveValue(/missing/);
  await page.getByRole('button', { name: '放弃草稿', exact: true }).click();
  await expect(editor).toHaveValue(source);
});

test('过期转换响应不覆盖较新的源码草稿', async ({ page }) => {
  await boot(page);
  const editor = page.getByLabel('Go 源码');
  let release!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  let received!: () => void;
  const waiting = new Promise<void>(resolve => { received = resolve; });
  await page.route('**/api/import', async route => {
    const response = await route.fetch();
    received(); await gate; await route.fulfill({ response });
  });
  await editor.fill('package main\nfunc main(){ return }');
  await page.getByRole('button', { name: '应用代码修改', exact: true }).click();
  await waiting;
  const newer = 'package main\nfunc main(){ /* 新草稿 */ }';
  await editor.fill(newer);
  const responseDone = page.waitForResponse(r => r.url().endsWith('/api/import'));
  release(); await responseDone;
  await expect(editor).toHaveValue(newer);
  await expect(page.locator('.operation-node.send')).toContainText('42');
  await expect(page.locator('.sync-bar')).toContainText('最近有效图形');
});

test('后端不可达显示真实错误且保留有效模型', async ({ page }) => {
  await boot(page);
  await page.route('**/api/build', route => route.abort('connectionrefused'));
  await page.getByRole('button', { name: '编译验证', exact: true }).click();
  await expect(page.locator('.diagnostics-panel')).toContainText('本地服务不可达');
  await expect(page.locator('.operation-node.send')).toContainText('42');
  await expect(page.locator('.diagnostics-header')).not.toContainText('编译通过');
});

test('编译超时与工具链缺失的响应均呈现失败状态', async ({ page }) => {
  await boot(page);
  for (const [status, label] of [['timeout', '编译超时'], ['toolchain_unavailable', 'Go 工具链不可用'], ['compile_error', '编译失败']]) {
    // Exercise UI failure branches; real timeout/unavailable toolchain behavior
    // is independently covered by backend compiler tests.
    await page.route('**/api/build', async route => {
      const body = route.request().postDataJSON();
      await route.fulfill({ json: { requestId: body.requestId, programRevision: body.programRevision, status, durationMs: 5, toolchainVersion: '', diagnostics: [{ code: status, severity: 'error', phase: 'build', message: label }] } });
    });
    await page.getByRole('button', { name: '编译验证', exact: true }).click();
    await expect(page.locator('.diagnostics-header')).toContainText(label);
    await expect(page.locator('.diagnostics-header')).not.toContainText('编译通过');
    await page.unroute('**/api/build');
  }
});

test('诊断中的表达式 ID 可定位发送操作', async ({ page }) => {
  await boot(page);
  await page.locator('.operation-node.send').click();
  await page.getByLabel('发送值', { exact: true }).fill('invalid_integer');
  await page.getByRole('button', { name: '应用图形修改', exact: true }).click();
  await expect(page.locator('.diagnostic.error')).toHaveCount(1);
  await page.locator('.operation-node.receive').click();
  await page.locator('.diagnostic.error').click();
  await expect(page.getByLabel('发送值', { exact: true })).toHaveValue('invalid_integer');
  await expect(page.locator('.operation-node.send.is-selected')).toBeVisible();
});

test('1366 × 768 首次导入可完整看见 Channel 资源', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1366, height: 768 });
  await boot(page);
  const canvas = page.locator('.canvas');
  await expect.poll(async () => {
    const resource = await page.locator('.resource-node').boundingBox();
    const bounds = await canvas.boundingBox();
    return Boolean(resource && bounds && resource.y >= bounds.y && resource.y + resource.height <= bounds.y + bounds.height - 18 && resource.x >= bounds.x && resource.x + resource.width <= bounds.x + bounds.width);
  }).toBe(true);
  await page.screenshot({ path: testInfo.outputPath('workspace-1366.png'), fullPage: true });
});
