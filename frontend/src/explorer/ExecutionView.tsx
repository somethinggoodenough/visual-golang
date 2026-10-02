import { useEffect, useMemo, useState } from 'react';
import { Pause, Play, RotateCcw, SkipBack, SkipForward } from 'lucide-react';
import { useLanguage } from '../i18n';
import { causalEdges, causalPath, type ExecutionEvent, type ExecutionTrace } from './model';

function positionIn(indices: number[], cursor: number) {
  const next = indices.findIndex(index => index > cursor);
  return next === -1 ? indices.length - 1 : next - 1;
}

export function ReplayControls({ trace, cursor, onCursor, indices }: { trace: ExecutionTrace; cursor: number; onCursor: (index: number) => void; indices?: number[] }) {
  const { t } = useLanguage(); const [playing, setPlaying] = useState(false);
  const steps = useMemo(() => indices ?? trace.events.map((_, i) => i), [indices, trace]);
  const position = positionIn(steps, cursor);
  const next = steps.find(index => index > cursor);
  useEffect(() => { setPlaying(false); }, [trace]);
  useEffect(() => {
    if (!playing) return;
    if (next === undefined) { setPlaying(false); return; }
    const timer = setTimeout(() => onCursor(next), 350); return () => clearTimeout(timer);
  }, [playing, next, onCursor]);
  const move = (index: number) => { setPlaying(false); onCursor(index); };
  return <div className="replay-controls">
    <button onClick={() => move(-1)} aria-label={t('重置回放')}><RotateCcw size={15}/></button>
    <button onClick={() => move(steps[position - 1] ?? -1)} disabled={cursor < 0} aria-label={t('前一步')}><SkipBack size={15}/></button>
    <button onClick={() => setPlaying(!playing)} disabled={next === undefined} aria-label={t(playing ? '暂停' : '播放')} >{playing ? <Pause size={15}/> : <Play size={15}/>}</button>
    <button onClick={() => move(next!)} disabled={next === undefined} aria-label={t('后一步')}><SkipForward size={15}/></button>
    <input type="range" min={-1} max={Math.max(-1, steps.length - 1)} value={position} onChange={e => move(steps[Number(e.target.value)] ?? -1)} aria-label={t('回放位置')}/>
    <span>{position + 1} / {steps.length}</span>
  </div>;
}

export function ExecutionView({ trace, cursor, onCursor }: { trace?: ExecutionTrace; cursor: number; onCursor: (index: number) => void }) {
  const { t } = useLanguage(); const [all, setAll] = useState(false);
  const lanes = useMemo(() => {
    if (!trace) return [];
    const application = new Set(trace.events.filter(e => e.sourceLine || e.nodeId).map(e => e.goroutineId));
    return trace.goroutines.filter(g => all || application.has(g.id));
  }, [trace, all]);
  const indices = useMemo(() => {
    const visible = new Set(lanes.map(g => g.id));
    return trace?.events.flatMap((e, i) => visible.has(e.goroutineId) && (all || !e.instrumentation) ? [i] : []) ?? [];
  }, [trace, lanes, all]);
  if (!trace) return <div className="explorer-empty">{t('点击“运行 + Trace”录制一次真实执行。')}</div>;
  const start = Math.max(0, positionIn(indices, cursor) - 24), end = Math.min(indices.length, start + 80);
  return <div className="execution-view">
    <ReplayControls trace={trace} cursor={cursor} onCursor={onCursor} indices={indices}/>
    <div className="view-options"><label><input type="checkbox" checked={all} onChange={e => setAll(e.target.checked)}/>{t('显示 Go 内部 goroutine')}</label><span>{t('真实执行回放 · 不控制调度')}</span></div>
    <div className="goroutine-lanes">
      {lanes.map(g => {
        let state = 'UNKNOWN';
        for (let i = 0; i <= cursor; i++) { const e = trace.events[i]; if (e.goroutineId === g.id && e.toState && (all || !e.instrumentation)) state = e.toState; }
        return <div className="goroutine-lane" key={g.id}><div className="lane-title"><strong>G{g.id}</strong><span>{state}</span><small>{g.function}</small></div><div className="lane-events" style={{ width: Math.max(500, (end - start) * 30) }}>
          {indices.slice(start, end).map((index, offset) => { const e = trace.events[index]; return e.goroutineId === g.id && <button key={e.id} style={{ left: offset * 30 }} className={`lane-event ${e.kind.toLowerCase()} ${index === cursor ? 'selected' : ''} ${index > cursor ? 'future' : ''}`} title={`#${index + 1} ${e.kind}: ${e.detail ?? ''}`} aria-label={`G${g.id} #${index + 1} ${e.kind}`} onClick={() => onCursor(index)}>{index + 1}</button>; })}
        </div></div>;
      })}
      {!lanes.length && <p className="explorer-note">{t('此记录没有可定位到用户源码的事件。可显示内部 goroutine 查看底层事件。')}</p>}
    </div>
    {trace.truncated && <p className="explorer-warning">{t('Trace 达到上限，仅展示已采集部分。')}</p>}
  </div>;
}

