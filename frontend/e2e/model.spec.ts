import { expect, test } from '@playwright/test';
import { runCommand, type Command } from '../src/model/commands';
import {
  emptyIR, findStatement, statementLabel, utf16ToUtf8, utf8ToUtf16,
  visibleSymbols, type ProgramIR, type StatementIR,
} from '../src/model/types';
import { currentIR, initialWorkspace, reducer } from '../src/state/workspace';

function buildDemo() {
  let ir = emptyIR();
  const main = ir.entryFunctionId;
  const apply = (command: Command) => {
    const result = runCommand(ir, command);
    ir = result.ir;
    return result.selected!;
  };
  const channelStatement = apply({ type: 'AddStatement', functionId: main, index: 0, kind: 'make_channel' });
  const channel = (findStatement(ir, channelStatement)!.statement as Extract<StatementIR, { kind: 'make_channel' }>).symbolId;
  const spawn = apply({ type: 'AddStatement', functionId: main, index: 1, kind: 'spawn' });
  const child = (findStatement(ir, spawn)!.statement as Extract<StatementIR, { kind: 'spawn' }>).functionId;
  const send = apply({ type: 'AddStatement', functionId: child, index: 0, kind: 'send' });
  const receive = apply({ type: 'AddStatement', functionId: main, index: 2, kind: 'receive' });
  const print = apply({ type: 'AddStatement', functionId: main, index: 3, kind: 'print' });
  return { ir, main, child, channelStatement, channel, spawn, send, receive, print };
}

function statement(ir: ProgramIR, id: string): StatementIR {
  const found = findStatement(ir, id);
  expect(found).toBeDefined();
  return found!.statement;
}

test('build the reference program entirely from semantic commands', () => {
  const demo = buildDemo();
  const main = demo.ir.functions.find(fn => fn.id === demo.main)!;
  const child = demo.ir.functions.find(fn => fn.id === demo.child)!;
  expect(main.body.statements.map(s => s.kind)).toEqual(['make_channel', 'spawn', 'receive', 'print']);
  expect(child.parentFunctionId).toBe(main.id);
  expect(child.body.statements).toHaveLength(1);
  expect(child.body.statements[0]).toMatchObject({ kind: 'send', channelSymbolId: demo.channel, value: { kind: 'int_literal', value: '42' } });
  const receive = statement(demo.ir, demo.receive) as Extract<StatementIR, { kind: 'receive' }>;
  expect(receive.channelSymbolId).toBe(demo.channel);
  expect(receive.bindings).toHaveLength(1);
  expect(statement(demo.ir, demo.print)).toMatchObject({ kind: 'print', value: { kind: 'variable', symbolId: receive.bindings[0] } });
  expect(demo.ir.symbols.map(s => [s.name, s.type])).toEqual([['ch', 'chan int'], ['value', 'int']]);

  const updated = runCommand(demo.ir, { type: 'UpdateLiteral', statementId: demo.send, value: '100' }).ir;
  expect(statementLabel(updated, statement(updated, demo.send))).toBe('ch ← 100');
  expect(statementLabel(demo.ir, statement(demo.ir, demo.send))).toBe('ch ← 42');
  expect(updated.functions[0].body.statements.map(s => s.id)).toEqual(main.body.statements.map(s => s.id));
});

test('channel references obey declaration order, lexical capture, and shadowing', () => {
  let ir = emptyIR();
  const main = ir.entryFunctionId;
  const add = (functionId: string, index: number, kind: StatementIR['kind']) => {
    const result = runCommand(ir, { type: 'AddStatement', functionId, index, kind });
    ir = result.ir;
    return statement(ir, result.selected!);
  };
  const outer = add(main, 0, 'make_channel') as Extract<StatementIR, { kind: 'make_channel' }>;
  const local = add(main, 1, 'let') as Extract<StatementIR, { kind: 'let' }>;
  const spawn = add(main, 2, 'spawn') as Extract<StatementIR, { kind: 'spawn' }>;
  const late = add(main, 3, 'make_channel') as Extract<StatementIR, { kind: 'make_channel' }>;
  expect(visibleSymbols(ir, main, 0)).toEqual([]);
  expect(visibleSymbols(ir, main, 2).map(s => s.id)).toEqual([outer.symbolId, local.symbolId]);
  expect(visibleSymbols(ir, spawn.functionId, 0).map(s => s.id)).toEqual([outer.symbolId]);
  const before = add(spawn.functionId, 0, 'send') as Extract<StatementIR, { kind: 'send' }>;
  expect(before.channelSymbolId).toBe(outer.symbolId);
  const shadow = add(spawn.functionId, 1, 'make_channel') as Extract<StatementIR, { kind: 'make_channel' }>;
  expect(ir.symbols.find(s => s.id === shadow.symbolId)!.name).toBe('ch');
  expect(visibleSymbols(ir, spawn.functionId, 1).map(s => s.id)).toEqual([outer.symbolId]);
  expect(visibleSymbols(ir, spawn.functionId, 2).map(s => s.id)).toEqual([shadow.symbolId]);
  const after = add(spawn.functionId, 2, 'send') as Extract<StatementIR, { kind: 'send' }>;
  expect(after.channelSymbolId).toBe(shadow.symbolId);
  expect(() => runCommand(ir, { type: 'BindChannel', statementId: before.id, symbolId: late.symbolId })).toThrow(/不可见/);
  expect(() => runCommand(ir, { type: 'BindChannel', statementId: before.id, symbolId: local.symbolId })).toThrow(/不可见/);
  expect(() => runCommand(ir, { type: 'BindChannel', statementId: after.id, symbolId: outer.symbolId })).toThrow(/不可见/);
});

