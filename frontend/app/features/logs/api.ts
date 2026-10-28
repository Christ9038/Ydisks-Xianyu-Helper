import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';
import { normalizeRuntimeLogLevel, normalizeRuntimeLogSource } from './models';
import type { RuntimeLogPage, RuntimeLogQuery } from './types';

/** fetchRuntimeLogs 读取运行日志增量页，并把传输 DTO 归一化为页面模型。 */
export const fetchRuntimeLogs = async (
  query: RuntimeLogQuery,
  since: number,
  limit: number,
  options?: RequestControlOptions,
): Promise<RuntimeLogPage> => {
  // data 是契约客户端按生成类型返回的运行日志传输 DTO。
  const data = await runContractRequest(/* signal 是本次运行日志请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/logs/runtime', {
    params: {
      query: {
        since,
        limit,
        level: query.level === '' ? undefined : query.level,
        source: query.source === '' ? undefined : query.source,
        keyword: query.keyword.trim() === '' ? undefined : query.keyword.trim(),
      },
    },
    signal,
  }), options);
  return {
    entries: data.entries.map(/* entry 是服务端返回的一条运行日志传输 DTO。 */ entry => ({
      seq: entry.seq,
      time: entry.time,
      level: normalizeRuntimeLogLevel(entry.level),
      source: normalizeRuntimeLogSource(entry.source),
      message: entry.message,
      method: entry.method ?? '',
      path: entry.path ?? '',
      status: entry.status ?? 0,
      durationMs: entry.duration_ms ?? 0,
      requestId: entry.request_id ?? '',
      api: entry.api ?? '',
      category: entry.category ?? '',
      ret: entry.ret ?? '',
      error: entry.error ?? '',
      accountId: entry.account_id ?? '',
      itemId: entry.item_id ?? '',
      orderId: entry.order_id ?? '',
      chatId: entry.chat_id ?? '',
      caller: entry.caller ?? '',
      func: entry.func ?? '',
      route: entry.route ?? '',
      runId: entry.run_id ?? '',
      details: (entry.details ?? []).map(/* detail 是服务端返回的一条附加属性。 */ detail => ({ key: detail.key, value: detail.value })),
    })),
    nextCursor: data.next_cursor,
    oldestSeq: data.oldest_seq,
    dropped: data.dropped,
    capacity: data.capacity,
  };
};
