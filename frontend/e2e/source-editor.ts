import type { Page } from '@playwright/test';

// Read the actual model rather than Monaco's virtualized accessibility textarea.
// This imports the same dev-server module; no test-only API ships in the app.
export async function sourceValue(page: Page): Promise<string> {
  return page.evaluate(async () => {
    const modulePath = '/src/editor/monaco.ts';
    const { monaco } = await import(modulePath);
    const editor = monaco.editor.getEditors().find((item: { getDomNode(): HTMLElement | null }) => item.getDomNode()?.closest('.detail-panel'));
    return editor?.getValue() ?? '';
  });
}

export async function selectedSource(page: Page): Promise<string> {
  return page.evaluate(async () => {
    const modulePath = '/src/editor/monaco.ts';
    const { monaco } = await import(modulePath);
    const editor = monaco.editor.getEditors().find((item: { getDomNode(): HTMLElement | null }) => item.getDomNode()?.closest('.detail-panel'));
    return editor?.getModel()?.getValueInRange(editor.getSelection()) ?? '';
  });
}

export async function fillSource(page: Page, source: string) {
  const editor = page.locator('.detail-panel .monaco-editor textarea');
  await editor.focus();
  await editor.press('ControlOrMeta+a');
  if (source) await page.keyboard.insertText(source);
  else await editor.press('Backspace');
}
