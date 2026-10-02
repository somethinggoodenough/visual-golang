import { test, expect, type Page } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { causalEdges, causalPath, eventNode, type AnalysisModel, type ExecutionEvent, type RunResult } from '../src/explorer/model';

async function boot(page: Page) {
  await page.goto('/');
  await expect(page.getByRole('button', { name: '执行探索', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: '执行探索', exact: true }).click();
  await expect(page.locator('.explorer-status')).toContainText('分析完成');
}

async function fill(page: Page, text: string) {
  await page.locator('.explorer-source .monaco-editor textarea').focus();
  await page.keyboard.press('ControlOrMeta+a');
  await page.keyboard.insertText(text);
}

test('KVStore structure, real execution, trace replay and source linking', async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
  await boot(page);
  await page.getByRole('button', { name: 'KVStore 示例', exact: true }).click();
  await page.getByRole('button', { name: '分析源码', exact: true }).click();
  await expect(page.locator('.structure-function h3').filter({ hasText: 'stateLoop' })).toBeVisible();
  await expect(page.locator('.structure-node.select').first()).toBeVisible();
  expect(await page.locator('.structure-node').count()).toBeLessThan(500);
  const nodeIds = await page.locator('.structure-node').evaluateAll(nodes => nodes.map(n => n.getAttribute('data-node-id')));
  expect(new Set(nodeIds).size).toBe(nodeIds.length);
  await page.getByRole('button', { name: '架构', exact: true }).click();
  await expect(page.locator('.channel-access').filter({ hasText: 'stateChan' }).first()).toBeVisible();

  await page.getByRole('button', { name: '编译验证', exact: true }).click();
  await expect(page.locator('.explorer-status')).toContainText('编译通过', { timeout: 45_000 });
  await expect(page.getByLabel('stdout', { exact: true })).toHaveText('无输出');
  await page.getByRole('button', { name: '运行', exact: true }).click();
  await expect(page.getByLabel('stdout', { exact: true })).toContainText('APPEND: Hello Go', { timeout: 45_000 });
  await expect(page.getByLabel('stdout', { exact: true })).toContainText('stopped');

  const response = page.waitForResponse(r => r.url().endsWith('/api/run'));
  await page.getByRole('button', { name: '运行 + Trace', exact: true }).click();
  const result = await (await response).json() as RunResult;
  expect(result.status).toBe('ok');
  expect(result.trace?.events.some(e => e.kind === 'SEND_ATTEMPT')).toBeTruthy();
  await expect(page.locator('.goroutine-lane')).not.toHaveCount(0);
  await page.getByRole('button', { name: '后一步', exact: true }).click();
  await expect(page.getByRole('slider', { name: '回放位置' })).toHaveValue('0');
  await page.getByRole('tab', { name: 'Trace', exact: true }).click();
  const sourcedIndex = result.trace!.events.findIndex(e => e.nodeId && e.sourceLine);
  expect(sourcedIndex).toBeGreaterThanOrEqual(0);
  await page.locator('.trace-list button').filter({ has: page.locator('span').filter({ hasText: new RegExp(`^#${sourcedIndex + 1}$`) }) }).click();
  await expect.poll(() => page.evaluate(async () => {
    const modulePath = '/src/editor/monaco.ts'; const { monaco } = await import(modulePath);
    const editor = monaco.editor.getEditors().find((e: { getDomNode(): HTMLElement }) => e.getDomNode()?.closest('.explorer-source'));
    return editor?.getSelection()?.startLineNumber;
  })).toBe(result.trace!.events[sourcedIndex].sourceLine);
  await page.getByRole('tab', { name: '结构', exact: true }).click();
  await expect(page.locator('.structure-node.selected')).toHaveCount(1);

  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: '下载原始 Trace', exact: true }).click();
  const artifact = await download;
  expect((await readFile((await artifact.path())!)).subarray(0, 16).toString()).toContain('go 1.');
  await page.screenshot({ path: testInfo.outputPath('kvstore-explorer.png'), fullPage: true });
  expect(errors).toEqual([]);
});

test('runtime deadlock differs from build failure and translations remain usable', async ({ page }) => {
  await boot(page);
  await fill(page, 'package main\nfunc main(){ ch := make(chan int); <-ch }');
  await page.getByRole('button', { name: '运行', exact: true }).click();
  await expect(page.locator('.explorer-status')).toContainText('Go 检测到死锁');
  await expect(page.getByLabel('stderr', { exact: true })).toContainText('deadlock');
  await page.getByRole('button', { name: 'English', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Run + Trace', exact: true })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Causality', exact: true })).toBeVisible();
  await expect(page.locator('.explorer-status')).toContainText('Go detected a deadlock');
  await fill(page, 'package main\nfunc main(){ missing() }');
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect(page.locator('.explorer-status')).toContainText('Build failed');
});

test('editing source invalidates an in-flight run and preserves the new draft', async ({ page }) => {
  await boot(page);
  let release!: () => void, arrived!: () => void;
  const gate = new Promise<void>(resolve => { release = resolve; });
  const pending = new Promise<void>(resolve => { arrived = resolve; });
  await page.route('**/api/run', async route => { const response = await route.fetch(); arrived(); await gate; await route.fulfill({ response }); });
  await page.getByRole('button', { name: '运行', exact: true }).click(); await pending;
  await fill(page, 'package main\nfunc main(){ println(99) }');
  release();
  await expect(page.locator('.explorer-status')).toHaveText('');
  await expect(page.getByLabel('stdout', { exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: '返回图形编辑', exact: true }).click();
  await expect(page.getByRole('button', { name: '应用代码修改', exact: true })).toBeVisible();
});

test('causal paths use per-goroutine observations and creation, never timestamp or value matching', () => {
  const event = (id: string, g: string, fields: Partial<ExecutionEvent> = {}): ExecutionEvent => ({ id, goroutineId:g, kind:'STATEMENT', timeNs:0, sourceLine:1, ...fields });
  const events = [event('a','1'), event('other','2'), event('spawn','3',{kind:'GOROUTINE_CREATE',actorGoroutineId:'1',fromState:'DONE',toState:'RUNNABLE'}), event('child','3'), event('last','3')];
  const edges = causalEdges(events);
  expect(causalPath(edges,'a','last')?.map(e => e.kind)).toEqual(['program_order','goroutine_start','program_order']);
  expect(causalPath(edges,'a','other')).toBeNull();
  expect(causalPath(edges,'last','a')).toBeNull();
  const blocked = event('blocked','1',{kind:'BLOCKED',fromState:'RUNNING',toState:'BLOCKED_SEND'});
  expect(causalEdges([blocked,event('receive','2',{kind:'RECEIVE_COMPLETE'})])).toEqual([]);
  expect(causalEdges([event('a','1'), event('helper','1',{instrumentation:true}), event('b','1')])).toEqual([{from:'a',to:'b',kind:'program_order'}]);
  const model: AnalysisModel = { schemaVersion:'1.0',packageName:'main',functions:[],channels:[],nodes:[{id:'current',kind:'send',label:'ch <- 1',span:{file:'main.go',startByte:0,endByte:7,startLine:7,startColumn:1}}] };
  expect(eventNode(event('legacy','1',{sourceLine:7}),model)?.id).toBe('current');
  expect(eventNode(event('stale','1',{nodeId:'old-node',sourceLine:7}),model)).toBeUndefined();
});
