import { beforeEach,describe,expect,test,vi } from 'vitest';

// getMock、postMock 和 putMock 是飞书绑定与分类设置适配器使用的契约客户端方法替身。
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
    { /** bindAllAccounts 是账号绑定选项。 */ bindAllAccounts: true, /** notifyWarning 是警告类通知选项。 */ notifyWarning: true, /** notifyInfo 是普通提示类通知选项。 */ notifyInfo: true },
    { /** bindAllAccounts 是账号绑定选项。 */ bindAllAccounts: false, /** notifyWarning 是警告类通知选项。 */ notifyWarning: false, /** notifyInfo 是普通提示类通知选项。 */ notifyInfo: false },
  ])('开始绑定提交具名账号与分类开关', /* startOptions 是本次请求的账号与分类选项。 */ async startOptions => {
    // startResult 是后端创建的扫码会话响应。
    const startResult = { session_id: 'session-1', qr_code_url: 'data:image/png;base64,abc', expires_in: 300 };
    postMock.mockResolvedValue({ data: startResult });
    // result 是适配器返回的类型化扫码会话。
    const result = await api.startFeishuAppBinding(startOptions);
    expect(postMock).toHaveBeenCalledWith('/api/v1/notifications/feishu-app/qr', expect.objectContaining({
      body: {
        bind_all_accounts: startOptions.bindAllAccounts,
        notify_warning: startOptions.notifyWarning,
        notify_info: startOptions.notifyInfo,
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

  test('读取分类设置把字符串渠道标识转换为数值路径参数', /* 当前回调验证 UI 渠道标识与契约整数主键的边界转换。 */ async () => {
    // categories 是服务端保存的警告类与普通提示类开关。
    const categories = { warning: false, info: true };
    getMock.mockResolvedValue({ data: categories });
    // result 是适配器返回的类型化分类设置。
    const result = await api.getFeishuChannelCategories('12');
    expect(getMock).toHaveBeenCalledWith('/api/v1/notifications/channels/{channel_id}/feishu-categories', expect.objectContaining({ params: { path: { channel_id: 12 } }, signal: expect.any(AbortSignal) }));
    expect(result).toEqual(categories);
  });

  test('保存分类设置提交布尔开关并返回操作结果', /* 当前回调验证 PUT 请求体和路径参数。 */ async () => {
    putMock.mockResolvedValue({ data: { success: true } });
    // result 是适配器返回的统一操作结果。
    const result = await api.updateFeishuChannelCategories('7', { warning: true, info: false });
    expect(putMock).toHaveBeenCalledWith('/api/v1/notifications/channels/{channel_id}/feishu-categories', expect.objectContaining({ params: { path: { channel_id: 7 } }, body: { warning: true, info: false }, signal: expect.any(AbortSignal) }));
    expect(result).toEqual({ success: true });
  });
});
