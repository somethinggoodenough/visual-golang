import { useCallback, useEffect, useReducer, useRef, useState } from 'react';
import { ArrowDownToLine, ArrowUpFromLine, Box, Check, CheckCheck, ChevronDown, Circle, CircleAlert, CircleCheck, CircleStop, Code2, Download, FileCode2, FilePlus2, FolderOpen, GitBranch, GitFork, HelpCircle, Layers, LoaderCircle, LogOut, PanelRightClose, Plus, Redo2, Save, Settings2, Terminal, Undo2, Upload, X } from 'lucide-react';
import { api, download, getHealth } from './api/client';
import { Canvas } from './editor/Canvas';
import { Inspector } from './editor/Inspector';
import type { Command } from './model/commands';
import { defaultLayout, DEMO_SOURCE, EMPTY_SOURCE, findStatement, labels, uid, utf16ToUtf8, utf8ToUtf16, visualNodeId, type APIResult, type Diagnostic, type Health, type Layout, type Project, type SourceSpan, type StatementKind } from './model/types';
import { currentIR, initialWorkspace, reducer, type Action } from './state/workspace';

const palette: { kind: StatementKind; icon: typeof Box; hint: string }[] = [
  { kind: 'make_channel', icon: Box, hint: 'make(chan int)' }, { kind: 'spawn', icon: GitFork, hint: 'go func()' },
  { kind: 'send', icon: ArrowUpFromLine, hint: 'ch ← value' }, { kind: 'receive', icon: ArrowDownToLine, hint: 'value := ← ch' },
  { kind: 'close', icon: CircleStop, hint: 'close(ch)' }, { kind: 'let', icon: Code2, hint: 'name := value' },
  { kind: 'print', icon: Terminal, hint: 'fmt.Println(value)' }, { kind: 'return', icon: LogOut, hint: 'return' },
];
interface BuildResult { status: string; message: string; revision: number }
interface Confirm { title: string; detail: string; action: () => void }

