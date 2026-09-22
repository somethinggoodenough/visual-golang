export type ValueType = 'int' | 'bool' | 'chan int';
export type ExprIR = { id: string; kind: 'int_literal'; value: string } | { id: string; kind: 'bool_literal'; value: boolean } | { id: string; kind: 'variable'; symbolId: string };
export type StatementIR =
  | { id: string; kind: 'make_channel'; symbolId: string; capacity: number }
  | { id: string; kind: 'let'; symbolId: string; value: ExprIR }
  | { id: string; kind: 'spawn'; functionId: string }
  | { id: string; kind: 'send'; channelSymbolId: string; value: ExprIR }
  | { id: string; kind: 'receive'; channelSymbolId: string; bindings: (string | null)[] }
  | { id: string; kind: 'close'; channelSymbolId: string }
  | { id: string; kind: 'print'; value: ExprIR }
  | { id: string; kind: 'return' };
export type StatementKind = StatementIR['kind'];
export interface FunctionIR { id: string; kind: 'main' | 'goroutine'; parentFunctionId: string | null; body: { id: string; statements: StatementIR[] } }
export interface SymbolIR { id: string; name: string; type: ValueType; scopeId: string }
export interface ProgramIR { schemaVersion: '1.0'; id: string; packageName: 'main'; entryFunctionId: string; functions: FunctionIR[]; symbols: SymbolIR[] }
export interface SourceSpan { file: 'main.go'; startByte: number; endByte: number; startLine: number; startColumn: number }
export type SourceMap = Record<string, SourceSpan>;
export interface Diagnostic { code: string; severity: 'error' | 'warning' | 'info'; phase: 'parse' | 'types' | 'subset' | 'ir' | 'generate' | 'build'; message: string; nodeId?: string; span?: SourceSpan }
export interface Layout { nodes: Record<string, { x: number; y: number }>; viewport: { x: number; y: number; zoom: number }; collapsed: Record<string, boolean> }
export interface Project { fileFormatVersion: '1.0'; programRevision: number; ir: ProgramIR; layout: Layout; source: string; sourceHash: string; sourceMap: SourceMap; originalImportedSource?: string }
export interface APIResult { requestId: string; status: string; programRevision?: number; ir?: ProgramIR; source?: string; sourceHash?: string; sourceMap?: SourceMap; diagnostics?: Diagnostic[]; project?: Project; toolchainVersion?: string; durationMs?: number }
export interface Health { status: string; goAvailable: boolean; goVersion: string; limits: { maxNodes: number; maxCapacity: number } }
export const DEMO_SOURCE = `package main

import "fmt"

func main() {
    ch := make(chan int)

    go func() {
        ch <- 42
    }()

    value := <-ch
    fmt.Println(value)
}
`;
export const EMPTY_SOURCE = 'package main\n\nfunc main() {\n}\n';
export const uid = (prefix: string) => `${prefix}_${crypto.randomUUID().replaceAll('-', '').slice(0, 16)}`;
export function emptyIR(): ProgramIR {
  const id = uid('fn');
  return { schemaVersion: '1.0', id: uid('program'), packageName: 'main', entryFunctionId: id, functions: [{ id, kind: 'main', parentFunctionId: null, body: { id: uid('block'), statements: [] } }], symbols: [] };
}
export function findStatement(ir: ProgramIR, id: string) {
  for (const fn of ir.functions) { const index = fn.body.statements.findIndex(s => s.id === id); if (index >= 0) return { fn, index, statement: fn.body.statements[index] }; }
}
export function visualNodeId(ir: ProgramIR, id: string): string {
  if (ir.symbols.some(symbol => symbol.id === id && symbol.type === 'chan int') || findStatement(ir, id) || ir.functions.some(fn => fn.id === id)) return id;
  const statement = ir.functions.flatMap(fn => fn.body.statements).find(s =>
    ('value' in s && s.value.id === id) || ('symbolId' in s && s.symbolId === id) || (s.kind === 'receive' && s.bindings.includes(id)));
  return statement?.id ?? ir.functions.find(fn => fn.body.id === id)?.id ?? id;
}
export const symbolName = (ir: ProgramIR, id: string | null) => id === null ? '_' : ir.symbols.find(s => s.id === id)?.name || '未绑定';
export const exprLabel = (ir: ProgramIR, value: ExprIR) => value.kind === 'variable' ? symbolName(ir, value.symbolId) : String(value.value);
export const labels: Record<StatementKind, string> = { make_channel: '创建 Channel', spawn: '启动 Goroutine', send: '发送', receive: '接收', close: '关闭 Channel', let: '声明变量', print: '打印', return: '返回' };
export function statementLabel(ir: ProgramIR, s: StatementIR): string {
  switch (s.kind) {
    case 'make_channel': return `${symbolName(ir, s.symbolId)} := make(chan int${s.capacity ? `, ${s.capacity}` : ''})`;
    case 'spawn': return 'go func() { … }()';
    case 'send': return `${symbolName(ir, s.channelSymbolId)} ← ${exprLabel(ir, s.value)}`;
    case 'receive': return `${s.bindings.length ? `${s.bindings.map(id => symbolName(ir, id)).join(', ')} := ` : ''}← ${symbolName(ir, s.channelSymbolId)}`;
    case 'close': return `close(${symbolName(ir, s.channelSymbolId)})`;
    case 'let': return `${symbolName(ir, s.symbolId)} := ${exprLabel(ir, s.value)}`;
    case 'print': return `fmt.Println(${exprLabel(ir, s.value)})`;
    case 'return': return 'return';
  }
}
export function declaredSymbols(s: StatementIR): string[] {
  if (s.kind === 'let' || s.kind === 'make_channel') return [s.symbolId];
  if (s.kind === 'receive') return s.bindings.filter((x): x is string => x !== null);
  return [];
}
// Resolve lexical visibility at a statement, including shadowing at each declaration.
export function visibleSymbols(ir: ProgramIR, functionId: string, index: number): SymbolIR[] {
  const fn = ir.functions.find(f => f.id === functionId);
  if (!fn) return [];
  const visible = new Map<string, SymbolIR>();
  if (fn.parentFunctionId) {
    const parent = ir.functions.find(f => f.id === fn.parentFunctionId);
    const spawnIndex = parent?.body.statements.findIndex(s => s.kind === 'spawn' && s.functionId === fn.id) ?? -1;
    for (const sym of visibleSymbols(ir, fn.parentFunctionId, spawnIndex)) if (sym.type === 'chan int') visible.set(sym.name, sym);
  }
  for (const s of fn.body.statements.slice(0, Math.max(0, index))) {
    for (const id of declaredSymbols(s)) { const sym = ir.symbols.find(x => x.id === id); if (sym) visible.set(sym.name, sym); }
  }
  return [...visible.values()];
}
export function defaultLayout(ir: ProgramIR, previous?: Layout): Layout {
  const nodes: Layout['nodes'] = Object.create(null);
  const collapsed: Layout['collapsed'] = Object.create(null);
  ir.functions.forEach((fn, f) => {
    nodes[fn.id] = previous ? ownValue(previous.nodes, fn.id) ?? { x: 40 + f * 410, y: f % 2 ? 95 : 35 } : { x: 40 + f * 410, y: f % 2 ? 95 : 35 };
    collapsed[fn.id] = previous ? ownValue(previous.collapsed, fn.id) ?? false : false;
    fn.body.statements.forEach((s, i) => { nodes[s.id] = (previous && ownValue(previous.nodes, s.id)) ?? { x: 28, y: 90 + i * 110 }; });
  });
  ir.symbols.filter(s => s.type === 'chan int').forEach((s, i) => {
    nodes[s.id] = (previous && ownValue(previous.nodes, s.id)) ?? { x: 40 + i * 290, y: Math.max(550, ...ir.functions.map(f => f.body.statements.length * 110 + 175)) };
  });
  return { nodes, collapsed, viewport: previous?.viewport ?? { x: 35, y: 30, zoom: 0.85 } };
}
export const ownValue = <T,>(record: Record<string, T>, id: string): T | undefined => Object.hasOwn(record, id) ? record[id] : undefined;
export const utf8ToUtf16 = (source: string, byte: number): number => {
  let total = 0, index = 0;
  for (const char of source) { const width = new TextEncoder().encode(char).length; if (total + width > byte) break; total += width; index += char.length; }
  return index;
};
export const utf16ToUtf8 = (source: string, index: number) => new TextEncoder().encode(source.slice(0, index)).length;
