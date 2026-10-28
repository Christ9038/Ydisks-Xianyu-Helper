import { beforeEach, describe, expect, test, vi } from 'vitest';

// getMock 是运行日志契约 GET 请求的共享客户端替身。
const { getMock } = vi.hoisted(/* getMockFactory 创建契约客户端 GET 方法替身。 */ () => ({ getMock: vi.fn() }));

// ContractMockResult 是测试契约请求回调返回的最小响应模型。
interface ContractMockResult {
  // data 是契约客户端解码出的成功响应。
  data?: unknown;
}

vi.mock('../../../shared/api-contract/client', /* contractClientModuleMockFactory 提供运行日志 API 适配测试所需的契约依赖。 */ () => ({
  // contractClientMock 只提供本测试覆盖的 GET 操作。
  contractClient: { GET: getMock },
  // runContractRequestMock 执行 feature adapter 传入的请求回调。
  runContractRequest: /* runContractRequestMock 执行 feature adapter 传入的契约请求回调。 */ async (/* execute 是 feature adapter 构造的契约请求动作。 */ execute: (signal: AbortSignal) => Promise<ContractMockResult>) => (await execute(new AbortController().signal)).data,
}));

import { contractClient } from '../../../shared/api-contract/client';
import { fetchRuntimeLogs } from './api';

// logsGetMock 是带有生成契约类型的 GET 方法替身。
const logsGetMock = vi.mocked(contractClient.GET);

// GetCallOptions 是测试中读取契约查询参数的最小参数模型。
interface GetCallOptions {
  // params 是契约客户端收到的查询参数对象。
  params?: { /** query 是本次运行日志请求的查询键值对。 */ query?: Record<string, unknown> };
}

// transportPayload 是服务端运行日志页的传输 DTO 样例，字段名与生成契约保持一致。
const transportPayload = {
  entries: [
    {
      seq: 7,
      time: '2026-10-16T12:00:00Z',
      level: 'error',
      source: 'mtop',
      message: 'MTOP 响应失败',
      method: 'POST',
      path: '/h5/mtop.trade.order.render/1.0/',
      status: 500,
      duration_ms: 120,
      request_id: 'req-7',
      api: 'mtop.trade.order.render',
      category: 'session_expired',
      ret: 'FAIL_SYS_SESSION_EXPIRED',
      error: '会话已过期',
      account_id: 'acc-1',
      item_id: 'item-1',
      order_id: 'order-1',
      chat_id: 'chat-1',
      caller: 'engine/reply.go:312',
      func: 'engine.(*ReplyService).Handle',
      route: '/api/v1/orders/{order_id}',
      run_id: 'run-1',
      details: [{ key: 'http_status', value: '500' }],
    },
    {
      seq: 8,
      time: '2026-10-16T12:00:01Z',
      level: 'unknown-level',
      source: 'unknown-source',
      message: '未识别记录',
    },
  ],
  next_cursor: 8,
  oldest_seq: 7,
  dropped: 3,
  capacity: 2000,
};

describe('runtime logs API adapter', /* 运行日志 API 适配测试覆盖传输 DTO 到 UI 模型的转换。 */ () => {
  beforeEach(/* 当前回调重置运行日志契约请求替身。 */ () => {
    vi.clearAllMocks();
    logsGetMock.mockResolvedValue({ data: transportPayload, response: { status: 200 } } as never);
  });

  test('把传输 DTO 转换为 UI 模型并透传分页边界', /* 当前回调验证字段映射与缓冲区边界透传。 */ async () => {
    // page 是适配器转换后的运行日志页。
    const page = await fetchRuntimeLogs({ level: '', source: '', keyword: '' }, 0, 200);
    expect(page.nextCursor).toBe(8);
    expect(page.oldestSeq).toBe(7);
    expect(page.dropped).toBe(3);
    expect(page.capacity).toBe(2000);
    // entry 是转换后的平台接口失败记录。
    const entry = page.entries[0];
    expect(entry.seq).toBe(7);
    expect(entry.level).toBe('error');
    expect(entry.source).toBe('mtop');
    expect(entry.api).toBe('mtop.trade.order.render');
    expect(entry.category).toBe('session_expired');
    expect(entry.ret).toBe('FAIL_SYS_SESSION_EXPIRED');
    expect(entry.status).toBe(500);
    expect(entry.durationMs).toBe(120);
    expect(entry.requestId).toBe('req-7');
    expect(entry.accountId).toBe('acc-1');
    expect(entry.itemId).toBe('item-1');
    expect(entry.orderId).toBe('order-1');
    expect(entry.chatId).toBe('chat-1');
    expect(entry.caller).toBe('engine/reply.go:312');
    expect(entry.func).toBe('engine.(*ReplyService).Handle');
    expect(entry.route).toBe('/api/v1/orders/{order_id}');
    expect(entry.runId).toBe('run-1');
    expect(entry.details).toEqual([{ key: 'http_status', value: '500' }]);
  });

  test('缺省字段与未知枚举值回退为安全默认值', /* 当前回调验证服务端省略可选字段时页面仍可渲染。 */ async () => {
    // page 是第二条记录只提供必填字段时的转换结果。
    const page = await fetchRuntimeLogs({ level: '', source: '', keyword: '' }, 0, 200);
    // fallbackEntry 是服务端只返回必填字段的记录。
    const fallbackEntry = page.entries[1];
    expect(fallbackEntry.level).toBe('info');
    expect(fallbackEntry.source).toBe('other');
    expect(fallbackEntry.method).toBe('');
    expect(fallbackEntry.path).toBe('');
    expect(fallbackEntry.status).toBe(0);
    expect(fallbackEntry.durationMs).toBe(0);
    expect(fallbackEntry.requestId).toBe('');
    expect(fallbackEntry.api).toBe('');
    expect(fallbackEntry.category).toBe('');
    expect(fallbackEntry.ret).toBe('');
    expect(fallbackEntry.error).toBe('');
    expect(fallbackEntry.accountId).toBe('');
    expect(fallbackEntry.itemId).toBe('');
    expect(fallbackEntry.orderId).toBe('');
    expect(fallbackEntry.chatId).toBe('');
    expect(fallbackEntry.caller).toBe('');
    expect(fallbackEntry.func).toBe('');
    expect(fallbackEntry.route).toBe('');
    expect(fallbackEntry.runId).toBe('');
    expect(fallbackEntry.details).toEqual([]);
  });

  test('过滤条件与游标转换为查询参数并去掉空白关键字', /* 当前回调验证请求参数契约与关键字归一化。 */ async () => {
    await fetchRuntimeLogs({ level: 'error', source: 'mtop', keyword: '  order  ' }, 12, 50);
    // params 是适配器传给契约客户端的查询参数。
    const params = (logsGetMock.mock.calls[0][1] as GetCallOptions).params?.query;
    expect(params).toEqual({ since: 12, limit: 50, level: 'error', source: 'mtop', keyword: 'order' });
  });

  test('空过滤条件不发送可选查询参数', /* 当前回调验证空过滤不会把空字符串发给服务端。 */ async () => {
    await fetchRuntimeLogs({ level: '', source: '', keyword: '   ' }, 0, 200);
    // params 是空过滤条件下传给契约客户端的查询参数。
    const params = (logsGetMock.mock.calls[0][1] as GetCallOptions).params?.query;
    expect(params).toEqual({ since: 0, limit: 200, level: undefined, source: undefined, keyword: undefined });
  });
});
