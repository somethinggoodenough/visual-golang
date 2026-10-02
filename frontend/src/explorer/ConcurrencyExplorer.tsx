import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft, CheckCheck, Download, LoaderCircle, Play, Square, WandSparkles } from 'lucide-react';
import { download } from '../api/client';
import { SourceEditor, type SourceEditorHandle } from '../editor/SourceEditor';
import { useLanguage } from '../i18n';
import { uid, utf16ToUtf8, type APIResult, type SourceSpan } from '../model/types';
import { CausalityView, EventExplanation, ExecutionView } from './ExecutionView';
import { StructureView } from './StructureView';
import { eventNode, lineSpan, type AnalysisNode } from './model';
import { useExplorer } from './useExplorer';
import kvstoreSource from '../../../examples/kvstore.go?raw';
import './explorer.css';

export function ConcurrencyExplorer({ source, onChange, onBack }: { source: string; onChange: (text: string) => void; onBack: () => void }) {
  const { language, setLanguage, t } = useLanguage();
  const work = useExplorer(source);
  const [view, setView] = useState<'structure' | 'execution' | 'causality'>('structure');
  const [panel, setPanel] = useState<'problems' | 'output' | 'trace' | 'explanation'>('problems');
  const [cursor, setCursor] = useState(-1);
  const [selected, setSelected] = useState<string | null>(null);
  const [selection, setSelection] = useState<SourceSpan | null>(null);
  const [lastAction, setLastAction] = useState('');
  const [formatting, setFormatting] = useState(false);
  const [notice, setNotice] = useState('');
  const sourceRef = useRef<SourceEditorHandle>(null);
  const currentSource = useRef(source); currentSource.current = source;
  const formatRequest = useRef<AbortController | null>(null);
  const model = work.analysis?.model;
  const events = work.result?.trace?.events ?? [];
  const event = events[cursor];
  const busy = Boolean(work.busy) || formatting;

  useEffect(() => { void work.perform('analyze'); }, [work.perform]);
  useEffect(() => { setCursor(-1); setSelected(null); setSelection(null); setNotice(''); setFormatting(false); formatRequest.current?.abort(); }, [source]);
  useEffect(() => () => formatRequest.current?.abort(), []);
  useEffect(() => { setCursor(-1); }, [work.result]);

  const highlight = useCallback((span: SourceSpan) => {
    setSelection(span); sourceRef.current?.highlight(span, false);
  }, []);
  const selectNode = useCallback((node: AnalysisNode) => { setSelected(node.id); highlight(node.span); }, [highlight]);
  const selectStep = useCallback((index: number) => {
    setCursor(index);
    const step = work.result?.trace?.events[index];
    const node = eventNode(step, work.analysis?.model);
    if (node) { setSelected(node.id); highlight(node.span); }
    else { setSelected(null); if (step?.sourceLine) highlight(lineSpan(currentSource.current, step.sourceLine)); else setSelection(null); }
  }, [work.result, work.analysis, highlight]);

  async function perform(mode: 'analyze' | 'build-source' | 'run' | 'trace') {
    setLastAction(mode); setCursor(-1);
    setPanel(mode === 'analyze' ? 'problems' : 'output');
    if (mode === 'trace') setView('execution');
    await work.perform(mode);
  }

  async function format() {
    if (busy) return;
    const text = currentSource.current, controller = new AbortController(), requestId = uid('format');
    formatRequest.current?.abort(); formatRequest.current = controller; setFormatting(true);
    try {
      const response = await fetch('/api/format', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ requestId, source: text }), signal: controller.signal });
      const result: APIResult = await response.json();
      if (controller.signal.aborted || text !== currentSource.current || result.requestId !== requestId) return;
      work.setDiagnostics(result.diagnostics ?? []);
      if (response.ok && result.status === 'ok' && result.source !== undefined) {
        sourceRef.current?.replaceSource(result.source);
        setNotice('源码已格式化，应用后更新图形。');
      } else { setPanel('problems'); setNotice('格式化失败，源码已保留。'); }
    } catch (reason) {
      if (!controller.signal.aborted && text === currentSource.current) setNotice(String(reason));
    } finally { if (formatRequest.current === controller) setFormatting(false); }
  }

  function downloadTrace() {
    if (!work.result?.nativeTrace) return;
    const data = Uint8Array.from(atob(work.result.nativeTrace), c => c.charCodeAt(0));
    const url = URL.createObjectURL(new Blob([data], { type: 'application/octet-stream' }));
    const link = document.createElement('a'); link.href = url; link.download = 'execution.trace'; link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  const statusLabels: Record<string, string> = { ok: lastAction === 'build-source' ? '编译通过' : '运行完成', compile_error: '编译失败', runtime_error: '运行时错误', timeout: '运行或编译超时', deadlock: 'Go 检测到死锁', cancelled: '操作已停止。', toolchain_unavailable: 'Go 工具链不可用' };
  const windowStart = Math.max(0, cursor - 75);

  return <div className="app-shell explorer-shell">
    <header className="explorer-header"><button onClick={onBack}><ArrowLeft size={16}/>{t('返回图形编辑')}</button><strong>Go Canvas <span>· {t('并发执行探索')}</span></strong><button className="language-toggle" onClick={() => setLanguage(language === 'zh' ? 'en' : 'zh')}>{language === 'zh' ? 'English' : '中文'}</button></header>
    <nav className="explorer-toolbar" aria-label={t('探索操作')}>
      <button onClick={() => onChange(kvstoreSource)} disabled={busy}>{t('KVStore 示例')}</button>
      <button onClick={() => download('main.go', source, 'text/plain')}><Download size={14}/>{t('导出 Go')}</button>
      <button onClick={() => void format()} disabled={busy}><WandSparkles size={14}/>{t('格式化 Go')}</button>
      <button onClick={() => void perform('analyze')} disabled={busy}>{t('分析源码')}</button>
      <div className="toolbar-spacer"/>
      <button onClick={() => void perform('build-source')} disabled={busy}><CheckCheck size={14}/>{t('编译验证')}</button>
      <button onClick={() => void perform('run')} disabled={busy}><Play size={14}/>{t('运行')}</button>
      <button className="run-trace" onClick={() => void perform('trace')} disabled={busy}><Play size={14}/>{t('运行 + Trace')}</button>
      {work.busy && <button onClick={work.cancel}><Square size={14}/>{t('停止')}</button>}
    </nav>
    <div className="explorer-caption">{t('本地可信源码 · 单文件标准库 · Trace 记录一次真实执行，插桩可能影响调度')}</div>
    <main className="explorer-workspace">
      <section className="explorer-main">
        <div className="explorer-tabs" role="tablist" aria-label={t('探索视图')}>{(['structure', 'execution', 'causality'] as const).map((value, i) => <button role="tab" aria-selected={view === value} className={view === value ? 'active' : ''} key={value} onClick={() => setView(value)}>{t(['结构', '执行', '因果'][i])}</button>)}</div>
        {view === 'structure' && <StructureView model={model} selected={selected} onSelect={selectNode}/>}
        {view === 'execution' && <ExecutionView trace={work.result?.trace} cursor={cursor} onCursor={selectStep}/>}
        {view === 'causality' && <CausalityView trace={work.result?.trace} cursor={cursor} onCursor={selectStep}/>}
      </section>
      <aside className="explorer-source"><div className="source-file"><span>main.go</span><span>{t('源码分析视图 · 修改后重新分析')}</span></div>
        <SourceEditor ref={sourceRef} value={source} diagnostics={work.diagnostics} selection={selection} onChange={onChange} onApply={() => void perform('analyze')} onFormat={() => void format()} ariaLabel={t('Go 源码')} formatLabel={t('格式化 Go')} onCursorOffset={offset => {
          const byte = utf16ToUtf8(source, offset);
          const match = model?.nodes.filter(n => n.span.startByte <= byte && byte < n.span.endByte).sort((a,b) => a.span.endByte-a.span.startByte-(b.span.endByte-b.span.startByte))[0];
          setSelected(match?.id ?? null);
        }}/>
        {selected && <div className="explorer-selected"><code>{model?.nodes.find(n => n.id === selected)?.label}</code></div>}
      </aside>
    </main>
    <section className="explorer-bottom">
      <div className="explorer-tabs" role="tablist" aria-label={t('结果面板')}>{(['problems','output','trace','explanation'] as const).map((value,i) => <button role="tab" aria-selected={panel === value} className={panel === value ? 'active' : ''} key={value} onClick={() => setPanel(value)}>{t(['问题','输出','Trace','解释'][i])}{value === 'problems' && work.diagnostics.length > 0 ? ` (${work.diagnostics.length})` : ''}</button>)}
        <span role="status" className="explorer-status">{busy ? <><LoaderCircle size={12} className="spin"/>{t(formatting ? '正在格式化源码…' : work.busy === 'analyze' ? '正在分析源码…' : '正在编译或运行…')}</> : work.result ? `${t(statusLabels[work.result.status] ?? work.result.status)} · ${work.result.durationMs} ms` : work.analysis ? t(work.analysis.status === 'ok' ? '分析完成' : '分析未通过') : ''}</span>
      </div>
      <div className="explorer-bottom-content">
        {(work.error || notice) && <p className="explorer-warning" role="alert">{t(work.error || notice)}</p>}
        {panel === 'problems' && (work.diagnostics.length ? work.diagnostics.map((d,i) => <button key={i} className={`diagnostic ${d.severity}`} onClick={() => { if (d.span) highlight(d.span); }}><code>{d.phase}</code>{t(d.message)}{d.span && ` · main.go:${d.span.startLine}`}</button>) : <p className="explorer-note">{t('诊断将在分析或编译后显示。')}</p>)}
        {panel === 'output' && (work.result ? <div className="run-output"><span>{t('退出码')}：{work.result.exitCode ?? '—'}</span><div><h4>stdout</h4><pre aria-label="stdout">{work.result.stdout || t('无输出')}</pre></div><div><h4>stderr</h4><pre aria-label="stderr">{work.result.stderr || t('无输出')}</pre></div>{work.result.outputTruncated && <p className="explorer-warning">{t('输出达到上限，已截断。')}</p>}{work.result.traceWarning && <p className="explorer-warning">{work.result.traceWarning}</p>}</div> : <p className="explorer-note">{t('运行结果将在这里显示。')}</p>)}
        {panel === 'trace' && <><div className="trace-actions"><button disabled={!work.result?.nativeTrace} onClick={downloadTrace}><Download size={13}/>{t('下载原始 Trace')}</button><span>{events.length} {t('事件')}</span></div><div className="trace-list">{events.slice(windowStart, windowStart+200).map((e,offset) => <button key={e.id} className={cursor === windowStart+offset ? 'active' : ''} onClick={() => { selectStep(windowStart+offset); setView('execution'); }}><span>#{windowStart+offset+1}</span><span>G{e.goroutineId}</span><strong>{e.kind}</strong><small>{(e.timeNs/1000000).toFixed(3)} ms {e.sourceLine ? `· L${e.sourceLine}` : ''}</small></button>)}</div></>}
        {panel === 'explanation' && <EventExplanation event={event}/>}
      </div>
    </section>
  </div>;
}
