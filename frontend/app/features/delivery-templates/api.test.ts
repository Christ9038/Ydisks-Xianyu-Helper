import { beforeEach, describe, expect, test, vi } from 'vitest';

// 契约客户端替身：每个 HTTP 方法记录调用参数。
const { getMock, postMock, putMock, deleteMock } = vi.hoisted(/* clientMocksFactory 创建四个 HTTP 方法替身。 */ () => ({ getMock: vi.fn(), postMock: vi.fn(), putMock: vi.fn(), deleteMock: vi.fn() }));

vi.mock('../../../shared/api-contract/client', /* contractModuleFactory 提供模板 API 适配所需的契约依赖。 */ () => ({
  // contractClient 只暴露本适配器用到的方法。
  contractClient: { GET: getMock, POST: postMock, PUT: putMock, DELETE: deleteMock },
  // runContractRequest 执行适配器传入的请求回调并返回 data。
  runContractRequest: /* executeMock 直接执行请求回调。 */ async (/* execute 是适配器构造的请求动作。 */ execute: (signal: AbortSignal) => Promise<{ /** data 是成功响应体。 */ data?: unknown }>) => (await execute(new AbortController().signal)).data,
}));

import { createDeliveryTemplate, deleteDeliveryTemplate, listDeliveryTemplates, updateDeliveryTemplate } from './api';
import type { DeliveryTemplateDraft } from './types';

// draft 是创建与更新测试共用的最小模板草稿。
const draft = { name: '模板', enabled: true, messages: [] } as unknown as DeliveryTemplateDraft;
// payload 是适配器按当前契约归一后的请求体：名称去空白、显式提交启用状态，且不再发送客户端推导的变量键。
const payload = { name: '模板', enabled: true, messages: [] };

describe('发货模板 API 适配器', /* 模板 API 测试覆盖列表归一化与增删改请求映射。 */ () => {
  beforeEach(/* 当前回调重置所有契约客户端替身。 */ () => {
    vi.clearAllMocks();
    // mock 是逐个重置默认响应的写操作替身。
    for (const /* mock 是逐个重置默认响应的写操作替身。 */ mock of [postMock, putMock, deleteMock]) mock.mockResolvedValue({ data: {} });
  });

  test('列表剥离历史 delivery. 前缀并规范 custom_keys', /* 当前回调验证消息正文与自定义键的归一化。 */ async () => {
    getMock.mockResolvedValue({ data: { data: [
      { id: 1, messages: [{ content: '你好 {{delivery.card}}' }], custom_keys: null },
      { id: 2, messages: [], custom_keys: ['k'] },
    ] } });
    // result 是归一化后的模板列表。
    const result = await listDeliveryTemplates();
    expect(result[0].messages[0].content).toBe('你好 {{card}}');
    expect(result[0].custom_keys).toEqual([]);
    expect(result[1].custom_keys).toEqual(['k']);
  });

  test('响应缺少 data 时返回空列表', /* 当前回调验证空响应不会抛出异常。 */ async () => {
    getMock.mockResolvedValue({ data: {} });
    expect(await listDeliveryTemplates()).toEqual([]);
  });

  test('创建、更新、删除分别调用对应契约操作', /* 当前回调验证路径参数转为字符串并携带请求体。 */ async () => {
    await createDeliveryTemplate(draft);
    expect(postMock).toHaveBeenCalledWith('/api/v1/delivery-templates', expect.objectContaining({ body: payload }));
    await updateDeliveryTemplate(7, draft);
    expect(putMock).toHaveBeenCalledWith('/api/v1/delivery-templates/{template_id}', expect.objectContaining({ params: { path: { template_id: '7' } }, body: payload }));
    await deleteDeliveryTemplate(7);
    expect(deleteMock).toHaveBeenCalledWith('/api/v1/delivery-templates/{template_id}', expect.objectContaining({ params: { path: { template_id: '7' } } }));
  });
});
