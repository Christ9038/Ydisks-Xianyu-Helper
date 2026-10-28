import { describe, expect, test } from 'vitest';
import {
  formatRuntimeLogTime,
  mergeRuntimeLogEntries,
  normalizeRuntimeLogLevel,
  normalizeRuntimeLogSource,
  runtimeLogContextItems,
  runtimeLogHeadline,
  runtimeLogLevelBadgeClass,
  runtimeLogLevelLabel,
  runtimeLogLevelRowClass,
  runtimeLogQuerySignature,
  runtimeLogRetentionHint,
  runtimeLogSourceLabel,
} from './models';
import type { RuntimeLogEntry } from './types';

/** buildEntry 构造一条字段完整的运行日志，供展示辅助函数断言使用。 */
const buildEntry = (/* overrides 是需要覆盖的日志字段。 */ overrides: Partial<RuntimeLogEntry> = {}): RuntimeLogEntry => ({
  seq: 1,
  time: '2026-10-16T12:00:00Z',
  level: 'info',
  source: 'http',
  message: 'HTTP 请求完成',
  method: '',
  path: '',
  status: 0,
  durationMs: 0,
  requestId: '',
  api: '',
  category: '',
  ret: '',
  error: '',
  accountId: '',
  itemId: '',
  orderId: '',
  chatId: '',
  caller: '',
  func: '',
  route: '',
  runId: '',
  details: [],
  ...overrides,
});

describe('运行日志展示模型', /* 当前测试组验证等级来源归一化、时间格式化与日志合并规则。 */ () => {
  test('等级与来源归一化只接受契约枚举，未知值安全回退', /* 当前回调验证服务端新增枚举值不会破坏页面渲染。 */ () => {
    expect(normalizeRuntimeLogLevel('ERROR')).toBe('error');
    expect(normalizeRuntimeLogLevel(' debug ')).toBe('debug');
    expect(normalizeRuntimeLogLevel('verbose')).toBe('info');
    expect(normalizeRuntimeLogLevel('')).toBe('info');
    expect(normalizeRuntimeLogSource('MTOP')).toBe('mtop');
    expect(normalizeRuntimeLogSource('custom')).toBe('other');
    expect(normalizeRuntimeLogSource('')).toBe('other');
  });

  test('等级与来源徽标文本及配色覆盖全部取值', /* 当前回调验证徽标在四种等级下都有稳定文案与配色。 */ () => {
    expect(runtimeLogLevelLabel('error')).toBe('ERROR');
    expect(runtimeLogSourceLabel('renewal')).toBe('凭证续期');
    expect(runtimeLogLevelRowClass('error')).toContain('border-l-red-400');
    expect(runtimeLogLevelRowClass('warn')).toContain('border-l-amber-400');
    expect(runtimeLogLevelRowClass('info')).toContain('border-l-sky-300');
    expect(runtimeLogLevelRowClass('debug')).toContain('border-l-slate-200');
    expect(runtimeLogLevelBadgeClass('error')).toContain('bg-red-100');
    expect(runtimeLogLevelBadgeClass('debug')).toContain('bg-slate-100');
  });

  test('时间格式化只输出本地可读文本，无效输入返回空字符串', /* 当前回调验证时间展示不因非法值产生 NaN。 */ () => {
    expect(formatRuntimeLogTime('not-a-date')).toBe('');
    expect(formatRuntimeLogTime('2026-10-16T12:00:00Z')).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
  });

  test('日志行标题优先展示接口名，其次请求目标', /* 当前回调验证接口失败日志一眼能看出是哪个接口。 */ () => {
    expect(runtimeLogHeadline(buildEntry({ api: 'mtop.trade.order.render', path: '/ignored' }))).toBe('mtop.trade.order.render');
    expect(runtimeLogHeadline(buildEntry({ method: 'POST', path: '/api/v1/orders' }))).toBe('POST /api/v1/orders');
    expect(runtimeLogHeadline(buildEntry({ path: '/health' }))).toBe('/health');
    expect(runtimeLogHeadline(buildEntry({ message: '纯消息' }))).toBe('纯消息');
  });

  test('键值标签只输出有值的字段并保持固定顺序', /* 当前回调验证日志行不会出现空标签。 */ () => {
    expect(runtimeLogContextItems(buildEntry())).toEqual([]);
    // items 是包含全部可展示字段时的标签集合。
    const items = runtimeLogContextItems(buildEntry({
      method: 'POST', path: '/api/v1/orders', status: 500, durationMs: 120,
      category: 'system', ret: 'FAIL_SYS_TIMEOUT', accountId: 'acc-1', itemId: 'item-1',
      orderId: 'order-1', chatId: 'chat-1', requestId: 'req-1', route: '/api/v1/orders', runId: 'run-1',
    }));
    expect(items.map(/* item 是一个键值标签。 */ item => item.label)).toEqual(['请求', '路由', '状态', '耗时', '失败分类', '平台返回', '账号', '商品', '订单', '会话', '运行', '请求标识']);
    expect(items[3].value).toBe('120ms');
  });

  test('过滤条件签名随任一条件变化', /* 当前回调验证条件签名驱动日志流重置。 */ () => {
    expect(runtimeLogQuerySignature({ level: '', source: '', keyword: '' })).toBe('||');
    expect(runtimeLogQuerySignature({ level: 'error', source: 'mtop', keyword: ' order ' })).toBe('error|mtop|order');
  });

  test('增量合并去重并按容量裁剪最旧记录', /* 当前回调验证日志流不会重复也不会无限增长。 */ () => {
    // previous 是已展示的两条日志。
    const previous = [buildEntry({ seq: 1 }), buildEntry({ seq: 2 })];
    expect(mergeRuntimeLogEntries(previous, [], 10)).toBe(previous);
    expect(mergeRuntimeLogEntries(previous, [buildEntry({ seq: 2 })], 10)).toBe(previous);
    expect(mergeRuntimeLogEntries(previous, [buildEntry({ seq: 3 }), buildEntry({ seq: 4 })], 10).map(/* entry 是一条日志。 */ entry => entry.seq)).toEqual([1, 2, 3, 4]);
    expect(mergeRuntimeLogEntries(previous, [buildEntry({ seq: 3 })], 2).map(/* entry 是一条日志。 */ entry => entry.seq)).toEqual([2, 3]);
    expect(mergeRuntimeLogEntries(previous, [buildEntry({ seq: 3 })], 0).map(/* entry 是一条日志。 */ entry => entry.seq)).toEqual([1, 2, 3]);
  });

  test('保留提示只在缓冲缺失或记录被覆盖时出现', /* 当前回调验证容量与丢弃计数驱动用户提示。 */ () => {
    expect(runtimeLogRetentionHint(0, 0)).toContain('未启用运行日志缓冲');
    expect(runtimeLogRetentionHint(2000, 5)).toContain('2000');
    expect(runtimeLogRetentionHint(2000, 5)).toContain('5');
    expect(runtimeLogRetentionHint(2000, 0)).toBe('');
  });
});
