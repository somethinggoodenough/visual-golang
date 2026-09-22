import { defaultLayout, emptyIR, EMPTY_SOURCE, type Layout, type ProgramIR, type Project } from '../model/types';
import { runCommand, type Command } from '../model/commands';

export type Draft = { kind: 'source'; source: string } | { kind: 'graph'; ir: ProgramIR } | null;
export interface Document { project: Project; draft: Draft; layout: Layout }
export interface Workspace { present: Document; past: Document[]; future: Document[]; selected: string | null; epoch: number; error: string | null }
export type Action =
  | { type: 'graph'; command: Command }
  | { type: 'source'; source: string }
  | { type: 'layout'; layout: Layout }
  | { type: 'select'; id: string | null }
  | { type: 'commit'; project: Project; reset?: boolean }
  | { type: 'discard' }
  | { type: 'undo' | 'redo' | 'clearError' };

export function initialWorkspace(): Workspace {
  const ir = emptyIR(), layout = defaultLayout(ir);
  return { present: { project: { fileFormatVersion: '1.0', programRevision: 0, ir, source: EMPTY_SOURCE, sourceMap: {}, sourceHash: '', layout }, layout, draft: null }, past: [], future: [], selected: null, epoch: 0, error: null };
}
export function currentIR(state: Workspace) { return state.present.draft?.kind === 'graph' ? state.present.draft.ir : state.present.project.ir; }
function change(state: Workspace, present: Document, selected = state.selected): Workspace {
  return { ...state, present, past: [...state.past.slice(-99), state.present], future: [], selected, epoch: state.epoch + 1, error: null };
}
export function reducer(state: Workspace, action: Action): Workspace {
  const doc = state.present;
  switch (action.type) {
    case 'select': return { ...state, selected: action.id };
    case 'clearError': return { ...state, error: null };
    case 'graph': {
      if (doc.draft?.kind === 'source') return { ...state, error: '请先应用或放弃代码草稿，再编辑图形。' };
      try {
        const { ir, selected } = runCommand(currentIR(state), action.command);
        return change(state, { ...doc, draft: { kind: 'graph', ir }, layout: defaultLayout(ir, doc.layout) }, selected ?? state.selected);
      } catch (error) { return { ...state, error: (error as Error).message }; }
    }
    case 'source': {
      if (doc.draft?.kind === 'graph') return { ...state, error: '请先应用或放弃图形草稿，再编辑代码。' };
      const draft: Draft = action.source === doc.project.source ? null : { kind: 'source', source: action.source };
      return change(state, { ...doc, draft });
    }
    case 'layout': return change(state, { ...doc, layout: action.layout });
    case 'discard': return change(state, { ...doc, draft: null, layout: defaultLayout(doc.project.ir, doc.layout) });
    case 'commit': {
      const present: Document = { project: action.project, draft: null, layout: action.project.layout };
      return action.reset
        ? { ...state, present, past: [], future: [], selected: null, epoch: state.epoch + 1, error: null }
        : change(state, present);
    }
    case 'undo': case 'redo': {
      const list = action.type === 'undo' ? state.past : state.future;
      const target = list.at(-1);
      if (!target) return state;
      const semanticChange = JSON.stringify(target.project.ir) !== JSON.stringify(doc.project.ir);
      const present = { ...target, project: { ...target.project, programRevision: semanticChange ? doc.project.programRevision + 1 : doc.project.programRevision } };
      return { ...state, present, past: action.type === 'undo' ? state.past.slice(0, -1) : [...state.past, doc], future: action.type === 'redo' ? state.future.slice(0, -1) : [...state.future, doc], epoch: state.epoch + 1, error: null };
    }
  }
}
