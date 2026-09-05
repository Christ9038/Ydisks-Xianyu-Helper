import { expect, test } from 'vitest';
import type { OrderRefreshResponse } from './models';
import { formatOrderSyncResult } from './syncResult';

/** successResult 提供零计数的完整 UI 模型，保证成功提示与原文逐字兼容。 */
const successResult: OrderRefreshResponse = {
  partial_failure: false, message: '发现 2 单，同步完成',
  summary: { restored: 0, reassigned: 0, discovered: 2, list_updated: 0, soft_deleted: 0, detail_total: 0, total: 0, updated: 0, no_change: 0, failed: 0 }, results: [],
};

test('普通成功保持原消息与空消息语义，恢复修正追加独立统计', /* 验证恢复计数展示不重写既有成功语义。 */ () => {
  expect(formatOrderSyncResult(successResult)).toBe(successResult.message);
  expect(formatOrderSyncResult({ ...successResult, message: '' })).toBe('');
  expect(formatOrderSyncResult({ ...successResult, summary: { ...successResult.summary, restored: 3, reassigned: 2 } })).toBe(`${successResult.message}\n恢复 3 单，修正 2 单`);
});

test('部分失败标记单独生效，逐项说明保留 error 和 message 且不混入成功项', /* 验证 failed 为零时仍按 partial_failure 判定未完成。 */ () => {
  // message 是仅依赖批次标记及失败明细生成的提示。
  const message = formatOrderSyncResult({ ...successResult, partial_failure: true, results: [
    { success: false, cookie_id: 'account-a', order_id: 'order-a', error: '归属错误', message: '核验卖家' },
    { success: false, message: '列表不可用' },
    { success: false, cookie_id: 'account-b', error: '会话失效', message: '会话失效' },
    { success: false },
    { success: true, message: '成功项不应展示' },
  ] });
  expect(message).toContain('未完成');
  expect(message).toContain('账号 account-a');
  expect(message).toContain('订单 order-a');
  expect(message).toContain('归属错误');
  expect(message).toContain('核验卖家');
  expect(message).toContain('列表不可用');
  expect(message).toContain('未提供失败原因');
  expect(message.match(/会话失效/g)).toHaveLength(1);
  expect(message).not.toContain(successResult.message);
  expect(message).not.toContain('成功项不应展示');
});
