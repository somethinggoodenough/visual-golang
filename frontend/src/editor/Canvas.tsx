import { useEffect, useMemo, useRef, useState } from 'react';
import { Background, BackgroundVariant, Controls, Handle, MarkerType, Position, ReactFlow, applyNodeChanges, useNodesInitialized, useReactFlow, type Connection, type Edge, type Node, type NodeProps, type ReactFlowInstance } from '@xyflow/react';
import { ArrowDownToLine, ArrowUpFromLine, Box, ChevronDown, ChevronRight, CircleStop, Code2, GitFork, Layers, LogOut, Terminal } from 'lucide-react';
import { labels, ownValue, statementLabel, type Layout, type ProgramIR, type StatementKind } from '../model/types';
import '@xyflow/react/dist/style.css';

const icons = { make_channel: Box, spawn: GitFork, send: ArrowUpFromLine, receive: ArrowDownToLine, close: CircleStop, let: Code2, print: Terminal, return: LogOut };
interface VisualData extends Record<string, unknown> { title: string; detail: string; kind?: StatementKind; index?: number; collapsed?: boolean; onToggle?: () => void; count?: number }
type VisualNode = Node<VisualData>;
function OperationNode({ data, selected }: NodeProps<VisualNode>) {
  const Icon = icons[data.kind!];
  return <div className={`operation-node ${data.kind} ${selected ? 'is-selected' : ''}`}>
    <Handle type="target" position={Position.Top} id="sequence-in" isConnectable={false} className="sequence-handle" />
    <div className="operation-heading"><span className="operation-icon"><Icon size={15} /></span><span>{data.title}</span><small>{String(data.index).padStart(2, '0')}</small></div>
    <code>{data.detail}</code>
    <Handle type="source" position={Position.Bottom} id="sequence-out" isConnectable={false} className="sequence-handle" />
    {['send', 'receive', 'close'].includes(data.kind!) && <Handle type="target" position={Position.Right} id="channel" className="channel-handle" title="从 Channel 资源拖动连接到此端口" />}
    {data.kind === 'make_channel' && <Handle type="target" position={Position.Left} id="declaration" isConnectable={false} className="channel-handle" />}
    {data.kind === 'spawn' && <Handle type="source" position={Position.Right} id="spawn" isConnectable={false} className="spawn-handle" />}
  </div>;
}
function FunctionNode({ data, selected }: NodeProps<VisualNode>) {
  return <div className={`function-node ${selected ? 'is-selected' : ''}`}>
    <div className="function-heading"><span className={`function-mark ${data.title === 'main' ? '' : 'purple'}`}>ƒ</span><div><strong>{data.title}</strong><small>{data.detail}</small></div><button className="nodrag icon-button" title={data.collapsed ? '展开函数' : '折叠函数'} onClick={data.onToggle}>{data.collapsed ? <ChevronRight size={15} /> : <ChevronDown size={15} />}</button></div>
    <span className="function-count">{data.count} 个操作</span>
    {data.count === 0 && !data.collapsed && <div className="empty-function">从左侧添加第一个操作<br/><span>语句按编号依次执行</span></div>}
    <Handle type="target" position={Position.Left} id="spawn-in" isConnectable={false} className="spawn-handle" style={{ top: 30 }} />
  </div>;
}
function ResourceNode({ data, selected }: NodeProps<VisualNode>) {
  return <div className={`resource-node ${selected ? 'is-selected' : ''}`}>
    <div className="resource-heading"><span className="resource-icon"><Layers size={17} /></span><strong>{data.title}</strong><span className="tiny-badge">chan int</span></div>
    <div className="resource-detail"><span>{data.detail}</span><span>{data.count} 个引用</span></div>
    <Handle type="source" position={Position.Right} id="channel" className="channel-handle" title="拖动到发送、接收或关闭操作以绑定 Channel" />
  </div>;
}
const nodeTypes = { operation: OperationNode, function: FunctionNode, resource: ResourceNode };

