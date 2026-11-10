import { beforeEach, describe, expect, test, vi } from 'vitest';

// getMock、putMock、postMock 是 Agent 回复契约请求的共享客户端替身。
const { getMock, putMock, postMock } = vi.hoisted(/* clientMockFactory 创建契约客户端方法替身。 */ () => ({ getMock: vi.fn(), putMock: vi.fn(), postMock: vi.fn() }));

// ContractMockResult 是测试契约请求回调返回的最小响应模型。
interface ContractMockResult {
  // data 是契约客户端解码出的成功响应。
  data?: unknown;
}

vi.mock('../../../shared/api-contract/client', /* contractClientModuleMockFactory 提供 Agent 回复 API 适配测试所需的契约依赖。 */ () => ({
  // contractClient 只提供本测试覆盖的 GET、PUT 与 POST 操作。
  contractClient: { GET: getMock, PUT: putMock, POST: postMock },
  // runContractRequest 执行 feature adapter 传入的请求回调。
  runContractRequest: /* runContractRequestMock 执行 feature adapter 传入的契约请求回调。 */ async (/* execute 是 feature adapter 构造的契约请求动作。 */ execute: (signal: AbortSignal) => Promise<ContractMockResult>) => (await execute(new AbortController().signal)).data,
}));

import { getAgentReplySettings, saveAgentReplySettings, testAgentReplyConnection } from './api';
import type { AgentReplyPayload } from './api';

// payload 是保存与测试共用的表单载荷基线。
const payload: AgentReplyPayload = { enabled: true, endpoint_url: 'https://agent.example.com/v1', request_timeout_seconds: 10, max_session_seconds: 60, token_action: 'retain', token_value: 'leftover' };

describe('agentReply API adapter', /* 当前回调验证 Agent 回复适配器的路径、请求体映射与响应归一化。 */ () => {
  beforeEach(/* 当前回调重置全部契约替身。 */ () => {
    getMock.mockReset();
    putMock.mockReset();
    postMock.mockReset();
  });

  test('读取配置使用账号路径并归一缺失字段', /* 当前回调执行该行为用例。 */ async () => {
    getMock.mockResolvedValueOnce({ data: { cookie_id: 'a1', enabled: true, endpoint_url: 'https://a.example.com', token_configured: true, request_timeout_seconds: 8, max_session_seconds: 90 } });
    await expect(getAgentReplySettings('a1')).resolves.toEqual({ cookie_id: 'a1', enabled: true, endpoint_url: 'https://a.example.com', token_configured: true, request_timeout_seconds: 8, max_session_seconds: 90 });
    expect(getMock.mock.calls[0][0]).toBe('/api/v1/agent-replies/{cookie_id}');
    expect(getMock.mock.calls[0][1].params.path).toEqual({ cookie_id: 'a1' });
    // 缺失或类型错误的字段回退到关闭状态与默认超时，账号标识回退到请求账号。
    getMock.mockResolvedValueOnce({ data: { enabled: 'yes', endpoint_url: 3 } });
    await expect(getAgentReplySettings('a2')).resolves.toEqual({ cookie_id: 'a2', enabled: false, endpoint_url: '', token_configured: false, request_timeout_seconds: 15, max_session_seconds: 120 });
    getMock.mockResolvedValueOnce({ data: null });
    await expect(getAgentReplySettings('a3')).resolves.toMatchObject({ cookie_id: 'a3', enabled: false });
  });

  test('保存只在替换命令下携带令牌明文', /* 当前回调执行该行为用例。 */ async () => {
    putMock.mockResolvedValue({ data: { cookie_id: 'a1', enabled: true, endpoint_url: 'https://agent.example.com/v1', token_configured: true, request_timeout_seconds: 10, max_session_seconds: 60 } });
    await saveAgentReplySettings('a1', payload);
    expect(putMock.mock.calls[0][0]).toBe('/api/v1/agent-replies/{cookie_id}');
    expect(putMock.mock.calls[0][1].body).toEqual({ enabled: true, endpoint_url: 'https://agent.example.com/v1', request_timeout_seconds: 10, max_session_seconds: 60, token: { action: 'retain', value: '' } });
    await saveAgentReplySettings('a1', { ...payload, token_action: 'replace', token_value: 'new-token' });
    expect(putMock.mock.calls[1][1].body.token).toEqual({ action: 'replace', value: 'new-token' });
    // 清除命令不携带遗留明文。
    await saveAgentReplySettings('a1', { ...payload, token_action: 'clear' });
    expect(putMock.mock.calls[2][1].body.token).toEqual({ action: 'clear', value: '' });
  });

  test('连接测试映射请求体并校验诊断状态', /* 当前回调执行该行为用例。 */ async () => {
    postMock.mockResolvedValueOnce({ data: { status: 'ok', status_code: 200, duration_ms: 12, handled: true, working: false, reply_count: 2 } });
    await expect(testAgentReplyConnection('a1', { ...payload, token_action: 'replace', token_value: 't' })).resolves.toEqual({ status: 'ok', status_code: 200, duration_ms: 12, handled: true, working: false, reply_count: 2 });
    expect(postMock.mock.calls[0][0]).toBe('/api/v1/agent-replies/{cookie_id}/test');
    expect(postMock.mock.calls[0][1].body).toEqual({ endpoint_url: 'https://agent.example.com/v1', request_timeout_seconds: 10, token: { action: 'replace', value: 't' } });
    // 未知状态与缺失字段按不可达归一，数值字段回退为 0。
    postMock.mockResolvedValueOnce({ data: { status: 'weird' } });
    await expect(testAgentReplyConnection('a1', payload)).resolves.toEqual({ status: 'unreachable', status_code: 0, duration_ms: 0, handled: false, working: false, reply_count: 0 });
    postMock.mockResolvedValueOnce({ data: 'oops' });
    await expect(testAgentReplyConnection('a1', payload)).resolves.toMatchObject({ status: 'unreachable' });
  });
});
