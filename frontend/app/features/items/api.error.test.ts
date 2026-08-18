import { describe, expect, test } from 'vitest';
import { ApiError } from '../../../shared/http/client';
import { itemErrorMessage } from './api';

// 商品 API 错误码适配测试覆盖权限、冲突、人工核对和可重试的恢复分支。
describe('items API error adapter', /* 商品错误适配测试组覆盖稳定机器码到恢复指引的映射。 */ () => {
  test('按统一机器错误码输出可执行的恢复提示', /* 当前回调验证权限、冲突、人工核对和重试提示。 */ () => {
    expect(itemErrorMessage(new ApiError(403, { code: 'stock_permission_missing', message: 'forbidden' }), 'fallback')).toContain('库存发布权限');
    expect(itemErrorMessage(new ApiError(409, { code: 'conflict', message: 'conflict' }), 'fallback')).toContain('刷新后重试');
    expect(itemErrorMessage(new ApiError(502, { code: 'external_result_unknown', message: 'unknown' }), 'fallback')).toContain('人工核对');
    expect(itemErrorMessage(new ApiError(503, { code: 'retryable', message: 'retry' }), 'fallback')).toContain('稍后重试');
  });
});
