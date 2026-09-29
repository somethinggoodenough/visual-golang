import { ArrowDown, ArrowUp, GitFork, Trash2 } from 'lucide-react';
import { useLanguage } from '../i18n';
import type { Command } from '../model/commands';
import { findStatement, labels, statementLabel, symbolName, visibleSymbols, type ExprIR, type ProgramIR, type SymbolIR } from '../model/types';

interface Props { ir: ProgramIR; selected: string | null; execute: (command: Command) => void; select: (id: string) => void; locked: boolean; maxCapacity: number }
export function Inspector({ ir, selected, execute, select, locked, maxCapacity }: Props) {
  const { language, t } = useLanguage();
  const codeLabel = (text: string) => text.replaceAll('未绑定', t('未绑定'));
  const resource = ir.symbols.find(sym => sym.id === selected && sym.type === 'chan int')?.id ?? null;
  const declaration = resource ? ir.functions.flatMap(f => f.body.statements).find(s => s.kind === 'make_channel' && s.symbolId === resource) : null;
  const found = selected ? findStatement(ir, declaration?.id ?? selected) : null;
  const fn = ir.functions.find(f => f.id === selected);
  if (fn) return <div className="inspector"><div className="section-eyebrow">{t('执行区域')}</div><h3>{fn.kind === 'main' ? 'main' : 'Goroutine'}</h3><p className="help">{t(fn.kind === 'main' ? '程序入口。main 返回时，Go 程序结束。' : '通过 go 语句创建的匿名函数。可捕获外层 Channel。')}</p><div className="property-row"><span>{t('操作数')}</span><code>{fn.body.statements.length}</code></div><div className="property-label">{t('执行顺序')}</div><div className="outline">{fn.body.statements.map((s, i) => <button key={s.id} onClick={() => select(s.id)}><span>{String(i + 1).padStart(2, '0')}</span><code>{codeLabel(statementLabel(ir, s))}</code></button>)}</div>{fn.kind !== 'main' && <button className="danger-button" disabled={locked} onClick={() => execute({ type: 'DeleteGoroutine', functionId: fn.id })}><Trash2 size={14}/>{t('删除 Goroutine 及其操作')}</button>}</div>;
  if (!found) return <div className="inspector empty-inspector"><span className="selection-illustration"><GitFork size={32}/></span><h3>{t('让并发结构清晰可见')}</h3><p>{t('选择一个操作，编辑其值与绑定。')}<br/>{t('选择执行区域，查看语句顺序。')}</p><div className="inspector-tip">{t('Channel 连线表示资源引用，')}<br/>{t('不代表消息的确定配对或执行先后。')}</div></div>;
  const { statement: s, fn: owner, index } = found;
  const symbols = visibleSymbols(ir, owner.id, index);
  const channels = symbols.filter(sym => sym.type === 'chan int');
  const scalarSymbols = symbols.filter(sym => sym.type !== 'chan int');
  const boundSymbol = 'symbolId' in s ? ir.symbols.find(sym => sym.id === s.symbolId) : undefined;
  const rename = (symbol: SymbolIR) => <label className="field" key={symbol.id}><span>{t('变量名称')} <small>{symbol.type}</small></span><input aria-label={`${t('变量名称')} ${symbol.type}`} value={symbol.name} onChange={e => execute({ type: 'RenameSymbol', symbolId: symbol.id, name: e.target.value })} spellCheck={false}/></label>;
  const expression = (value: ExprIR) => <>
    <label className="field"><span>{t('表达式类型')}</span><select aria-label={t('表达式类型')} value={value.kind} onChange={e => {
      const kind = e.target.value as ExprIR['kind'];
      execute({ type: 'UpdateExpression', statementId: s.id, value: kind === 'variable' ? { id: value.id, kind, symbolId: scalarSymbols[0]?.id ?? '' } : kind === 'bool_literal' ? { id: value.id, kind, value: true } : { id: value.id, kind, value: '42' } });
    }}><option value="int_literal">{t('int · 整数字面量')}</option><option value="bool_literal">{t('bool · 布尔字面量')}</option><option value="variable">{t('引用局部变量')}</option></select></label>
    {value.kind === 'int_literal' && <label className="field"><span>{t(s.kind === 'send' ? '发送值' : '整数值')} <small>int</small></span><input aria-label={t(s.kind === 'send' ? '发送值' : '整数值')} className="literal-input" value={value.value} onChange={e => execute({ type: 'UpdateLiteral', statementId: s.id, value: e.target.value })} inputMode="numeric" spellCheck={false}/><small className="field-hint">{t('以十进制字符串保存，保留整数精度。')}</small></label>}
    {value.kind === 'bool_literal' && <label className="field"><span>{t('布尔值')}</span><select aria-label={t('布尔值')} value={String(value.value)} onChange={e => execute({ type: 'UpdateLiteral', statementId: s.id, value: e.target.value === 'true' })}><option value="true">true</option><option value="false">false</option></select></label>}
    {value.kind === 'variable' && <label className="field"><span>{t('引用变量')}</span><select aria-label={t('引用变量')} value={value.symbolId} onChange={e => execute({ type: 'UpdateExpression', statementId: s.id, value: { ...value, symbolId: e.target.value } })}><option value="" disabled>{t('请选择变量')}</option>{!scalarSymbols.some(sym => sym.id === value.symbolId) && value.symbolId && <option value={value.symbolId}>{t('不可见 ·')} {t(symbolName(ir, value.symbolId))}</option>}{scalarSymbols.map(sym => <option key={sym.id} value={sym.id}>{sym.name} · {sym.type}</option>)}</select></label>}
  </>;
  return <div className="inspector"><div className="section-eyebrow">{t(resource ? 'CHANNEL 资源 / 创建操作' : '操作属性')}</div><h3>{t(labels[s.kind])}</h3><div className="node-preview"><code>{codeLabel(statementLabel(ir, s))}</code></div>
    {locked && <p className="locked-note">{t('代码草稿编辑中。先应用或放弃草稿即可修改属性。')}</p>}
    <fieldset disabled={locked}>
    {boundSymbol && rename(boundSymbol)}
    {s.kind === 'make_channel' && <><label className="field"><span>{t('元素类型')}</span><input value="int" readOnly aria-label={t('Channel 元素类型')}/></label><label className="field"><span>{t('缓冲容量')}</span><input type="number" aria-label={t('缓冲容量')} min={0} max={maxCapacity} step={1} value={s.capacity} onChange={e => execute({ type: 'UpdateChannelCapacity', statementId: s.id, capacity: Number(e.target.value) })}/><small className="field-hint">{language === 'en' ? `0 is unbuffered; maximum capacity is ${maxCapacity}.` : `0 表示无缓冲；产品容量上限 ${maxCapacity}。`}</small></label>{resource && <button className="text-button" onClick={() => select(s.id)}>{t('↗ 定位 Channel 创建语句')}</button>}</>}
    {'channelSymbolId' in s && <label className="field"><span>{t('Channel 绑定')} <small>chan int</small></span><select aria-label={t('Channel 绑定')} value={s.channelSymbolId} onChange={e => execute({ type: 'BindChannel', statementId: s.id, symbolId: e.target.value })}><option value="" disabled>{t('连接或选择 Channel')}</option>{!channels.some(sym => sym.id === s.channelSymbolId) && s.channelSymbolId && <option value={s.channelSymbolId}>{t('不可见 ·')} {t(symbolName(ir, s.channelSymbolId))}</option>}{channels.map(sym => <option value={sym.id} key={sym.id}>{sym.name} · {t(sym.scopeId === owner.body.id ? '当前作用域' : '外层捕获')}</option>)}</select><small className="field-hint">{t('也可从资源节点的端口拖动连接。')}</small></label>}
    {'value' in s && expression(s.value)}
    {s.kind === 'receive' && <><label className="field"><span>{t('接收绑定')}</span><select aria-label={t('接收绑定')} value={s.bindings.length} onChange={e => {
      const count = Number(e.target.value);
      execute({ type: 'UpdateReceiveBindings', statementId: s.id, names: Array.from({ length: count }, (_, i) => s.bindings[i] ? symbolName(ir, s.bindings[i]) : i === 0 ? 'value' : 'ok') });
    }}><option value={0}>{t('仅接收 · 忽略结果')}</option><option value={1}>value := ← ch</option><option value={2}>value, ok := ← ch</option></select></label>{s.bindings.map((binding, i) => <label className="field" key={i}><span>{t(i === 0 ? '接收变量' : '接收状态变量')} <small>{i === 0 ? 'int' : 'bool'}</small></span><input aria-label={t(i === 0 ? '接收变量' : '接收状态变量')} value={binding ? symbolName(ir, binding) : '_'} onChange={e => execute({ type: 'UpdateReceiveBindings', statementId: s.id, names: s.bindings.map((id, j) => j === i ? (e.target.value === '_' ? null : e.target.value) : id ? symbolName(ir, id) : null) })}/></label>)}<small className="field-hint">{t('使用 _ 忽略一个结果；至少保留一个新变量。')}</small></>}
    {s.kind === 'spawn' && <button className="secondary-button full" onClick={() => select(s.functionId)}><GitFork size={14}/>{t('选择 Goroutine 函数体')}</button>}
    <div className="property-divider"/><div className="property-label">{t('语句顺序')} <span>{index + 1} / {owner.body.statements.length}</span></div><div className="order-controls"><button disabled={index === 0} onClick={() => execute({ type: 'ReorderStatement', statementId: s.id, index: index - 1 })}><ArrowUp size={14}/>{t('上移')}</button><button disabled={index === owner.body.statements.length - 1} onClick={() => execute({ type: 'ReorderStatement', statementId: s.id, index: index + 1 })}><ArrowDown size={14}/>{t('下移')}</button></div><button className="danger-button" onClick={() => execute({ type: 'DeleteStatement', statementId: s.id })}><Trash2 size={14}/>{t(s.kind === 'spawn' ? '删除操作与 Goroutine' : '删除操作')}</button>
    </fieldset><div className="node-id">{s.id}</div></div>;
}
