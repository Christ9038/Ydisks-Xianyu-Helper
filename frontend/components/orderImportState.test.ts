import { expect, test } from 'vitest';
import { failedOrderImportRows, normalizeOrderImportResult } from './orderImportState';

test('order import state preserves row-level failure details for display', () => {
  const result = normalizeOrderImportResult({
    total: 2,
    success_count: 1,
    failed_count: 1,
    results: [
      { order_id: 'ok', success: true, message: '订单已导入' },
      { order_id: 'bad', success: false, message: '不支持的订单状态' },
    ],
  });
  expect(result.failed_count).toBe(1);
  expect(failedOrderImportRows(result)).toEqual([
    { order_id: 'bad', success: false, message: '不支持的订单状态' },
  ]);
});
