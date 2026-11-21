import { beforeEach,describe,expect,test,vi } from 'vitest';

// apiMocks 保存库存请求使用的契约客户端替身。
const apiMocks = vi.hoisted(/* apiMockFactory 创建 GET 与 PUT 请求替身。 */ () => ({ get: vi.fn(), put: vi.fn() }));

// ContractMockResult 是测试契约请求回调返回的最小响应模型。
interface ContractMockResult {
  // data 是契约客户端解码后的成功数据。
  data?: unknown;
}

vi.mock('../../../shared/api-contract/client', /* contractClientMockFactory 提供库存 API 测试依赖。 */ () => ({
  // contractClientMock 暴露当前测试覆盖的 GET 与 PUT 方法。
  contractClient: { GET: apiMocks.get, PUT: apiMocks.put },
  // contractMultipartBodyMock 保留其他导出以满足商品 API 模块初始化。
  contractMultipartBody: <T>(/* form 是调用方提交的原生表单。 */ form: FormData) => form as unknown as T,
  // runContractRequestMock 执行 feature adapter 构造的请求并返回成功数据。
  runContractRequest: /* runContractRequestMock 执行商品 API 请求回调。 */ async (/* execute 是待运行的契约请求动作。 */ execute: (signal: AbortSignal) => Promise<ContractMockResult>) => (await execute(new AbortController().signal)).data,
}));

import { ApiError } from '../../../shared/http/client';
import { getItemStock,isItemStockChangedError,updateItemStock } from './api';

describe('商品库存 API 适配器', /* stockApiSuite 验证路径、DTO 映射与错误判定。 */ () => {
  beforeEach(/* apiMockReset 重置每个用例的请求替身。 */ () => {
    vi.clearAllMocks();
    apiMocks.get.mockResolvedValue({ data: { success: true, data: { item_id: 'item-1', multi_spec: true, quantity: 7, skus: [{ key: 'sku:a', label: '红色', price_cents: 100, quantity: 3 }, { key: 'sku:b', label: '蓝色', price_cents: 200, quantity: 4 }] } } });
    apiMocks.put.mockResolvedValue({ data: { success: true, data: { item_id: 'item-1', message: 'ok', cookie_persisted: true } } });
  });

  test('读取库存并把传输字段归一为 UI 模型', /* getStockCase 验证路径参数、取消信号与字段映射。 */ async () => {
    // stock 是归一后的库存。
    const stock = await getItemStock('account-1', 'item-1');
    expect(apiMocks.get).toHaveBeenCalledWith('/api/v1/items/{cookie_id}/{item_id}/stock', expect.objectContaining({
      params: { path: { cookie_id: 'account-1', item_id: 'item-1' } },
      signal: expect.any(AbortSignal),
    }));
    expect(stock).toEqual({ itemId: 'item-1', multiSpec: true, quantity: 7, skus: [
      { key: 'sku:a', label: '红色', priceCents: 100, quantity: 3 },
      { key: 'sku:b', label: '蓝色', priceCents: 200, quantity: 4 },
    ] });
  });

  test('多规格修改映射为下划线 DTO', /* putMultiCase 验证每行 expected_quantity 不丢失。 */ async () => {
    await updateItemStock('account-1', 'item-1', { skus: [{ key: 'sku:a', quantity: 6, expectedQuantity: 3 }] });
    expect(apiMocks.put).toHaveBeenCalledWith('/api/v1/items/{cookie_id}/{item_id}/stock', expect.objectContaining({
      params: { path: { cookie_id: 'account-1', item_id: 'item-1' } },
      body: { quantity: undefined, expected_quantity: undefined, skus: [{ key: 'sku:a', quantity: 6, expected_quantity: 3 }] },
      signal: expect.any(AbortSignal),
    }));
  });

  test('单规格修改携带数量与原库存', /* putSingleCase 验证单规格字段映射。 */ async () => {
    await updateItemStock('account-1', 'item-1', { quantity: 8, expectedQuantity: 3 });
    expect(apiMocks.put).toHaveBeenCalledWith('/api/v1/items/{cookie_id}/{item_id}/stock', expect.objectContaining({
      body: { quantity: 8, expected_quantity: 3, skus: undefined },
    }));
  });

  test('只把库存已变化错误码识别为需要重读', /* changedErrorCase 验证错误判定。 */ () => {
    expect(isItemStockChangedError(new ApiError(409, { code: 'item_stock_changed', message: '库存已变化' }))).toBe(true);
    expect(isItemStockChangedError(new ApiError(409, { code: 'conflict', message: '其他冲突' }))).toBe(false);
    expect(isItemStockChangedError(new Error('x'))).toBe(false);
  });
});
