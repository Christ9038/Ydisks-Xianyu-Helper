import { describe, expect, test } from 'vitest';
import {
  batchStatusText,
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

  // 运行中和安全取消中的任务都必须继续轮询远端结果。
  test('keeps polling running and canceling tasks',
    // 轮询场景回调验证运行和取消中的任务状态。
    () => {
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
  });
