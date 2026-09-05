import { afterEach, expect, test, vi } from 'vitest';
import { syncOrders } from './api';

afterEach(/* 清理订单契约请求的全局网络替身。 */ () => vi.unstubAllGlobals());

test.each([false, true])('刷新计数兼容老任务并保留新任务统计，终态复查=%s', /* finalRead 决定使用普通轮询还是超时取消后的终态复查路径。 */ async (finalRead) => {
  // counts 分别表示旧任务省略统计、新任务提供恢复和错绑修正统计。
  for (const /* counts 是当前新旧持久化任务的计数字段组合。 */ counts of [{}, { restored: 3, reassigned: 2 }]) {
    // response 是旧后端仍合法返回的最小订单刷新结果。
    const response = { partial_failure: false, message: '同步完成', summary: { discovered: 0, list_updated: 0, soft_deleted: 0, detail_total: 0, total: 0, updated: 0, no_change: 0, failed: 0, ...counts }, results: [] };
    // fetchMock 按任务创建、可选取消、终态查询顺序返回契约响应。
    const fetchMock = vi.fn().mockResolvedValueOnce(Response.json({ success: true, job_id: 'job-counts', status: 'running' }));
    if (finalRead) fetchMock.mockResolvedValueOnce(Response.json({ success: true, job_id: 'job-counts', status: 'cancelled' }));
    fetchMock.mockResolvedValueOnce(Response.json({ success: true, job_id: 'job-counts', status: 'succeeded', result: response }));
    vi.stubGlobal('fetch', fetchMock);
    // result 是 adapter 最终输出给 UI 的模型，两个可选 transport 字段必须已补零。
    const result = await syncOrders(undefined, undefined, { pollLimit: finalRead ? 0 : 1 });
    expect(result.summary).toMatchObject({ restored: counts.restored ?? 0, reassigned: counts.reassigned ?? 0 });
    expect(result.message).toBe('同步完成');
  }
});