test('an int shadow hides an inherited channel from nested goroutines', () => {
  const demo = buildDemo();
  let result = runCommand(demo.ir, { type: 'AddStatement', functionId: demo.child, index: 1, kind: 'let' });
  const local = statement(result.ir, result.selected!) as Extract<StatementIR, { kind: 'let' }>;
  let ir = runCommand(result.ir, { type: 'RenameSymbol', symbolId: local.symbolId, name: 'ch' }).ir;
  result = runCommand(ir, { type: 'AddStatement', functionId: demo.child, index: 2, kind: 'spawn' });
  ir = result.ir;
  const grandchild = (statement(ir, result.selected!) as Extract<StatementIR, { kind: 'spawn' }>).functionId;
  expect(visibleSymbols(ir, demo.child, 2).map(s => [s.name, s.type])).toEqual([['ch', 'int']]);
  expect(visibleSymbols(ir, grandchild, 0)).toEqual([]);
});

test('deleting declarations with surviving references is rejected without mutation', () => {
  const demo = buildDemo();
  const snapshot = structuredClone(demo.ir);
  expect(() => runCommand(demo.ir, { type: 'DeleteStatement', statementId: demo.channelStatement })).toThrow(/仍有 2 个操作引用/);
  expect(() => runCommand(demo.ir, { type: 'DeleteStatement', statementId: demo.receive })).toThrow(/仍有 1 个操作引用/);
  expect(demo.ir).toEqual(snapshot);

  let ir = runCommand(demo.ir, { type: 'DeleteGoroutine', functionId: demo.child }).ir;
  expect(ir.functions.map(f => f.id)).toEqual([demo.main]);
  expect(ir.symbols.some(s => s.id === demo.channel)).toBe(true);
  ir = runCommand(ir, { type: 'DeleteStatement', statementId: demo.print }).ir;
  ir = runCommand(ir, { type: 'DeleteStatement', statementId: demo.receive }).ir;
  ir = runCommand(ir, { type: 'DeleteStatement', statementId: demo.channelStatement }).ir;
  expect(ir.functions[0].body.statements).toEqual([]);
  expect(ir.symbols).toEqual([]);
});

test('receive editing retains stable identities and correct int/bool slots', () => {
  const demo = buildDemo();
  let ir = runCommand(demo.ir, { type: 'DeleteStatement', statementId: demo.print }).ir;
  const original = statement(ir, demo.receive) as Extract<StatementIR, { kind: 'receive' }>;
  const valueID = original.bindings[0];
  ir = runCommand(ir, { type: 'UpdateReceiveBindings', statementId: demo.receive, names: ['number', 'open'] }).ir;
  const both = statement(ir, demo.receive) as Extract<StatementIR, { kind: 'receive' }>;
  expect(both.bindings[0]).toBe(valueID);
  expect(both.bindings[1]).toBeTruthy();
  expect(both.bindings.map(id => ir.symbols.find(s => s.id === id))).toMatchObject([
    { name: 'number', type: 'int' }, { name: 'open', type: 'bool' },
  ]);
  const okID = both.bindings[1];
  ir = runCommand(ir, { type: 'UpdateReceiveBindings', statementId: demo.receive, names: [null, 'ok'] }).ir;
  expect(statement(ir, demo.receive)).toMatchObject({ bindings: [null, okID] });
  expect(ir.symbols.some(s => s.id === valueID)).toBe(false);
  expect(ir.symbols.find(s => s.id === okID)).toMatchObject({ name: 'ok', type: 'bool' });
  ir = runCommand(ir, { type: 'UpdateReceiveBindings', statementId: demo.receive, names: [] }).ir;
  expect(statement(ir, demo.receive)).toMatchObject({ bindings: [] });
  expect(ir.symbols.map(s => s.id)).toEqual([demo.channel]);
});

test('semantic editing keeps large integer literals as exact decimal strings', () => {
  const demo = buildDemo();
  for (const value of ['9007199254740993', '9223372036854775807', '-9223372036854775808']) {
    const ir = runCommand(demo.ir, { type: 'UpdateLiteral', statementId: demo.send, value }).ir;
    expect(statement(ir, demo.send)).toMatchObject({ value: { kind: 'int_literal', value } });
    expect(statementLabel(ir, statement(ir, demo.send))).toContain(value);
    expect(JSON.parse(JSON.stringify(ir))).toEqual(ir);
  }
});