// Fit only freshly imported programs. Saved-project viewport changes do not trigger this.
function FitImportedView({ request, expectedNodes }: { request: number; expectedNodes: string }) {
  const initialized = useNodesInitialized();
  const { fitView, getNodes } = useReactFlow();
  const fitted = useRef(0);
  useEffect(() => {
    if (!initialized || request <= fitted.current || JSON.stringify(getNodes().map(node => node.id)) !== expectedNodes) return;
    const frame = requestAnimationFrame(() => {
      fitted.current = request;
      void fitView({ padding: 0.17, minZoom: 0.15, maxZoom: 0.95 });
    });
    return () => cancelAnimationFrame(frame);
  }, [initialized, request, expectedNodes, fitView, getNodes]);
  return null;
}
interface Props { ir: ProgramIR; layout: Layout; fitRequest: number; selected: string | null; onSelect: (id: string) => void; onLayout: (layout: Layout) => void; onConnect: (statementId: string, symbolId: string) => void; onAdd: (kind: StatementKind, functionId: string) => void; onError: (message: string) => void; locked: boolean }
export function Canvas({ ir, layout, fitRequest, selected, onSelect, onLayout, onConnect, onAdd, onError, locked }: Props) {
  const instance = useRef<ReactFlowInstance<VisualNode, Edge> | null>(null);
  const [ready, setReady] = useState(false);
  const projection = useMemo(() => {
    const nodes: VisualNode[] = [], edges: Edge[] = [];
    const nodeIds = new Set<string>();
    ir.functions.forEach((fn, i) => {
      const collapsed = ownValue(layout.collapsed, fn.id);
      const positions = fn.body.statements.map(s => ownValue(layout.nodes, s.id));
      const width = Math.max(325, ...positions.map(p => (p?.x ?? 28) + 295));
      const height = collapsed ? 78 : Math.max(205, ...positions.map(p => (p?.y ?? 90) + 112));
      nodes.push({ id: fn.id, type: 'function', position: ownValue(layout.nodes, fn.id) ?? { x: i * 410, y: 30 }, data: { title: fn.kind === 'main' ? 'main' : `goroutine ${i}`, detail: fn.kind === 'main' ? '程序入口 · main goroutine' : `词法父级 · ${ir.functions.findIndex(f => f.id === fn.parentFunctionId) === 0 ? 'main' : `goroutine ${ir.functions.findIndex(f => f.id === fn.parentFunctionId)}`}`, collapsed, count: fn.body.statements.length, onToggle: () => onLayout({ ...layout, collapsed: { ...layout.collapsed, [fn.id]: !collapsed } }) }, style: { width, height }, selected: selected === fn.id, dragHandle: '.function-heading' });
      nodeIds.add(fn.id);
      fn.body.statements.forEach((s, index) => {
        nodes.push({ id: s.id, type: 'operation', parentId: fn.id, extent: 'parent', position: ownValue(layout.nodes, s.id) ?? { x: 28, y: 90 + index * 110 }, data: { title: labels[s.kind], detail: statementLabel(ir, s), kind: s.kind, index: index + 1 }, selected: selected === s.id, hidden: collapsed, style: { width: 270 } });
        if (!collapsed) nodeIds.add(s.id);
        if (index > 0) edges.push({ id: `sequence:${s.id}`, source: fn.body.statements[index - 1].id, target: s.id, sourceHandle: 'sequence-out', targetHandle: 'sequence-in', type: 'smoothstep', style: { stroke: '#617077', strokeWidth: 1.4 }, markerEnd: { type: MarkerType.ArrowClosed, color: '#617077', width: 12, height: 12 }, selectable: false });
        if (s.kind === 'spawn') edges.push({ id: `spawn:${s.id}`, source: s.id, target: s.functionId, sourceHandle: 'spawn', targetHandle: 'spawn-in', type: 'smoothstep', label: 'go · 创建', style: { stroke: '#a494ed', strokeWidth: 1.6, strokeDasharray: '7 5' }, labelStyle: { fill: '#b8a4f5', fontSize: 10 }, labelBgStyle: { fill: '#161a23' }, markerEnd: { type: MarkerType.ArrowClosed, color: '#a494ed' }, selectable: false });
        if ('channelSymbolId' in s && s.channelSymbolId) edges.push({ id: `reference:${s.id}`, source: s.channelSymbolId, target: s.id, sourceHandle: 'channel', targetHandle: 'channel', type: 'default', label: `引用 · ${labels[s.kind]}`, style: { stroke: '#68c8ac', strokeWidth: 1.3, strokeDasharray: '3 5' }, labelStyle: { fill: '#89c8b5', fontSize: 10 }, labelBgStyle: { fill: '#141c1c' }, reconnectable: 'source', selectable: true });
        if (s.kind === 'make_channel') edges.push({ id: `resource-declaration:${s.id}`, source: s.symbolId, target: s.id, sourceHandle: 'channel', targetHandle: 'declaration', type: 'default', label: '创建位置', style: { stroke: '#41695d', strokeWidth: 1, strokeDasharray: '2 6' }, labelStyle: { fill: '#769d90', fontSize: 10 }, labelBgStyle: { fill: '#141c1c' }, selectable: false });
      });
    });
    ir.symbols.filter(sym => sym.type === 'chan int').forEach(sym => {
      const declaration = ir.functions.flatMap(f => f.body.statements).find(s => s.kind === 'make_channel' && s.symbolId === sym.id);
      const capacity = declaration?.kind === 'make_channel' ? declaration.capacity : 0;
      const count = ir.functions.flatMap(f => f.body.statements).filter(s => 'channelSymbolId' in s && s.channelSymbolId === sym.id).length;
      const id = sym.id;
      nodeIds.add(id);
      nodes.push({ id, type: 'resource', position: ownValue(layout.nodes, id) ?? { x: 50, y: 600 }, data: { title: sym.name, detail: capacity === 0 ? '无缓冲 · unbuffered' : `缓冲容量 ${capacity}`, count }, selected: selected === id, style: { width: 270 } });
    });
    return { nodes, edges: edges.filter(e => nodeIds.has(e.source) && nodeIds.has(e.target)) };
  }, [ir, layout, selected, onLayout]);
  const [nodes, setNodes] = useState<VisualNode[]>(projection.nodes);
  useEffect(() => setNodes(current => {
    const previous = new Map(current.map(node => [node.id, node]));
    return projection.nodes.map(node => ({ ...node, measured: previous.get(node.id)?.measured }));
  }), [projection.nodes]);
  useEffect(() => {
    if (ready && instance.current) void instance.current.setViewport(layout.viewport);
  }, [ready, layout.viewport]);
  const connect = (c: Connection) => {
    if (locked) { onError('请先应用或放弃代码草稿，再编辑 Channel 绑定。'); return; }
    if (!ir.symbols.some(sym => sym.id === c.source && sym.type === 'chan int') || c.targetHandle !== 'channel') { onError('请将 Channel 资源的圆形端口连接到发送、接收或关闭操作。'); return; }
    onConnect(c.target, c.source);
  };
  return <div className="canvas" onDragOver={e => { e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; }} onDrop={e => {
    e.preventDefault(); const kind = e.dataTransfer.getData('application/goviz') as StatementKind;
    if (!labels[kind] || !instance.current) return;
    const point = instance.current.screenToFlowPosition({ x: e.clientX, y: e.clientY });
    const fn = [...nodes].reverse().find(n => n.type === 'function' && point.x >= n.position.x && point.x <= n.position.x + Number(n.style?.width) && point.y >= n.position.y && point.y <= n.position.y + Number(n.style?.height));
    onAdd(kind, fn?.id ?? ir.entryFunctionId);
  }}>
    <ReactFlow<VisualNode, Edge> nodes={nodes} edges={projection.edges} nodeTypes={nodeTypes} onNodesChange={changes => setNodes(current => applyNodeChanges(changes, current))} onNodeClick={(_, node) => onSelect(node.id)} onNodeDragStop={(_, node) => onLayout({ ...layout, nodes: { ...layout.nodes, [node.id]: node.position } })} onConnect={connect} onReconnect={(_, c) => connect(c)} onMoveEnd={(_, viewport) => {
      if (Math.abs(viewport.x - layout.viewport.x) + Math.abs(viewport.y - layout.viewport.y) + Math.abs(viewport.zoom - layout.viewport.zoom) > 0.001) onLayout({ ...layout, viewport });
    }} onInit={flow => { instance.current = flow; setReady(true); }} defaultViewport={layout.viewport} minZoom={0.25} maxZoom={1.5} deleteKeyCode={null} nodesConnectable={!locked} colorMode="dark" elevateEdgesOnSelect onlyRenderVisibleElements={false}>
      <FitImportedView request={fitRequest} expectedNodes={JSON.stringify(projection.nodes.map(node => node.id))}/><Background variant={BackgroundVariant.Dots} color="#303b3e" gap={22} size={1} />
      <Controls showInteractive={false} position="bottom-left" />
    </ReactFlow>
    <div className="canvas-legend"><span><i className="legend-line sequence"/>语句顺序</span><span><i className="legend-line spawn"/>并发创建</span><span><i className="legend-line channel"/>Channel 引用</span></div>
    <div className="canvas-note">拖动调整布局 · 通过编号调整执行顺序</div>
  </div>;
}
