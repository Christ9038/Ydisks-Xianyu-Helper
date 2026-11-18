import { beforeEach, describe, expect, test, vi } from 'vitest';

// 契约客户端各 HTTP 方法替身，用于记录令牌适配器构造的请求。
const { getMock, postMock, deleteMock } = vi.hoisted(/* clientMocksFactory 创建 HTTP 方法替身。 */ () => ({ getMock: vi.fn(), postMock: vi.fn(), deleteMock: vi.fn() }));

vi.mock('../../../shared/api-contract/client', /* contractModuleFactory 提供令牌 API 适配所需的契约依赖。 */ () => ({
  // contractClient 暴露令牌适配器用到的 HTTP 方法。
  contractClient: { GET: getMock, POST: postMock, DELETE: deleteMock },
  // runContractRequest 执行请求动作；响应带 error 时抛出，模拟统一契约运行时的失败语义。
  runContractRequest: /* executeMock 执行适配器传入的请求动作。 */ async (/* execute 是适配器构造的请求动作。 */ execute: (signal: AbortSignal) => Promise<{ /** data 是成功响应体。 */ data?: unknown; /** error 是失败响应体。 */ error?: unknown }>) => {
    // result 是请求动作返回的契约响应。
    const result = await execute(new AbortController().signal);
    if (result.error) throw new Error('contract-error');
    return result.data;
  },
}));

import { createAPIAccessToken, listAPIAccessTokens, revokeAPIAccessToken } from './api';

describe('API 访问令牌适配器', /* 令牌适配器测试覆盖请求映射与错误传播。 */ () => {
  beforeEach(/* 当前回调重置所有契约方法替身。 */ () => {
    vi.clearAllMocks();
  });

  test('列表读取 access-tokens 集合并带取消信号', /* 当前回调验证 GET 路径与响应透传。 */ async () => {
    getMock.mockResolvedValue({ data: { success: true, data: [] } });
    await expect(listAPIAccessTokens()).resolves.toEqual({ success: true, data: [] });
    expect(getMock).toHaveBeenCalledWith('/api/v1/access-tokens', { signal: expect.any(AbortSignal) });
  });

  test('签发只发送名称、权限与有效期，并返回一次性明文', /* 当前回调验证 POST 请求体与创建响应。 */ async () => {
    postMock.mockResolvedValue({ data: { success: true, token: 'ydk_live_fixture', data: { id: 'k1' } } });
    // input 是本次签发请求的用户输入。
    const input = { name: '客服 Agent', scopes: ['chat:read' as const], expires_in_days: 30 };
    // created 是创建响应，其中明文只在此返回一次。
    const created = await createAPIAccessToken(input);
    expect(created.token).toBe('ydk_live_fixture');
    expect(postMock).toHaveBeenCalledWith('/api/v1/access-tokens', { body: input, signal: expect.any(AbortSignal) });
  });

  test('撤销把令牌标识放入路径参数', /* 当前回调验证 DELETE 路径参数映射。 */ async () => {
    deleteMock.mockResolvedValue({ data: { success: true } });
    await expect(revokeAPIAccessToken('k1')).resolves.toEqual({ success: true });
    expect(deleteMock).toHaveBeenCalledWith('/api/v1/access-tokens/{token_id}', { params: { path: { token_id: 'k1' } }, signal: expect.any(AbortSignal) });
  });

  test('服务端失败会向调用方抛出而不是吞掉', /* 当前回调验证三个操作均传播契约错误。 */ async () => {
    // failure 是模拟的统一错误响应。
    const failure = { error: { code: 'internal_error' } };
    getMock.mockResolvedValue(failure);
    postMock.mockResolvedValue(failure);
    deleteMock.mockResolvedValue(failure);
    await expect(listAPIAccessTokens()).rejects.toThrow('contract-error');
    await expect(createAPIAccessToken({ name: 'x', scopes: ['chat:read'], expires_in_days: 0 })).rejects.toThrow('contract-error');
    await expect(revokeAPIAccessToken('k1')).rejects.toThrow('contract-error');
  });
});
