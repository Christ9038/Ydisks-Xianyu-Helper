import { expect, test } from 'vitest';
import {
  canSubmitOrderImport,
  failedOrderImportRows,
  isCurrentOrderRequest,
  normalizeOrderImportResult,
  orderStatusOptions,
  validateOrderImportFile,
} from './state';

// createFile 创建订单导入预检使用的最小浏览器文件对象。
const createFile = (name: string): File => new File(['order'], name, { type: 'text/plain' });

test('订单导入响应归一化并保留失败行详情',
  // 导入结果测试验证汇总字段和失败行展示数据。
  () => {
  // result 是包含成功和失败行的后端兼容响应。
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

test('订单导入预检阻止重复提交和不支持的文件格式',
  // 导入预检测试验证文件格式和重复提交门禁。
  () => {
  expect(canSubmitOrderImport(createFile('orders.csv'), false)).toBe(true);
  expect(canSubmitOrderImport(createFile('orders.csv'), true)).toBe(false);
  expect(validateOrderImportFile(createFile('orders.xlsx'))).toBe('');
  expect(validateOrderImportFile(createFile('orders.exe'))).toContain('仅支持');
  expect(validateOrderImportFile(null)).toBe('请选择订单文件');
  });

test('订单请求代次和筛选标签保持稳定边界',
  // 查询边界测试验证旧响应隔离和筛选项顺序。
  () => {
  expect(isCurrentOrderRequest(4, 4)).toBe(true);
  expect(isCurrentOrderRequest(3, 4)).toBe(false);
  expect(orderStatusOptions.map(
    // option 是待验证顺序的筛选配置。
    option => option.key,
  )).toEqual([
    'all', 'processing', 'shipped', 'pending_ship', 'completed', 'cancelled', 'refunding',
  ]);
  });
