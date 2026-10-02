import type { Diagnostic, SourceSpan } from '../model/types';

// Independent from editable ProgramIR: a source projection cannot safely be
// round-tripped through the deliberately smaller visual editing vocabulary.
export interface AnalysisFunction { id: string; name: string; receiver?: string; parentFunctionId?: string; span: SourceSpan }
export interface AnalysisNode { id: string; kind: string; label: string; parentId?: string; functionId?: string; channelId?: string; targetFunctionId?: string; span: SourceSpan; opaque?: boolean }
export interface AnalysisChannel { id: string; name: string; type: string; kind: string; span: SourceSpan }
export interface AnalysisModel { schemaVersion: string; packageName: string; functions: AnalysisFunction[]; nodes: AnalysisNode[]; channels: AnalysisChannel[] }
export interface AnalysisResult { requestId: string; status: string; sourceHash: string; model?: AnalysisModel; diagnostics: Diagnostic[] }

export interface ExecutionEvent { id: string; goroutineId: string; actorGoroutineId?: string; kind: string; timeNs: number; sourceLine?: number; nodeId?: string; detail?: string; fromState?: string; toState?: string; instrumentation?: boolean }
export interface GoroutineInstance { id: string; state: string; function?: string; sourceLine?: number; nodeId?: string }
export interface ExecutionTrace { schemaVersion: string; events: ExecutionEvent[]; goroutines: GoroutineInstance[]; truncated: boolean }
export interface RunResult { requestId: string; programRevision?: number; status: string; stdout: string; stderr: string; exitCode?: number | null; durationMs: number; sourceHash: string; outputTruncated: boolean; diagnostics: Diagnostic[]; trace?: ExecutionTrace; nativeTrace?: string; traceWarning?: string }

export interface CausalEdge { from: string; to: string; kind: 'program_order' | 'goroutine_start' }

// Semantic annotations emitted in one goroutine are sequenced in that goroutine.
// Scheduler transitions and timestamp order across goroutines do not establish
// application happens-before, nor do they identify matched channel operations.
export function causalEdges(events: ExecutionEvent[]): CausalEdge[] {
  const result: CausalEdge[] = [], last = new Map<string, string>(), created = new Map<string, string>();
  for (const event of events) {
    if (event.instrumentation) continue;
    const isCreate = event.kind === 'GOROUTINE_CREATE' && event.actorGoroutineId && event.actorGoroutineId !== event.goroutineId;
    const semantic = !event.fromState && Boolean(event.sourceLine || event.nodeId);
    if (!semantic && !isCreate) continue;
    const actor = isCreate ? event.actorGoroutineId! : event.goroutineId;
    const previous = last.get(actor);
    if (previous) result.push({ from: previous, to: event.id, kind: 'program_order' });
    if (created.has(actor)) { result.push({ from: created.get(actor)!, to: event.id, kind: 'goroutine_start' }); created.delete(actor); }
    last.set(actor, event.id);
    if (isCreate) created.set(event.goroutineId, event.id);
  }
  return result;
}

export function causalPath(edges: CausalEdge[], from: string, to: string): CausalEdge[] | null {
  if (from === to) return [];
  const children = new Map<string, CausalEdge[]>();
  for (const edge of edges) children.set(edge.from, [...children.get(edge.from) ?? [], edge]);
  const queue = [from], visited = new Map<string, CausalEdge>();
  for (let i = 0; i < queue.length; i++) {
    for (const edge of children.get(queue[i]) ?? []) {
      if (visited.has(edge.to) || edge.to === from) continue;
      visited.set(edge.to, edge);
      if (edge.to === to) {
        const path: CausalEdge[] = []; let current = to;
        while (current !== from) { const step = visited.get(current)!; path.unshift(step); current = step.from; }
        return path;
      }
      queue.push(edge.to);
    }
  }
  return null;
}

export function eventNode(event: ExecutionEvent | undefined, model: AnalysisModel | undefined): AnalysisNode | undefined {
  if (!event || !model) return;
  if (event.nodeId) return model.nodes.find(node => node.id === event.nodeId);
  // Multiple operations may share a line; without an exact node mapping do not
  // invent which one ran. The source line can still be highlighted.
  const sameLine = model.nodes.filter(node => node.span.startLine === event.sourceLine);
  return sameLine.length === 1 ? sameLine[0] : undefined;
}

export function lineSpan(source: string, line: number): SourceSpan {
  const lines = source.split('\n'); const before = lines.slice(0, Math.max(0, line - 1)).join('\n');
  const startByte = new TextEncoder().encode(before + (line > 1 ? '\n' : '')).length;
  return { file: 'main.go', startByte, endByte: startByte + new TextEncoder().encode(lines[line - 1] ?? '').length, startLine: line, startColumn: 1 };
}
