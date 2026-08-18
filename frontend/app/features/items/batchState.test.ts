import { describe, expect, test } from 'vitest';
import {
  batchStatusText,
  batchStatusClass,
  canRetryBatch,
  canStartBatch,
  isBatchInProgress,
  isCurrentBatchRequest,
  selectActivePublishBatch,
} from './batchState';

// ItemList 批量行为测试覆盖预检、取消、重试和过期任务响应。
describe('ItemList batch state',
  // 测试组回调集中验证批量状态机的关键分支。
  () => {
  // 预检通过且存在有效行时才能启动批量任务。
  test('starts only when the preview has publishable rows',
    // 预检成功场景回调验证有效行门禁。
    () => {
    expect(canStartBatch({ preview_id: 'preview-1', valid: 2 })).toBe(true);
    expect(canStartBatch({ preview_id: 'preview-1', valid: 0 })).toBe(false);
    expect(canStartBatch(null)).toBe(false);
    });

  // 等待、运行中和安全取消中的任务都必须继续轮询远端结果。
  test('keeps polling pending, running and canceling tasks',
    // 轮询场景回调验证等待、运行和取消中的任务状态。
    () => {
    expect(isBatchInProgress('pending')).toBe(true);
    expect(isBatchInProgress('running')).toBe(true);
    expect(isBatchInProgress('canceling')).toBe(true);
    expect(isBatchInProgress('completed')).toBe(false);
    expect(batchStatusText('canceling')).toBe('正在安全取消');
    });

  // 只有非运行状态下仍有失败行的批次允许重试。
  test('allows retry only for retryable completed failures',
    // 重试场景回调验证失败行和状态门禁。
    () => {
    expect(canRetryBatch({ id: 'batch-1', retryable: 2, status: 'failed' })).toBe(true);
    expect(canRetryBatch({ id: 'batch-1', retryable: 2, status: 'running' })).toBe(false);
    expect(canRetryBatch({ id: 'batch-1', retryable: 0, status: 'failed' })).toBe(false);
    });

  // 新任务代次产生后，旧轮询响应必须被视为过期并丢弃。
  test('rejects an expired polling response',
    // 过期响应场景回调验证轮询代次门禁。
    () => {
    expect(isCurrentBatchRequest(1, 2)).toBe(false);
    expect(isCurrentBatchRequest(2, 2)).toBe(true);
    });

  // 已完成历史不能覆盖新任务上传流程，运行任务仍可恢复。
  test('selects only an active recoverable batch',
    // 恢复场景回调验证完成历史不会覆盖新流程。
    () => {
    expect(selectActivePublishBatch([{ id: 'done', status: 'completed' }])).toBeUndefined();
    expect(selectActivePublishBatch([{ id: 'done', status: 'completed' }, { id: 'active', status: 'running' }])?.id).toBe('active');
  });

  test('covers all batch status labels and style groups',
    // 状态展示场景回调验证后端状态到中文和样式的完整映射。
    () => {
    // labels 是批量状态到中文文案的完整断言表。
    const labels: Record<string, string> = {
      preview: '待确认', pending: '等待中', running: '发布中', canceling: '正在安全取消', success: '成功',
      failed: '失败', completed: '已完成', partially_failed: '部分失败', canceled: '已取消', unknown: 'unknown',
    };
    Object.entries(labels).forEach(
      // entry 是状态值和对应的中文文案。
      ([status, label]) => expect(batchStatusText(status)).toBe(label),
    );
    expect(batchStatusText()).toBe('-');
    expect(batchStatusClass('success')).toContain('bg-emerald-50');
    expect(batchStatusClass('completed')).toContain('bg-emerald-50');
    expect(batchStatusClass('partially_failed')).toContain('bg-red-50');
    expect(batchStatusClass('failed')).toContain('bg-red-50');
    expect(batchStatusClass('running')).toContain('bg-blue-50');
    expect(batchStatusClass('canceled')).toContain('bg-gray-100');
    expect(batchStatusClass('pending')).toContain('bg-amber-50');
    });
});