test('changing a local literal also changes the declaration type', () => {
  const ir = emptyIR();
  const added = runCommand(ir, { type: 'AddStatement', functionId: ir.entryFunctionId, index: 0, kind: 'let' });
  const local = statement(added.ir, added.selected!) as Extract<StatementIR, { kind: 'let' }>;
  const updated = runCommand(added.ir, { type: 'UpdateLiteral', statementId: local.id, value: false }).ir;
  expect(statement(updated, local.id)).toMatchObject({ value: { kind: 'bool_literal', value: false } });
  expect(updated.symbols.find(s => s.id === local.symbolId)!.type).toBe('bool');
  const restored = runCommand(updated, { type: 'UpdateLiteral', statementId: local.id, value: '42' }).ir;
  expect(restored.symbols.find(s => s.id === local.symbolId)!.type).toBe('int');
});

test('source selections convert Chinese and emoji between UTF-8 bytes and UTF-16', () => {
  const source = 'package main\n// 中文 😀 注释\nfunc main() { 通道 := make(chan int); 通道 <- 42 }';
  const encoder = new TextEncoder();
  let utf16 = 0;
  for (const char of source) {
    const byte = encoder.encode(source.slice(0, utf16)).length;
    expect(utf16ToUtf8(source, utf16)).toBe(byte);
    expect(utf8ToUtf16(source, byte)).toBe(utf16);
    for (let interior = 1; interior < encoder.encode(char).length; interior++) {
      expect(utf8ToUtf16(source, byte + interior)).toBe(utf16);
    }
    utf16 += char.length;
  }
  expect(utf8ToUtf16(source, encoder.encode(source).length)).toBe(source.length);
  const target = source.indexOf('通道 <-');
  const startByte = utf16ToUtf8(source, target);
  const endByte = utf16ToUtf8(source, target + '通道 <- 42'.length);
  expect(source.slice(utf8ToUtf16(source, startByte), utf8ToUtf16(source, endByte))).toBe('通道 <- 42');
});

test('layout undo and redo preserve source, program semantics, and revision', () => {
  const original = initialWorkspace();
  const layout = structuredClone(original.present.layout);
  layout.nodes[original.present.project.ir.entryFunctionId] = { x: 523, y: 317 };
  layout.viewport = { x: -80, y: 55, zoom: 1.25 };
  const moved = reducer(original, { type: 'layout', layout });
  expect(moved.present.draft).toBeNull();
  expect(moved.present.project).toEqual(original.present.project);
  expect(currentIR(moved)).toEqual(currentIR(original));
  const undone = reducer(moved, { type: 'undo' });
  expect(undone.present.layout).toEqual(original.present.layout);
  expect(undone.present.project.programRevision).toBe(original.present.project.programRevision);
  const redone = reducer(undone, { type: 'redo' });
  expect(redone.present.layout).toEqual(layout);
  expect(redone.present.project.source).toBe(original.present.project.source);
  expect(redone.present.project.programRevision).toBe(original.present.project.programRevision);
});

test('source and graph drafts are exclusive and both support undo and redo', () => {
  const original = initialWorkspace();
  const command: Command = { type: 'AddStatement', functionId: currentIR(original).entryFunctionId, index: 0, kind: 'return' };
  const source = `${original.present.project.source}\n// source draft\n`;
  const sourceDraft = reducer(original, { type: 'source', source });
  expect(sourceDraft.present.draft).toEqual({ kind: 'source', source });
  const graphBlocked = reducer(sourceDraft, { type: 'graph', command });
  expect(graphBlocked.error).toMatch(/代码草稿/);
  expect(graphBlocked.present).toBe(sourceDraft.present);
  expect(graphBlocked.epoch).toBe(sourceDraft.epoch);
  const graph = reducer(reducer(sourceDraft, { type: 'discard' }), { type: 'graph', command });
  expect(graph.present.draft?.kind).toBe('graph');
  expect(currentIR(graph).functions[0].body.statements.map(s => s.kind)).toEqual(['return']);
  expect(graph.present.project.ir).toEqual(original.present.project.ir);
  expect(graph.present.project.source).toBe(original.present.project.source);
  const sourceBlocked = reducer(graph, { type: 'source', source });
  expect(sourceBlocked.error).toMatch(/图形草稿/);
  expect(sourceBlocked.present).toBe(graph.present);
  const undone = reducer(graph, { type: 'undo' });
  expect(undone.present.draft).toBeNull();
  const redone = reducer(undone, { type: 'redo' });
  expect(redone.present.draft).toEqual(graph.present.draft);
  expect(currentIR(redone)).toEqual(currentIR(graph));
  const newBranch = reducer(undone, { type: 'source', source });
  expect(newBranch.future).toEqual([]);
  const sourceUndone = reducer(newBranch, { type: 'undo' });
  expect(sourceUndone.present.draft).toBeNull();
  expect(reducer(sourceUndone, { type: 'redo' }).present.draft).toEqual({ kind: 'source', source });
});
