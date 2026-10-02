import * as monaco from 'monaco-editor/editor';
import 'monaco-editor/features/codeEditor/register';
import 'monaco-editor/features/codicon/register';
import 'monaco-editor/features/bracketMatching/register';
import 'monaco-editor/features/clipboard/register';
import 'monaco-editor/features/comment/register';
import 'monaco-editor/features/contextmenu/register';
import 'monaco-editor/features/cursorUndo/register';
import 'monaco-editor/features/find/register';
import 'monaco-editor/features/folding/register';
import 'monaco-editor/features/fontZoom/register';
import 'monaco-editor/features/gotoError/register';
import 'monaco-editor/features/gotoLine/register';
import 'monaco-editor/features/hover/register';
import 'monaco-editor/features/indentation/register';
import 'monaco-editor/features/linesOperations/register';
import 'monaco-editor/features/multicursor/register';
import 'monaco-editor/features/readOnlyMessage/register';
import 'monaco-editor/features/smartSelect/register';
import 'monaco-editor/features/snippet/register';
import 'monaco-editor/features/suggest/register';
import 'monaco-editor/features/tokenization/register';
import 'monaco-editor/features/wordHighlighter/register';
import 'monaco-editor/features/wordOperations/register';
import 'monaco-editor/languages/definitions/go/register';
import EditorWorker from 'monaco-editor/editor/editor.worker?worker';

// Vite emits the worker alongside the application. Editing never needs a CDN.
self.MonacoEnvironment = { getWorker: () => new EditorWorker() };

monaco.editor.defineTheme('go-canvas', {
  base: 'vs-dark', inherit: true,
  rules: [
    { token: 'keyword', foreground: 'C5A7EA' },
    { token: 'string', foreground: 'ABCDAA' },
    { token: 'number', foreground: 'E4BE8D' },
    { token: 'comment', foreground: '6F8A80' },
  ],
  colors: {
    'editor.background': '#161F1B',
    'editor.foreground': '#BED0C5',
    'editorLineNumber.foreground': '#587062',
    'editorLineNumber.activeForeground': '#B2D1BB',
    'editor.selectionBackground': '#426C5680',
    'editor.inactiveSelectionBackground': '#426C5650',
    'editor.lineHighlightBackground': '#203128',
    'editorCursor.foreground': '#BFFBDD',
    'editorWidget.background': '#202C25',
    'editorWidget.border': '#48604E',
  },
});

export { monaco };
