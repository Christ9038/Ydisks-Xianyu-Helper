import { beforeEach,describe,expect,test,vi } from 'vitest';

// getMock 是规则日志契约 GET 请求的共享客户端替身。
const { getMock } = vi.hoisted(/* getMockFactory 创建契约客户端 GET 方法替身。 */ () => ({ getMock: vi.fn() }));

// ContractMockResult 是测试契约请求回调返回的最小响应模型。
interface ContractMockResult {
  // data 是契约客户端解码出的成功响应。
  data?: unknown;
}

vi.mock('../../../shared/api-contract/client', /* contractClientModuleMockFactory 提供规则日志 API 适配测试所需的契约依赖。 */ () => ({
  // contractClient 只提供本测试覆盖的 GET 操作。
  contractClient: { GET: getMock },
  // runContractRequest 执行 feature adapter 传入的请求回调。
  runContractRequest: /* runContractRequestMock 执行 feature adapter 传入的契约请求回调。 */ async (/* execute 是 feature adapter 构造的契约请求动作。 */ execute: (signal: AbortSignal) => Promise<ContractMockResult>) => (await execute(new AbortController().signal)).data,
}));

import { getAuditOrderDeliveredCards,getAutomationAuditEvents } from './api';

// GetCallOptions 是测试中读取契约参数的最小参数模型。
interface GetCallOptions {
  // params 是契约客户端收到的路径与查询参数对象。
  params?: { /** query 是查询键值对。 */ query?: Record<string, unknown>; /** path 是路径参数。 */ path?: Record<string, unknown> };
}

describe('auditApi', /* 当前回调验证规则日志适配器的参数映射与响应归一化。 */ () => {
  beforeEach(/* 当前回调重置替身。 */ () => {
    getMock.mockReset();
  });

  // 用例：查询参数映射为 snake_case，空筛选项省略，响应时间换算为毫秒。
  test('查询参数映射为契约字段并归一化响应', async () => {
    getMock.mockResolvedValue({ data: { success: true, total: 1, page: 1, page_size: 20, total_pages: 0, data: [{ id: 1, cookie_id: 'c1', order_id: 'o1', item_id: 'i1', rule_id: 2, run_id: 3, trigger_type: 'order_paid', source: 'ws', event_type: 'run_finished', status: 'success', action_type: '', summary: 'S', reason: '', created_at: 10 }] } });
    // page 是归一化后的一页日志。
    const page = await getAutomationAuditEvents({ cookieId: '', status: '', triggerType: '', search: '  ', runId: 0, page: 2, pageSize: 20 });
    // options 是第一次 GET 调用收到的契约参数。
    const options = getMock.mock.calls[0][1] as GetCallOptions;
    expect(getMock.mock.calls[0][0]).toBe('/api/v1/automation-audit-events');
    expect(options.params?.query).toEqual({ cookie_id: undefined, status: undefined, trigger_type: undefined, search: undefined, run_id: undefined, page: 2, page_size: 20 });
    expect(page.totalPages).toBe(1);
    expect(page.events[0]).toMatchObject({ id: 1, cookieId: 'c1', orderId: 'o1', runId: 3, createdAtMs: 10_000, summary: 'S' });

    getMock.mockResolvedValue({ data: { success: true, total: 0, page: 1, page_size: 20, total_pages: 1, data: [] } });
    await getAutomationAuditEvents({ cookieId: 'c1', status: 'failed', triggerType: 'order_paid', search: ' o1 ', runId: 9, page: 1, pageSize: 100 });
    expect((getMock.mock.calls[1][1] as GetCallOptions).params?.query).toEqual({ cookie_id: 'c1', status: 'failed', trigger_type: 'order_paid', search: 'o1', run_id: 9, page: 1, page_size: 100 });
  });

  // 用例：已发卡密按订单读取，时间优先取售出时间并换算为毫秒。
  test('已发卡密按订单读取并归一化', /* 当前回调断言已发卡密的路径参数与时间归一化。 */ async () => {
    getMock.mockResolvedValue({ data: { success: true, data: [{ id: 1, card_name: '组A', content: 'K1', sold_at: 5, created_at: 1 }, { id: 2, card_name: '组B', content: 'K2', sold_at: 0, created_at: 3 }] } });
    // cards 是归一化后的已发卡密。
    const cards = await getAuditOrderDeliveredCards('o 1');
    expect((getMock.mock.calls[0][1] as GetCallOptions).params?.path).toEqual({ order_id: 'o 1' });
    expect(cards).toEqual([
      { id: 1, cardName: '组A', content: 'K1', deliveredAtMs: 5000 },
      { id: 2, cardName: '组B', content: 'K2', deliveredAtMs: 3000 },
    ]);
  });
});