export default function App() {
  const [state, dispatch] = useReducer(reducer, undefined, initialWorkspace);
  const stateRef = useRef(state); stateRef.current = state;
  const [health, setHealth] = useState<Health | null>(null);
  const [connection, setConnection] = useState<'connecting' | 'online' | 'offline'>('connecting');
  const [diagnostics, setDiagnostics] = useState<Diagnostic[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [build, setBuild] = useState<BuildResult | null>(null);
  const [tab, setTab] = useState<'source' | 'properties'>('source');
  const [target, setTarget] = useState('');
  const [insertion, setInsertion] = useState('end');
  const [confirm, setConfirm] = useState<Confirm | null>(null);
  const [notice, setNotice] = useState('');
  const [showHelp, setShowHelp] = useState(false);
  const [sourceSelection, setSourceSelection] = useState<SourceSpan | null>(null);
  const [fitRequest, setFitRequest] = useState(0);
  const [fitProgramId, setFitProgramId] = useState<string | null>(null);
  const [diagnosticStatus, setDiagnosticStatus] = useState('');
  const sourceRef = useRef<HTMLTextAreaElement>(null);
  const gutterRef = useRef<HTMLDivElement>(null);
  const goInput = useRef<HTMLInputElement>(null);
  const projectInput = useRef<HTMLInputElement>(null);
  const requestSequence = useRef(0);
  const semanticTicket = useRef(0);
  const bootstrapped = useRef(false);
  const { project, draft, layout } = state.present;
  const ir = currentIR(state);
  const source = draft?.kind === 'source' ? draft.source : project.source;
  const targetFn = ir.functions.find(f => f.id === target) ?? ir.functions.find(f => f.id === ir.entryFunctionId)!;
  const appliedBuild = build?.revision === project.programRevision && !draft ? build : null;

  const message = useCallback((text: string) => setNotice(text), []);
  const edit = useCallback((action: Action) => {
    if (!['layout', 'select', 'clearError'].includes(action.type)) {
      semanticTicket.current++; requestSequence.current++; setBusy(null); setBuild(null); setDiagnostics([]); setDiagnosticStatus(''); setSourceSelection(null);
    }
    dispatch(action);
  }, []);
  const execute = useCallback((command: Command) => {
    if (stateRef.current.present.draft?.kind === 'source') {
      setConfirm({ title: '切换到图形编辑？', detail: '当前代码草稿尚未应用。可以保留草稿继续编辑，或放弃它后修改图形。', action: () => { edit({ type: 'discard' }); edit({ type: 'graph', command }); setTab('properties'); } }); return;
    }
    edit({ type: 'graph', command }); setTab('properties');
  }, [edit]);
  const changeLayout = useCallback((next: Layout) => edit({ type: 'layout', layout: next }), [edit]);

  const highlight = useCallback((span: SourceSpan, text = stateRef.current.present.project.source, focus = true) => {
    setSourceSelection(span);
    requestAnimationFrame(() => {
      const editor = sourceRef.current;
      if (!editor) return;
      if (focus) editor.focus();
      editor.setSelectionRange(utf8ToUtf16(text, span.startByte), utf8ToUtf16(text, span.endByte));
      editor.scrollTop = Math.max(0, (span.startLine - 4) * 24);
      if (gutterRef.current) gutterRef.current.scrollTop = editor.scrollTop;
    });
  }, []);
  const select = useCallback((id: string, properties = true) => {
    const current = stateRef.current, model = currentIR(current);
    const visualId = visualNodeId(model, id);
    dispatch({ type: 'select', id: visualId });
    const found = findStatement(model, visualId);
    if (found) setTarget(found.fn.id);
    else if (model.functions.some(fn => fn.id === id)) setTarget(id);
    let mappingId = id;
    if (model.symbols.some(sym => sym.id === id && sym.type === 'chan int')) mappingId = model.functions.flatMap(f => f.body.statements).find(s => s.kind === 'make_channel' && s.symbolId === id)?.id ?? id;
    const span = current.present.project.sourceMap[mappingId];
    if (span && !current.present.draft) highlight(span, current.present.project.source, false);
    if (properties) setTab('properties');
  }, [highlight]);

  async function convertSource(text: string, reset = false) {
    const serial = ++requestSequence.current, ticket = semanticTicket.current, requestId = uid('request');
    setBusy('import'); setDiagnostics([]); setDiagnosticStatus('');
    try {
      const result = await api('import', { requestId, source: text });
      if (serial !== requestSequence.current || ticket !== semanticTicket.current || result.requestId !== requestId) return;
      setConnection('online'); setDiagnostics(result.diagnostics ?? []);
      if (result.status === 'editable' && result.ir && result.sourceMap && result.sourceHash) {
        const next: Project = { fileFormatVersion: '1.0', programRevision: stateRef.current.present.project.programRevision + 1, ir: result.ir, source: text, sourceHash: result.sourceHash, sourceMap: result.sourceMap, originalImportedSource: text, layout: defaultLayout(result.ir) };
        dispatch({ type: 'commit', project: next, reset });
        semanticTicket.current++; setBuild(null); setTarget(result.ir.entryFunctionId); setInsertion('end'); setSourceSelection(null);
        setFitRequest(request => request + 1);
        setFitProgramId(result.ir.id);
        setDiagnosticStatus('源码已解析，图形与代码同步。');
        message(reset ? '已打开程序，可以选择节点开始编辑。' : '代码修改已应用。');
      } else {
        if (stateRef.current.present.draft?.kind !== 'source' || stateRef.current.present.draft.source !== text) dispatch({ type: 'source', source: text });
        setDiagnosticStatus(result.status === 'unsupported' ? '包含 v0 尚未支持的 Go 语法；保留最近有效图形。' : '代码未通过检查；保留最近有效图形。');
      }
    } catch (error) { if (serial === requestSequence.current) reportError(error, 'parse'); }
    finally { if (serial === requestSequence.current) setBusy(null); }
  }
  function reportError(error: unknown, phase: Diagnostic['phase']) {
    const text = (error as Error).message || '请求失败';
    setDiagnostics([{ code: 'request_failed', severity: 'error', phase, message: text.includes('fetch') ? '本地服务不可达。请确认 Go 后端正在运行，再重试。' : text }]);
    setDiagnosticStatus('请求未完成，当前模型与草稿已保留。');
    if (text.includes('fetch')) setConnection('offline');
  }
  async function apply() {
    const doc = stateRef.current.present;
    if (!doc.draft) return;
    if (doc.draft.kind === 'source') { await convertSource(doc.draft.source); return; }
    const serial = ++requestSequence.current, ticket = semanticTicket.current, requestId = uid('request');
    setBusy('generate'); setDiagnostics([]);
    try {
      const revision = doc.project.programRevision;
      const result = await api('generate', { requestId, programRevision: revision, ir: doc.draft.ir });
      if (serial !== requestSequence.current || ticket !== semanticTicket.current || result.requestId !== requestId || (result.programRevision !== undefined && result.programRevision !== revision)) return;
      setConnection('online'); setDiagnostics(result.diagnostics ?? []);
      if (result.status === 'ok' && result.source !== undefined && result.sourceMap && result.sourceHash) {
        const next: Project = { ...doc.project, programRevision: revision + 1, ir: doc.draft.ir, source: result.source, sourceMap: result.sourceMap, sourceHash: result.sourceHash, layout: defaultLayout(doc.draft.ir, stateRef.current.present.layout) };
        dispatch({ type: 'commit', project: next }); semanticTicket.current++; setBuild(null); setSourceSelection(null); setDiagnosticStatus('图形修改已应用，Go 代码已通过生成与类型检查。'); message('图形修改已应用，Go 源码已更新。');
      } else setDiagnosticStatus('图形草稿未通过检查，请按诊断修正后重新应用。');
    } catch (error) { if (serial === requestSequence.current) reportError(error, 'generate'); }
    finally { if (serial === requestSequence.current) setBusy(null); }
  }
  async function compile() {
    const doc = stateRef.current.present;
    if (doc.draft) { message('请先应用修改，再编译当前程序。'); return; }
    const serial = ++requestSequence.current, ticket = semanticTicket.current, requestId = uid('request');
    setBusy('build'); setDiagnostics([]); setBuild(null);
    try {
      const result = await api('build', { requestId, programRevision: doc.project.programRevision, ir: doc.project.ir });
      if (serial !== requestSequence.current || ticket !== semanticTicket.current || result.requestId !== requestId || (result.programRevision !== undefined && result.programRevision !== doc.project.programRevision)) return;
      setConnection('online'); setDiagnostics(result.diagnostics ?? []);
      const statuses: Record<string, string> = { ok: '编译通过', compile_error: '编译失败', timeout: '编译超时', toolchain_unavailable: 'Go 工具链不可用' };
      const text = `${statuses[result.status] ?? result.status}${result.toolchainVersion ? ` · ${result.toolchainVersion}` : ''}${result.durationMs !== undefined ? ` · ${result.durationMs} ms` : ''}`;
      setBuild({ status: result.status, message: text, revision: doc.project.programRevision }); setDiagnosticStatus(text);
    } catch (error) { if (serial === requestSequence.current) reportError(error, 'build'); }
    finally { if (serial === requestSequence.current) setBusy(null); }
  }
  function beforeReplace(action: () => void) {
    if (stateRef.current.present.draft) setConfirm({ title: '当前修改尚未应用', detail: '继续将放弃当前语义草稿。已应用程序也将被所选文件或示例替换。', action });
    else action();
  }
  function openSource(text: string) {
    beforeReplace(() => {
      edit({ type: 'discard' });
      edit({ type: 'source', source: text });
      setTab('source'); void convertSource(text, true);
    });
  }
  async function openProject(file: File) {
    try {
      const parsed: unknown = JSON.parse(await file.text());
      beforeReplace(() => void loadProject(parsed));
    } catch (error) { reportError(error, 'ir'); }
  }
  async function loadProject(contents: unknown) {
    const serial = ++requestSequence.current, ticket = semanticTicket.current, requestId = uid('request');
    setBusy('project'); setDiagnostics([]);
    try {
      const result: APIResult = await api('project/validate', { requestId, project: contents });
      if (serial !== requestSequence.current || ticket !== semanticTicket.current || result.requestId !== requestId) return;
      setConnection('online'); setDiagnostics(result.diagnostics ?? []);
      if (result.status === 'ok' && result.project) {
        setFitProgramId(null);
        dispatch({ type: 'commit', project: result.project, reset: true }); semanticTicket.current++; setBuild(null); setTarget(result.project.ir.entryFunctionId); setSourceSelection(null); setDiagnosticStatus('项目一致性验证通过，模型、布局与源码已恢复。'); message('项目已恢复，布局与节点 ID 已保留。');
      } else setDiagnosticStatus('项目一致性验证失败，当前工作区未被替换。');
    } catch (error) { if (serial === requestSequence.current) reportError(error, 'ir'); }
    finally { if (serial === requestSequence.current) setBusy(null); }
  }
  function saveProject() {
    if (draft) { message('请先应用或放弃草稿，再保存项目。'); return; }
    if (!project.sourceHash) { message('请先连接后端并应用程序，再保存项目。'); return; }
    download('main.goviz.json', JSON.stringify({ ...project, layout: defaultLayout(project.ir, layout) }, null, 2), 'application/json'); message('已保存 main.goviz.json');
  }
  function add(kind: StatementKind, functionId = targetFn.id) {
    const fn = ir.functions.find(f => f.id === functionId)!;
    const index = insertion === 'end' ? fn.body.statements.length : Math.min(Number(insertion), fn.body.statements.length);
    execute({ type: 'AddStatement', kind, functionId, index });
  }
  function sourceSelect() {
    if (draft || !sourceRef.current) return;
    const byte = utf16ToUtf8(source, sourceRef.current.selectionStart);
    const match = Object.entries(project.sourceMap).filter(([, span]) => span.startByte <= byte && byte < span.endByte).sort((a, b) => (a[1].endByte - a[1].startByte) - (b[1].endByte - b[1].startByte))[0];
    if (!match) return;
    const id = visualNodeId(ir, match[0]);
    dispatch({ type: 'select', id }); setSourceSelection(match[1]);
  }
  function diagnosticSelect(d: Diagnostic) {
    // A generate/build span refers to freshly formatted code, which can differ
    // from the committed imported text. Node identity is the safe link here.
    if (d.nodeId && draft?.kind !== 'source') { select(d.nodeId); return; }
    if (d.span) { setTab('source'); highlight(d.span, source); }
    else if (d.nodeId) select(d.nodeId);
  }

  useEffect(() => {
    if (bootstrapped.current) return;
    bootstrapped.current = true;
    void getHealth().then(data => { setHealth(data); setConnection('online'); }).catch(() => setConnection('offline'));
    void convertSource(DEMO_SOURCE, true);
  }, []);
  useEffect(() => {
    if (!notice) return;
    const timeout = setTimeout(() => setNotice(''), 4800); return () => clearTimeout(timeout);
  }, [notice]);
  useEffect(() => {
    const handle = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') { e.preventDefault(); saveProject(); }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'z' && !(e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLInputElement)) { e.preventDefault(); edit({ type: e.shiftKey ? 'redo' : 'undo' }); }
      if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); void apply(); }
    };
    window.addEventListener('keydown', handle); return () => window.removeEventListener('keydown', handle);
  });
  useEffect(() => {
    const warn = (e: BeforeUnloadEvent) => { if (stateRef.current.present.draft) e.preventDefault(); };
    window.addEventListener('beforeunload', warn); return () => window.removeEventListener('beforeunload', warn);
  }, []);

  return <div className="app-shell">
    <header className="app-header"><div className="brand"><div className="brand-symbol"><GitBranch size={22}/></div><div><span>Go<span className="brand-light">Canvas</span></span><small>并发，从结构开始。</small></div><span className="version-tag">v0.1</span></div><div className="header-center"><span className="workspace-dot"/>本地工作区<span className="header-slash">/</span><FileCode2 size={14}/>main.go{draft && <span className="dirty-dot" title="存在未应用草稿"/>}</div><div className="header-right"><span className={`connection ${connection}`}><i/>{connection === 'online' ? '本地服务已连接' : connection === 'offline' ? '服务离线' : '正在连接'}</span><button className="icon-button" aria-label="使用帮助" onClick={() => setShowHelp(true)}><HelpCircle size={18}/></button></div></header>
    <nav className="toolbar" aria-label="项目操作"><div className="toolbar-group"><button onClick={() => openSource(EMPTY_SOURCE)}><FilePlus2 size={15}/>新建</button><button onClick={() => openSource(DEMO_SOURCE)}><Layers size={15}/>打开示例</button><button onClick={() => goInput.current?.click()}><Upload size={15}/>导入 Go</button><button onClick={() => projectInput.current?.click()}><FolderOpen size={15}/>打开项目</button></div><div className="toolbar-group undo-group"><button className="icon-button" aria-label="撤销" title="撤销 ⌘Z" disabled={!state.past.length} onClick={() => edit({ type: 'undo' })}><Undo2 size={16}/></button><button className="icon-button" aria-label="重做" title="重做 ⇧⌘Z" disabled={!state.future.length} onClick={() => edit({ type: 'redo' })}><Redo2 size={16}/></button></div><div className="toolbar-spacer"/><div className="toolbar-group export-group"><button onClick={() => { if (draft) { message('请先应用或放弃草稿，再导出 Go。'); return; } download('main.go', project.source, 'text/plain'); }}><Download size={15}/>导出 Go</button><button onClick={saveProject}><Save size={15}/>保存项目</button></div><button className="compile-button" disabled={Boolean(draft) || busy === 'build'} onClick={() => void compile()}>{busy === 'build' ? <LoaderCircle className="spin" size={15}/> : <CheckCheck size={15}/>}编译验证</button></nav>
    <input ref={goInput} type="file" accept=".go,text/plain" aria-label="导入 Go 文件" hidden onChange={async e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) openSource(await file.text()); }}/>
    <input ref={projectInput} type="file" accept=".json,.goviz.json,application/json" aria-label="打开项目文件" hidden onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) void openProject(file); }}/>
    <main className="workspace">
      <aside className="toolbox"><div className="panel-heading"><span><Box size={14}/>操作工具箱</span><span className="small-number">08</span></div><div className="toolbox-body"><div className="section-eyebrow">添加到执行区域</div><label className="target-field"><select aria-label="目标函数" value={targetFn.id} onChange={e => { setTarget(e.target.value); setInsertion('end'); }} >{ir.functions.map((fn, i) => <option key={fn.id} value={fn.id}>{fn.kind === 'main' ? 'main' : `goroutine ${i}`}</option>)}</select><ChevronDown size={13}/></label><label className="insertion-field"><span>插入位置</span><select aria-label="插入位置" value={insertion} onChange={e => setInsertion(e.target.value)}><option value="end">末尾</option>{targetFn.body.statements.map((s, i) => <option key={s.id} value={i}>第 {i + 1} 个操作之前</option>)}</select></label><div className="palette">{palette.map(({ kind, icon: Icon, hint }) => <button key={kind} className={`palette-item ${kind}`} aria-label={labels[kind]} draggable onDragStart={e => { e.dataTransfer.setData('application/goviz', kind); e.dataTransfer.effectAllowed = 'copy'; }} onClick={() => add(kind)}><span className="palette-icon"><Icon size={17}/></span><span><strong>{labels[kind]}</strong><code>{hint}</code></span><Plus size={13} className="palette-plus"/></button>)}</div><div className="toolbox-hint"><span className="hint-dot"/>点击添加，或拖入执行区域。<br/>添加后在属性中配置操作。</div></div><div className="toolbox-footer"><Code2 size={17}/><div><strong>Go 语言子集</strong><span>Channel · Goroutine · int / bool</span></div></div></aside>
      <section className="graph-panel"><div className="panel-heading graph-heading"><div><GitBranch size={15}/><strong>程序结构</strong><span className="view-tag">STRUCTURE</span></div><span className="graph-count">{ir.functions.length} 个函数 · {ir.functions.reduce((n, fn) => n + fn.body.statements.length, 0)} 个操作</span></div><div className={`sync-bar ${draft ? 'draft' : ''}`}><div>{draft ? <Circle size={12}/> : <CircleCheck size={13}/>}<span>{draft?.kind === 'source' ? `显示最近有效图形 · r${project.programRevision}` : draft?.kind === 'graph' ? '图形草稿 · 尚未应用到代码' : `图形与代码已同步 · r${project.programRevision}`}</span></div><button className={draft ? 'apply-button' : 'synced-button'} disabled={!draft} onClick={() => void apply()}>{busy === 'generate' || busy === 'import' ? <LoaderCircle className="spin" size={13}/> : draft ? <Check size={13}/> : <CheckCheck size={13}/>} {draft?.kind === 'source' ? '应用代码修改' : draft?.kind === 'graph' ? '应用图形修改' : '已同步'}</button></div><Canvas ir={ir} layout={layout} fitRequest={fitProgramId === ir.id ? fitRequest : 0} selected={state.selected} onSelect={select} onLayout={changeLayout} onConnect={(statementId, symbolId) => execute({ type: 'BindChannel', statementId, symbolId })} onAdd={add} onError={message} locked={draft?.kind === 'source'}/>{draft && <div className="draft-footer"><span>{draft.kind === 'graph' && project.originalImportedSource === project.source ? '生成将规范化排版；原始源码副本随项目保留。' : '应用通过检查后，另一侧视图才会更新。'}</span><button onClick={() => edit({ type: 'discard' })}>放弃草稿</button></div>}</section>
      <aside className="detail-panel"><div className="detail-tabs"><button className={tab === 'source' ? 'active' : ''} onClick={() => { setTab('source'); if (sourceSelection && !draft) highlight(sourceSelection, source, false); }}><FileCode2 size={14}/>源码</button><button className={tab === 'properties' ? 'active' : ''} onClick={() => setTab('properties')}><Settings2 size={14}/>属性</button><PanelRightClose size={15} className="panel-decoration"/></div>
        <div className="source-panel" hidden={tab !== 'source'}><div className="source-file"><span><span className="go-file">GO</span>main.go</span><span>{draft?.kind === 'source' ? '未应用' : '已应用源码'}</span></div>{draft?.kind === 'graph' && <div className="source-lock"><span>图形草稿编辑中，显示已应用源码。</span><button onClick={() => setConfirm({ title: '切换到代码编辑？', detail: '当前图形草稿尚未应用。放弃草稿后，可以直接编辑 Go 源码。', action: () => { edit({ type: 'discard' }); requestAnimationFrame(() => sourceRef.current?.focus()); } })}>编辑源码</button></div>}
          <div className="source-editor"><div className="line-numbers" ref={gutterRef} aria-hidden="true">{source.split('\n').map((_, i) => <div key={i} className={sourceSelection?.startLine === i + 1 && !draft ? 'line-selected' : ''}>{i + 1}</div>)}</div><textarea ref={sourceRef} aria-label="Go 源码" spellCheck={false} autoCapitalize="off" autoComplete="off" wrap="off" value={source} readOnly={draft?.kind === 'graph'} onChange={e => edit({ type: 'source', source: e.target.value })} onClick={sourceSelect} onKeyUp={sourceSelect} onScroll={e => { if (gutterRef.current) gutterRef.current.scrollTop = e.currentTarget.scrollTop; }} onKeyDown={e => { if (e.key === 'Tab' && !e.currentTarget.readOnly) { e.preventDefault(); const editor = e.currentTarget, start = editor.selectionStart, end = editor.selectionEnd; edit({ type: 'source', source: source.slice(0, start) + '\t' + source.slice(end) }); requestAnimationFrame(() => editor.setSelectionRange(start + 1, start + 1)); } }}/></div><div className="source-footer"><span>Go</span><span>UTF-8<span className="footer-dot">·</span>{source.split('\n').length} 行</span></div>{project.originalImportedSource && project.originalImportedSource !== project.source && <button className="original-source" onClick={() => download('original-import.go', project.originalImportedSource!, 'text/plain')}><Download size={12}/>导出原始源码副本</button>}</div>
        {tab === 'properties' && <Inspector ir={ir} selected={state.selected} execute={execute} select={select} locked={draft?.kind === 'source'} maxCapacity={health?.limits?.maxCapacity ?? 1024}/>}<div className="detail-footer"><span className="tiny-dot"/>结构编辑，不执行用户程序</div>
      </aside>
    </main>
    <section className={`diagnostics-panel ${diagnostics.length || state.error ? 'has-items' : ''}`} aria-label="诊断"><div className="diagnostics-header"><span><Terminal size={14}/>诊断 <b>{diagnostics.length + (state.error ? 1 : 0)}</b></span><span className={appliedBuild?.status === 'ok' ? 'success-text' : ''}>{busy ? <><LoaderCircle size={12} className="spin"/> {busy === 'build' ? '正在调用 Go 编译器…' : '正在验证程序…'}</> : appliedBuild?.message || diagnosticStatus || '所有检查结果将在这里显示'}</span></div><div className="diagnostics-body">{state.error && <button className="diagnostic error" onClick={() => dispatch({ type: 'clearError' })}><CircleAlert size={14}/><span>{state.error}</span><X size={12}/></button>}{diagnostics.map((d, i) => <button className={`diagnostic ${d.severity}`} key={`${d.code}-${i}`} onClick={() => diagnosticSelect(d)}><CircleAlert size={14}/><code>{d.phase}</code><span>{d.message}</span>{d.span && <small>main.go:{d.span.startLine}:{d.span.startColumn}</small>}</button>)}{!diagnostics.length && !state.error && <div className="diagnostics-empty">{appliedBuild?.status === 'ok' ? <CircleCheck size={15}/> : <span className="diagnostics-idle-dot"/>}<span>{appliedBuild?.status === 'ok' ? '真实 Go 工具链编译通过。编译成功不代表程序不会阻塞。' : '导入、生成和编译诊断会关联到对应节点与源码位置。'}</span></div>}</div></section>
    <footer className="statusbar"><div><span className={`tiny-dot ${connection === 'online' ? 'connected' : ''}`}/>{health?.goAvailable ? health.goVersion : connection === 'offline' ? '后端未连接' : health ? 'Go 工具链未安装' : '检查工具链…'}<span className="status-separator"/>单文件 · package main</div><div>Program IR 1.0<span className="status-separator"/>本地编译 · 不运行<span className="status-separator"/><span>⌘ / Ctrl + Enter 应用修改</span></div></footer>
    {notice && <div role="status" className="toast"><CircleCheck size={16}/>{notice}<button aria-label="关闭提示" onClick={() => setNotice('')}><X size={14}/></button></div>}
    {confirm && <div className="modal-backdrop"><div className="modal" role="dialog" aria-modal="true" aria-label={confirm.title}><div className="modal-icon"><GitBranch size={23}/></div><h2>{confirm.title}</h2><p>{confirm.detail}</p><div className="modal-actions"><button className="secondary-button" onClick={() => setConfirm(null)}>保留当前草稿</button><button className="primary-button" onClick={() => { const action = confirm.action; setConfirm(null); action(); }}>放弃草稿并继续</button></div></div></div>}
    {showHelp && <div className="modal-backdrop"><div className="modal help-modal" role="dialog" aria-modal="true" aria-label="使用帮助"><button className="modal-close icon-button" aria-label="关闭帮助" onClick={() => setShowHelp(false)}><X size={17}/></button><div className="section-eyebrow">GO CANVAS · 工作流</div><h2>从源码到图形，再回到源码。</h2><ol><li><strong>导入或构建</strong><p>打开示例、导入 Go，或新建后从工具箱添加操作。目标函数和插入位置决定程序结构。</p></li><li><strong>编辑与连接</strong><p>选中节点修改属性。将 Channel 资源端口连接到操作来改变绑定；用属性面板的上移、下移排序。</p></li><li><strong>应用与验证</strong><p>应用修改会运行语法和类型检查。编译验证调用本机 Go 工具链，不运行代码。</p></li><li><strong>保存工作</strong><p>Go 文件保存源码；项目文件还保存节点 ID、坐标、折叠状态和视口。先应用或放弃草稿再保存。</p></li></ol><div className="help-note">支持 int Channel、匿名 Goroutine、收发、关闭、int/bool 变量、打印与返回。其他 Go 语法会显示具体诊断。</div></div></div>}
  </div>;
}
