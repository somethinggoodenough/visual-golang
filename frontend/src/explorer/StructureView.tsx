import { useEffect, useMemo, useRef, useState } from 'react';
import { useLanguage } from '../i18n';
import type { AnalysisModel, AnalysisNode } from './model';

const controlKinds = new Set(['for', 'range', 'if', 'else', 'switch', 'type_switch', 'case', 'switch_case', 'select', 'select_case', 'go', 'spawn', 'return', 'continue', 'break']);

export function StructureView({ model, selected, onSelect }: { model?: AnalysisModel; selected: string | null; onSelect: (node: AnalysisNode) => void }) {
  const { t } = useLanguage();
  const [level, setLevel] = useState<'architecture' | 'control' | 'statement'>('statement');
  const [channel, setChannel] = useState<string | null>(null);
  const content = useRef<HTMLDivElement>(null);
  useEffect(() => { if (selected) setLevel('statement'); }, [selected]);
  useEffect(() => {
    if (!selected || level !== 'statement') return;
    const node = content.current?.querySelector(`[data-node-id="${CSS.escape(selected)}"]`);
    for (let parent = node?.parentElement; parent; parent = parent.parentElement) {
      if (parent instanceof HTMLDetailsElement) parent.open = true;
    }
    node?.scrollIntoView({ block: 'nearest' });
  }, [selected, level]);
  const children = useMemo(() => {
    const map = new Map<string, AnalysisNode[]>();
    for (const node of model?.nodes ?? []) {
      if (node.parentId) map.set(node.parentId, [...map.get(node.parentId) ?? [], node]);
    }
    return map;
  }, [model]);
  function renderNode(node: AnalysisNode, depth = 0): React.ReactNode {
    if (depth > 60) return null;
    const nested = (children.get(node.id) ?? []).filter(n => n.functionId === node.functionId);
    const visible = level === 'statement' || controlKinds.has(node.kind) || nested.length > 0;
    if (!visible) return null;
    const card = <button className={`structure-node ${node.kind} ${selected === node.id ? 'selected' : ''} ${channel && node.channelId === channel ? 'channel-match' : ''}`} onClick={() => onSelect(node)} data-node-id={node.id}>
      <span className="node-kind">{node.kind}</span><code>{node.label}</code><small>L{node.span.startLine}</small>{node.opaque && <em>{t('不透明调用')}</em>}
    </button>;
    return nested.length ? <details key={node.id} open className="structure-block"><summary>{node.kind} · L{node.span.startLine}</summary>{card}<div className="structure-nested">{nested.map(n => renderNode(n, depth + 1))}</div></details> : <div key={node.id}>{card}</div>;
  }
  if (!model) return <div className="explorer-empty">{t('分析源码后，查看函数、控制流与 Channel 引用。')}</div>;
  return <div className="structure-view">
    <div className="view-options" role="group" aria-label={t('结构层级')}>
      {(['architecture', 'control', 'statement'] as const).map((value, i) => <button key={value} className={level === value ? 'active' : ''} onClick={() => setLevel(value)}>{t(['架构', '控制流', '语句'][i])}</button>)}
      <span>{model.functions.length} {t('函数')} · {model.channels.length} channels</span>
    </div>
    <div className="structure-content" ref={content}>
      {level === 'architecture' ? <>
        <p className="explorer-note">{t('静态访问关系不代表通信配对或 Go 所有权保证。')}</p>
        <div className="architecture-functions">{model.functions.map(fn => <button key={fn.id} onClick={() => { const node = model.nodes.find(n => n.functionId === fn.id); if (node) onSelect(node); }}><strong>{fn.name}</strong><small>{model.nodes.filter(n => n.functionId === fn.id).length} {t('操作')}</small></button>)}</div>
        {model.channels.map(ch => <section className="channel-access" key={ch.id}>
          <h3>{ch.name} <code>{ch.type}</code></h3><small>{ch.kind} · L{ch.span.startLine}</small>
          <div>{model.nodes.filter(n => n.channelId === ch.id).map(n => <button key={n.id} className={`access-${n.kind}`} onClick={() => onSelect(n)}>{model.functions.find(fn => fn.id === n.functionId)?.name ?? 'global'} <span>→ {n.kind}</span> · L{n.span.startLine}</button>)}</div>
        </section>)}
      </> : <>
        <div className="channel-filter"><span>{t('Channel 引用')}</span><button onClick={() => setChannel(null)} className={!channel ? 'active' : ''}>{t('全部')}</button>{model.channels.map(ch => <button key={ch.id} className={channel === ch.id ? 'active' : ''} onClick={() => setChannel(ch.id)}>{ch.name}</button>)}</div>
        {model.functions.map(fn => {
          const roots = model.nodes.filter(n => n.functionId === fn.id && (!n.parentId || !model.nodes.some(parent => parent.id === n.parentId && parent.functionId === fn.id)));
          return <section key={fn.id} className="structure-function"><h3>{fn.name} <small>L{fn.span.startLine}</small></h3>{roots.map(n => renderNode(n))}</section>;
        })}
      </>}
    </div>
  </div>;
}
