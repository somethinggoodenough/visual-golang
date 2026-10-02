import { forwardRef, useEffect, useImperativeHandle, useRef } from 'react';
import { utf8ToUtf16, type Diagnostic, type SourceSpan } from '../model/types';
import { monaco } from './monaco';
import './source-editor.css';

export interface SourceEditorHandle {
  focus(): void;
  highlight(span: SourceSpan, focus?: boolean): void;
  replaceSource(source: string): void;
}

interface SourceEditorProps {
  value: string;
  readOnly?: boolean;
  diagnostics?: Diagnostic[];
  onChange: (value: string) => void;
  onCursorOffset?: (offset: number) => void;
  onApply?: () => void;
  onFormat?: () => void;
  ariaLabel: string;
  formatLabel: string;
  selection?: SourceSpan | null;
  visible?: boolean;
}

function spanRange(model: monaco.editor.ITextModel, span: SourceSpan): monaco.Range {
  const source = model.getValue();
  const start = model.getPositionAt(utf8ToUtf16(source, span.startByte));
  const end = model.getPositionAt(utf8ToUtf16(source, span.endByte));
  return new monaco.Range(start.lineNumber, start.column, end.lineNumber, end.column);
}

export const SourceEditor = forwardRef<SourceEditorHandle, SourceEditorProps>(function SourceEditor(props, ref) {
  const host = useRef<HTMLDivElement>(null);
  const editor = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const latest = useRef(props); latest.current = props;
  const updating = useRef(false);
  const selectionDecoration = useRef<monaco.editor.IEditorDecorationsCollection | null>(null);

  useImperativeHandle(ref, () => ({
    focus: () => editor.current?.focus(),
    highlight: (span, focus = true) => {
      const instance = editor.current, model = instance?.getModel();
      if (!instance || !model) return;
      const range = spanRange(model, span);
      updating.current = true;
      instance.setSelection(range);
      instance.revealRangeInCenterIfOutsideViewport(range);
      if (focus) instance.focus();
      updating.current = false;
    },
    replaceSource: value => {
      const instance = editor.current, model = instance?.getModel();
      if (!instance || !model || model.getValue() === value) return;
      // gofmt is one undoable edit. User typing retains Monaco's native undo stack.
      instance.pushUndoStop();
      updating.current = true;
      try {
        // Imported Windows files may use CRLF; gofmt returns LF. Keep the exact
        // formatted bytes instead of letting Monaco restore the model's old EOL.
        instance.executeEdits('gofmt', [{ range: model.getFullModelRange(), text: value }]);
        model.pushEOL(value.includes('\r\n') ? monaco.editor.EndOfLineSequence.CRLF : monaco.editor.EndOfLineSequence.LF);
      } finally { updating.current = false; }
      instance.pushUndoStop();
      latest.current.onChange(model.getValue());
    },
  }), []);

  useEffect(() => {
    if (!host.current) return;
    const model = monaco.editor.createModel(latest.current.value, 'go');
    model.updateOptions({ tabSize: 4, insertSpaces: false });
    const instance = monaco.editor.create(host.current, {
      model, theme: 'go-canvas', automaticLayout: true,
      readOnly: latest.current.readOnly,
      ariaLabel: latest.current.ariaLabel,
      fontFamily: '"JetBrains Mono", "Cascadia Code", Consolas, monospace',
      fontSize: 12, lineHeight: 24, padding: { top: 14, bottom: 20 },
      minimap: { enabled: false }, lineNumbersMinChars: 3,
      scrollBeyondLastLine: false, glyphMargin: false, folding: true,
      renderLineHighlight: 'line', overviewRulerLanes: 2,
      autoIndent: 'full', autoClosingBrackets: 'always', autoClosingQuotes: 'always',
      bracketPairColorization: { enabled: true },
      wordBasedSuggestions: 'currentDocument',
      // Keep keyboard/accessibility behavior deterministic across browser versions.
      editContext: false,
    });
    editor.current = instance;
    selectionDecoration.current = instance.createDecorationsCollection();
    const content = model.onDidChangeContent(() => {
      if (!updating.current) latest.current.onChange(model.getValue());
    });
    const cursor = instance.onDidChangeCursorPosition(event => {
      if (!updating.current && event.source !== 'api') latest.current.onCursorOffset?.(model.getOffsetAt(event.position));
    });
    const apply = instance.addAction({
      id: 'go-canvas.apply', label: 'Apply changes',
      keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter],
      run: () => latest.current.onApply?.(),
    });
    return () => {
      apply.dispose(); content.dispose(); cursor.dispose();
      selectionDecoration.current = null; editor.current = null;
      instance.dispose(); model.dispose();
    };
  }, []);

  useEffect(() => {
    const instance = editor.current;
    const action = instance?.addAction({
      id: 'go-canvas.format', label: props.formatLabel,
      keybindings: [monaco.KeyMod.Shift | monaco.KeyMod.Alt | monaco.KeyCode.KeyF],
      contextMenuGroupId: '1_modification', contextMenuOrder: 1.3,
      precondition: '!editorReadonly', run: () => latest.current.onFormat?.(),
    });
    return () => action?.dispose();
  }, [props.formatLabel]);

  useEffect(() => {
    const model = editor.current?.getModel();
    if (!model || model.getValue() === props.value) return;
    // Applied graph edits, project imports and discard replace the source model.
    updating.current = true;
    model.setValue(props.value);
    updating.current = false;
  }, [props.value]);

  useEffect(() => {
    editor.current?.updateOptions({ readOnly: Boolean(props.readOnly), ariaLabel: props.ariaLabel });
  }, [props.readOnly, props.ariaLabel]);

  useEffect(() => {
    const model = editor.current?.getModel();
    if (!model) return;
    monaco.editor.setModelMarkers(model, 'go-canvas', (props.diagnostics ?? []).flatMap(diagnostic => {
      if (!diagnostic.span) return [];
      return [{
        ...spanRange(model, diagnostic.span), message: diagnostic.message, code: diagnostic.code,
        source: diagnostic.phase,
        severity: diagnostic.severity === 'error' ? monaco.MarkerSeverity.Error : diagnostic.severity === 'warning' ? monaco.MarkerSeverity.Warning : monaco.MarkerSeverity.Info,
      }];
    }));
  }, [props.diagnostics, props.value]);

  useEffect(() => {
    const model = editor.current?.getModel();
    if (!model) return;
    selectionDecoration.current?.set(props.selection ? [{
      range: spanRange(model, props.selection),
      options: { className: 'go-source-linked-range', isWholeLine: false },
    }] : []);
  }, [props.selection, props.value]);

  useEffect(() => { if (props.visible !== false) editor.current?.layout(); }, [props.visible]);

  return <div className="monaco-source-editor" data-testid="source-editor"><div className="monaco-source-host" ref={host}/></div>;
});
