import { beforeEach,describe,expect,test,vi } from 'vitest';

// apiMocks 保存商品级 AI 请求使用的契约客户端替身。
const apiMocks = vi.hoisted(/* apiMockFactory 创建 GET 与 PUT 请求替身。 */ () => ({ get: vi.fn(), put: vi.fn() }));

// ContractMockResult 是测试契约请求回调返回的最小响应模型。
interface ContractMockResult {
  // data 是契约客户端解码后的成功数据。
  data?: unknown;
}

vi.mock('../../../shared/api-contract/client', /* contractClientMockFactory 提供商品级 AI API 测试依赖。 */ () => ({
  // contractClientMock 暴露当前测试覆盖的 GET 与 PUT 方法。
  contractClient: { GET: apiMocks.get, PUT: apiMocks.put },
  // contractMultipartBodyMock 保留其他导出以满足商品 API 模块初始化。
  contractMultipartBody: <T>(/* form 是调用方提交的原生表单。 */ form: FormData) => form as unknown as T,
  // runContractRequestMock 执行 feature adapter 构造的请求并返回成功数据。
  runContractRequest: /* runContractRequestMock 执行商品 API 请求回调。 */ async (/* execute 是待运行的契约请求动作。 */ execute: (signal: AbortSignal) => Promise<ContractMockResult>) => (await execute(new AbortController().signal)).data,
}));

import { getItemAISettings,updateItemAISettings } from './api';

describe('商品级 AI API 适配器', /* itemAIAPIAdapterSuite 验证路径、DTO 与取消信号映射。 */ () => {
  beforeEach(/* apiMockReset 重置每个用例的请求替身。 */ () => {
    vi.clearAllMocks();
    apiMocks.get.mockResolvedValue({ data: { ai_override: 'inherit', item_context: '' } });
    apiMocks.put.mockResolvedValue({ data: { ai_override: 'enabled', item_context: '全新未拆封' } });
  });

  test('打开弹窗时读取指定商品配置', /* getSettingsCase 验证 GET 路径参数与取消信号。 */ async () => {
    await getItemAISettings('account-1', 'item-1');
    expect(apiMocks.get).toHaveBeenCalledWith('/api/v1/items/{cookie_id}/{item_id}/ai-settings', expect.objectContaining({
      params: { path: { cookie_id: 'account-1', item_id: 'item-1' } },
      signal: expect.any(AbortSignal),
    }));
  });

  test('保存完整三态和商品资料 DTO', /* putSettingsCase 验证 PUT 请求不会丢失配置字段。 */ async () => {
    // settings 是用户确认提交的商品级 AI 配置。
    const settings = { ai_override: 'enabled' as const, item_context: '全新未拆封' };
    await updateItemAISettings('account-1', 'item-1', settings);
    expect(apiMocks.put).toHaveBeenCalledWith('/api/v1/items/{cookie_id}/{item_id}/ai-settings', expect.objectContaining({
      params: { path: { cookie_id: 'account-1', item_id: 'item-1' } },
      body: settings,
      signal: expect.any(AbortSignal),
    }));
  });
});