export function CausalityView({ trace, cursor, onCursor }: { trace?: ExecutionTrace; cursor: number; onCursor: (index: number) => void }) {
  const { t } = useLanguage(); const [from, setFrom] = useState('');
  const edges = useMemo(() => causalEdges(trace?.events ?? []), [trace]);
  useEffect(() => { setFrom(''); }, [trace]);
  if (!trace) return <div className="explorer-empty">{t('录制 Trace 后检查程序顺序和因果路径。')}</div>;
  const selected = trace.events[cursor];
  const path = selected && from ? causalPath(edges, from, selected.id) : null;
  const linked = selected ? edges.filter(e => e.from === selected.id || e.to === selected.id) : [];
  return <div className="causality-view">
    <ReplayControls trace={trace} cursor={cursor} onCursor={onCursor}/>
    <p className="explorer-note">{t('这里只显示已证明的程序顺序与 goroutine 创建同步。时间先后、唤醒和相同数据值不能证明 Channel 配对。')}</p>
    <label className="causal-query">{t('从事件')} <select value={from} onChange={e => setFrom(e.target.value)}><option value="">{t('选择起点')}</option>{trace.events.filter(e => e.sourceLine || e.nodeId).map(e => <option key={e.id} value={e.id}>#{trace.events.indexOf(e) + 1} G{e.goroutineId} {e.kind} L{e.sourceLine}</option>)}</select> → {selected ? `#${cursor + 1}` : t('选择终点步骤')}</label>
    {from && selected && (path?.length ? <ol className="causal-path">{path.map(edge => <li key={`${edge.from}-${edge.to}`}><button onClick={() => onCursor(trace.events.findIndex(e => e.id === edge.from))}>{edge.from}</button><span> → {t(edge.kind === 'program_order' ? '程序顺序' : '创建同步')} → </span><button onClick={() => onCursor(trace.events.findIndex(e => e.id === edge.to))}>{edge.to}</button></li>)}</ol> : <p className="explorer-note">{t(from === selected.id ? '起点和终点是同一事件。' : '当前证据中没有这条因果路径；不代表它们一定没有因果关系。')}</p>)}
    <h3>{t('所选事件的直接关系')}</h3>
    {linked.map(edge => <p key={`${edge.from}-${edge.to}`}><code>{edge.from} → {edge.to}</code> · {t(edge.kind === 'program_order' ? '程序顺序' : '创建同步')}</p>)}
    <p className="explorer-note">{t('通信精确配对、buffer 值和共享状态尚未采集。')}</p>
  </div>;
}

export function EventExplanation({ event }: { event?: ExecutionEvent }) {
  const { t } = useLanguage();
  if (!event) return <p className="explorer-note">{t('选择步骤查看运行证据。')}</p>;
  const reasons: Record<string, string> = {
    BLOCKED_SEND: '运行时确认：此 goroutine 正在等待 Channel 发送完成。尚未记录具体 channel 实例或接收方。',
    BLOCKED_RECEIVE: '运行时确认：此 goroutine 正在等待 Channel 接收完成。尚未记录具体 channel 实例或发送方。',
    BLOCKED_SELECT: '运行时确认：此 goroutine 的 select 正在等待某个 case 可执行；当前证据不能指出未来会选哪个 case。',
    UNBLOCKED: '运行时已把此 goroutine 从等待转为可运行。可运行不代表立刻执行，唤醒来源也不能单独证明通信配对。',
    SEND_COMPLETE: '发送语句已返回。缓冲发送不要求接收方已经收到；此事件本身不能确定对应接收事件。',
    RECEIVE_COMPLETE: '接收语句已返回。它可能收到发送的值，也可能从已关闭 channel 收到零值；本记录没有捕获值或 ok 标志。',
    SELECT_CASE_CHOSEN: '执行已进入这个 select case。不能由此推断其他 case 当时是否可执行。',
  };
  const reason = reasons[event.toState ?? ''] ?? reasons[event.kind];
  return <div className="event-explanation"><strong>{event.id} · G{event.goroutineId} · {event.kind}</strong>
    {event.fromState && <p>{event.fromState} → {event.toState}</p>}
    {event.sourceLine && <p>main.go:{event.sourceLine}</p>}
    {event.detail && <pre>{t(event.detail)}</pre>}
    {event.instrumentation && <p>{t('此事件来自 Trace 采集辅助代码，不代表用户程序中的等待。')}</p>}
    {event.actorGoroutineId && event.actorGoroutineId !== event.goroutineId && <p>{t('事件由 goroutine 发出：')} G{event.actorGoroutineId}</p>}
    {!event.instrumentation && reason && <p>{t(reason)}</p>}
    <p className="explorer-note">{t(event.toState === 'WAITING' || event.toState?.startsWith('BLOCKED_') ? 'Go runtime 记录该 goroutine 进入等待；原因以上方 trace 证据为准。发送或接收尝试本身并不证明发生阻塞。' : '这是一次实际执行中的观测事件，其他运行可能采用不同调度。')}</p>
  </div>;
}
