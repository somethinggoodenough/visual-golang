import { declaredSymbols, findStatement, uid, visibleSymbols, type ExprIR, type ProgramIR, type StatementIR, type StatementKind, type SymbolIR } from './types';

export type Command =
  | { type: 'AddStatement'; functionId: string; index: number; kind: StatementKind }
  | { type: 'UpdateExpression'; statementId: string; value: ExprIR }
  | { type: 'UpdateLiteral'; statementId: string; value: string | boolean }
  | { type: 'UpdateChannelCapacity'; statementId: string; capacity: number }
  | { type: 'BindChannel'; statementId: string; symbolId: string }
  | { type: 'RenameSymbol'; symbolId: string; name: string }
  | { type: 'UpdateReceiveBindings'; statementId: string; names: (string | null)[] }
  | { type: 'ReorderStatement'; statementId: string; index: number }
  | { type: 'DeleteStatement'; statementId: string }
  | { type: 'DeleteGoroutine'; functionId: string };

function uniqueName(ir: ProgramIR, scopeId: string, base: string) {
  const names = new Set(ir.symbols.filter(s => s.scopeId === scopeId).map(s => s.name));
  let name = base, n = 2;
  while (names.has(name)) name = `${base}${n++}`;
  return name;
}

export function runCommand(original: ProgramIR, cmd: Command): { ir: ProgramIR; selected?: string } {
  const ir = structuredClone(original);
  const addSymbol = (scopeId: string, base: string, type: SymbolIR['type']) => {
    const sym: SymbolIR = { id: uid('sym'), name: uniqueName(ir, scopeId, base), scopeId, type };
    ir.symbols.push(sym); return sym.id;
  };
  if (cmd.type === 'AddStatement') {
    const fn = ir.functions.find(f => f.id === cmd.functionId);
    if (!fn) throw new Error('请选择操作所在的函数。');
    const id = uid('stmt'), index = Math.min(cmd.index, fn.body.statements.length);
    const symbols = visibleSymbols(ir, fn.id, index);
    const channel = symbols.find(s => s.type === 'chan int')?.id ?? '';
    const literal: ExprIR = { id: uid('expr'), kind: 'int_literal', value: '42' };
    let s: StatementIR;
    switch (cmd.kind) {
      case 'make_channel': s = { id, kind: cmd.kind, symbolId: addSymbol(fn.body.id, 'ch', 'chan int'), capacity: 0 }; break;
      case 'let': s = { id, kind: cmd.kind, symbolId: addSymbol(fn.body.id, 'value', 'int'), value: literal }; break;
      case 'spawn': {
        const functionId = uid('fn');
        ir.functions.push({ id: functionId, kind: 'goroutine', parentFunctionId: fn.id, body: { id: uid('block'), statements: [] } });
        s = { id, kind: cmd.kind, functionId }; break;
      }
      case 'send': s = { id, kind: cmd.kind, channelSymbolId: channel, value: literal }; break;
      case 'receive': s = { id, kind: cmd.kind, channelSymbolId: channel, bindings: [addSymbol(fn.body.id, 'value', 'int')] }; break;
      case 'close': s = { id, kind: cmd.kind, channelSymbolId: channel }; break;
      case 'print': {
        const value = symbols.filter(s => s.type !== 'chan int').at(-1);
        s = { id, kind: cmd.kind, value: value ? { id: uid('expr'), kind: 'variable', symbolId: value.id } : literal }; break;
      }
      case 'return': s = { id, kind: cmd.kind }; break;
    }
    fn.body.statements.splice(index, 0, s);
    return { ir, selected: id };
  }
  if (cmd.type === 'RenameSymbol') {
    const sym = ir.symbols.find(s => s.id === cmd.symbolId);
    if (sym) sym.name = cmd.name;
    return { ir };
  }
  const statementId = cmd.type === 'DeleteGoroutine'
    ? ir.functions.flatMap(f => f.body.statements).find(s => s.kind === 'spawn' && s.functionId === cmd.functionId)?.id
    : cmd.statementId;
  const found = statementId && findStatement(ir, statementId);
  if (!found) throw new Error('该操作已不存在。');
  const { statement: s, fn, index } = found;
  switch (cmd.type) {
    case 'UpdateExpression':
      if ('value' in s) {
        s.value = cmd.value;
        if (s.kind === 'let') {
          const sym = ir.symbols.find(x => x.id === s.symbolId)!;
          sym.type = cmd.value.kind === 'bool_literal' ? 'bool' : cmd.value.kind === 'variable' ? ir.symbols.find(x => x.id === (cmd.value as Extract<ExprIR, { kind: 'variable' }>).symbolId)?.type ?? 'int' : 'int';
        }
      }
      break;
    case 'UpdateLiteral': if ('value' in s && s.value.kind !== 'variable') {
      s.value = typeof cmd.value === 'boolean' ? { id: s.value.id, kind: 'bool_literal', value: cmd.value } : { id: s.value.id, kind: 'int_literal', value: cmd.value };
      if (s.kind === 'let') { const symbol = ir.symbols.find(sym => sym.id === s.symbolId); if (symbol) symbol.type = typeof cmd.value === 'boolean' ? 'bool' : 'int'; }
    } break;
    case 'UpdateChannelCapacity': if (s.kind === 'make_channel') s.capacity = cmd.capacity; break;
    case 'BindChannel': {
      const visible = visibleSymbols(ir, fn.id, index).some(sym => sym.id === cmd.symbolId && sym.type === 'chan int');
      if (!visible) throw new Error('此 Channel 在该操作的词法位置不可见；请检查声明顺序与作用域。');
      if ('channelSymbolId' in s) s.channelSymbolId = cmd.symbolId;
      break;
    }
    case 'UpdateReceiveBindings':
      if (s.kind === 'receive') {
        const old = s.bindings;
        s.bindings = cmd.names.map((name, i) => {
          if (name === null) return null;
          const existing = old[i] && ir.symbols.find(sym => sym.id === old[i]);
          if (existing) { existing.name = name; return existing.id; }
          const id = addSymbol(fn.body.id, name, i === 0 ? 'int' : 'bool');
          ir.symbols.find(sym => sym.id === id)!.name = name;
          return id;
        });
        ir.symbols = ir.symbols.filter(sym => !old.includes(sym.id) || s.bindings.includes(sym.id));
      }
      break;
    case 'ReorderStatement': {
      fn.body.statements.splice(index, 1);
      fn.body.statements.splice(Math.max(0, Math.min(cmd.index, fn.body.statements.length)), 0, s);
      break;
    }
    case 'DeleteStatement': case 'DeleteGoroutine': {
      const removedFunctions = new Set<string>();
      const removedStatements = new Set<string>([s.id]);
      const removedSymbols = new Set<string>(declaredSymbols(s));
      const visit = (functionId: string) => {
        removedFunctions.add(functionId);
        const child = ir.functions.find(f => f.id === functionId);
        child?.body.statements.forEach(stmt => {
          removedStatements.add(stmt.id);
          declaredSymbols(stmt).forEach(id => removedSymbols.add(id));
          if (stmt.kind === 'spawn') visit(stmt.functionId);
        });
      };
      if (s.kind === 'spawn') visit(s.functionId);
      const refs = ir.functions.flatMap(f => f.body.statements).filter(stmt => !removedStatements.has(stmt.id) && (
        ('channelSymbolId' in stmt && removedSymbols.has(stmt.channelSymbolId)) ||
        ('value' in stmt && stmt.value.kind === 'variable' && removedSymbols.has(stmt.value.symbolId))
      ));
      if (refs.length) throw new Error(`无法删除：仍有 ${refs.length} 个操作引用该声明（${refs.map(r => r.id).join('、')}）。请先删除或重新绑定引用。`);
      fn.body.statements.splice(index, 1);
      ir.functions = ir.functions.filter(f => !removedFunctions.has(f.id));
      ir.symbols = ir.symbols.filter(sym => !removedSymbols.has(sym.id));
      return { ir, selected: fn.id };
    }
  }
  return { ir };
}
