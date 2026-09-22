import { test, expect } from '@playwright/test';
import { defaultLayout, type ProgramIR } from '../src/model/types';

test('channel resource views use globally unique symbol IDs without reserved prefixes or prototype collisions', () => {
  const ir: ProgramIR = {
    schemaVersion: '1.0', id: 'constructor', packageName: 'main', entryFunctionId: 'fn_main',
    functions: [{ id: 'fn_main', kind: 'main', parentFunctionId: null, body: { id: 'block_main', statements: [
      { id: 'resource:__proto__', kind: 'make_channel', symbolId: '__proto__', capacity: 0 },
      { id: 'stmt_close', kind: 'close', channelSymbolId: '__proto__' },
    ] } }],
    symbols: [{ id: '__proto__', name: 'ch', type: 'chan int', scopeId: 'block_main' }],
  };
  const layout = defaultLayout(ir);
  expect(Object.hasOwn(layout.nodes, '__proto__')).toBe(true);
  expect(Object.hasOwn(layout.nodes, 'resource:__proto__')).toBe(true);
  expect(layout.nodes['__proto__']).not.toEqual(layout.nodes['resource:__proto__']);
  const reloaded = defaultLayout(ir, JSON.parse(JSON.stringify(layout)));
  expect(reloaded.nodes['__proto__']).toEqual(layout.nodes['__proto__']);
  expect(reloaded.nodes['resource:__proto__']).toEqual(layout.nodes['resource:__proto__']);
});
