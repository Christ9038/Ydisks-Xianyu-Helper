import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { NotificationEventType } from './models';

// getMock、postMock 和 putMock 是飞书扫码绑定适配器使用的契约客户端方法替身。
const { getMock,postMock,putMock } = vi.hoisted(/* clientMocksFactory 创建契约客户端方法替身。 */ () => ({ getMock: vi.fn(), postMock: vi.fn(), putMock: vi.fn() }));

// ContractMockResult 是测试契约请求回调返回的最小响应模型。
interface ContractMockResult {
  // data 是契约客户端解码出的成功响应。
  data?: unknown;
}

vi.mock('../../../shared/api-contract/client', /* contractModuleFactory 提供飞书绑定 API 适配测试所需的契约依赖。 */ () => ({
  // contractClient 暴露本测试覆盖的 GET、POST 和 PUT 操作。
  contractClient: { GET: getMock, POST: postMock, PUT: putMock },
  // runContractRequestMock 执行适配器传入的请求回调并返回响应数据。
  runContractRequest: /* runContractRequestMock 执行适配器传入的契约请求回调。 */ async (/* execute 是适配器构造的契约请求动作。 */ execute: (signal: AbortSignal) => Promise<ContractMockResult>) => (await execute(new AbortController().signal)).data,
}));

import * as api from './api';

describe('飞书绑定与分类设置 API 适配器', /* 当前测试组验证请求路径、分类开关请求体和数值渠道参数。 */ () => {
  beforeEach(/* 当前回调重置契约客户端替身。 */ () => {
    vi.clearAllMocks();
  });

  test.each([
    { /** name 是渠道名称。 */ name: '  值班飞书 ', /** eventTypes 是订阅事件，含重复项。 */ eventTypes: ['chat_message_received', 'account_offline', 'account_offline'] as NotificationEventType[], /** enabled 是启用状态。 */ enabled: true, /** wantEventTypes 是期望提交的去重 JSON 文本。 */ wantEventTypes: '["chat_message_received","account_offline"]' },
    { /** name 是渠道名称。 */ name: '', /** eventTypes 为空表示全部事件。 */ eventTypes: [] as NotificationEventType[], /** enabled 是启用状态。 */ enabled: false, /** wantEventTypes 是期望提交的空文本。 */ wantEventTypes: '' },
  ])('开始绑定提交与链接添加一致的渠道表单字段', /* startOptions 是本次请求的表单与账号绑定选项。 */ async ({ wantEventTypes, ...startOptions }) => {
    // startResult 是后端创建的扫码会话响应。
    const startResult = { session_id: 'session-1', qr_code_url: 'data:image/png;base64,abc', expires_in: 300 };
    postMock.mockResolvedValue({ data: startResult });
    // result 是适配器返回的类型化扫码会话。
    const result = await api.startFeishuAppBinding(startOptions);
    expect(postMock).toHaveBeenCalledWith('/api/v1/notifications/feishu-app/qr', expect.objectContaining({
      body: {
        name: startOptions.name.trim(),
        event_types: wantEventTypes,
        enabled: startOptions.enabled,
      },
      signal: expect.any(AbortSignal),
    }));
    expect(result).toEqual(startResult);
  });

  test('状态查询把会话 ID 放入路径并返回类型化阶段', /* 当前回调验证状态轮询不会把会话 ID 混入查询参数。 */ async () => {
    // statusResult 是扫码等待阶段的响应。
    const statusResult = { status: 'waiting' as const, message: '等待扫码确认' };
    getMock.mockResolvedValue({ data: statusResult });
    // result 是适配器返回的类型化状态结果。
    const result = await api.getFeishuAppBindingStatus('session-9', { timeoutMs: 5_000 });
    expect(getMock).toHaveBeenCalledWith('/api/v1/notifications/feishu-app/qr/{session_id}', expect.objectContaining({ params: { path: { session_id: 'session-9' } }, signal: expect.any(AbortSignal) }));
    expect(result).toEqual(statusResult);
  });
});
