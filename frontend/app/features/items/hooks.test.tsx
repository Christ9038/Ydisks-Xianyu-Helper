// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { BatchCancelResponse,BatchIDResponse,CategoryRecommendationResponse,ItemPublishBatchPreviewResponse,ItemPublishBatchResponse } from './api';
import {
cancelItemPublishBatch,
deleteItemPublishBatch,
getItemPublishBatch,
getItemPublishBatches,
previewItemPublishBatch,
recommendPublishCategory,
retryFailedItemPublishBatch,
startItemPublishBatch,
} from './api';
import { useItemPublishBatch } from './hooks';

vi.mock('./api', /* itemsApiMockFactory 提供批量发布 Hook 的确定性 API 替身。 */ () => ({
  cancelItemPublishBatch: vi.fn(),
  deleteItemPublishBatch: vi.fn(),
  getItemPublishBatch: vi.fn(),
  getItemPublishBatches: vi.fn(),
  previewItemPublishBatch: vi.fn(),
  recommendPublishCategory: vi.fn(),
  retryFailedItemPublishBatch: vi.fn(),
  startItemPublishBatch: vi.fn(),
}));

// cancelBatchMock 是取消批量任务的可控替身。
const cancelBatchMock = vi.mocked(cancelItemPublishBatch);
// deleteBatchMock 是删除预检任务的可控替身。
const deleteBatchMock = vi.mocked(deleteItemPublishBatch);
// getBatchMock 是读取批量详情的可控替身。
const getBatchMock = vi.mocked(getItemPublishBatch);
// getBatchesMock 是读取批量任务列表的可控替身。
const getBatchesMock = vi.mocked(getItemPublishBatches);
// previewBatchMock 是批量预检的可控替身。
const previewBatchMock = vi.mocked(previewItemPublishBatch);
// recommendCategoryMock 是类目推荐的可控替身。
const recommendCategoryMock = vi.mocked(recommendPublishCategory);
// retryBatchMock 是失败行重试的可控替身。
const retryBatchMock = vi.mocked(retryFailedItemPublishBatch);
// startBatchMock 是启动批量任务的可控替身。
const startBatchMock = vi.mocked(startItemPublishBatch);

// previewFixture 是预检成功且存在可发布行的响应。
const previewFixture: ItemPublishBatchPreviewResponse = { success: true, preview_id: 'preview-1', total: 1, valid: 1, invalid: 0, rows: [] };
// runningBatchFixture 是可恢复的运行中任务详情。
const runningBatchFixture: ItemPublishBatchResponse = { id: 'batch-1', status: 'running', filename: 'items.xlsx', total: 1, success: 0, failed: 0, pending: 0, running: 1, retryable: 0, rows: [], created_at: '2026-08-15T00:00:00Z', updated_at: '2026-08-15T00:00:00Z' };
// failedBatchFixture 是存在可重试失败行的任务详情。
const failedBatchFixture: ItemPublishBatchResponse = { ...runningBatchFixture, status: 'failed', failed: 1, running: 0, retryable: 1 };

describe('useItemPublishBatch', /* 当前回调处理批量发布的表单、任务和异常状态。 */ () => {
  beforeEach(/* 当前回调重置批量 API 和浏览器交互替身。 */ () => {
    vi.clearAllMocks();
    getBatchesMock.mockResolvedValue([runningBatchFixture]);
    getBatchMock.mockResolvedValue(runningBatchFixture);
    recommendCategoryMock.mockResolvedValue({ success: true, category: { cat_id: 'cat-1', cat_name: '服饰', channel_cat_id: 'channel-1', tb_cat_id: 'tb-1' } } satisfies CategoryRecommendationResponse);
    previewBatchMock.mockResolvedValue(previewFixture);
    startBatchMock.mockResolvedValue({ success: true, batch_id: 'batch-1' } satisfies BatchIDResponse);
    cancelBatchMock.mockResolvedValue({ success: true, status: 'canceling' } satisfies BatchCancelResponse);
    deleteBatchMock.mockResolvedValue({ success: true });
    retryBatchMock.mockResolvedValue({ success: true, batch_id: 'batch-1' } satisfies BatchIDResponse);
    // alertMock 是批量表单提示的浏览器替身。
    vi.stubGlobal('alert', vi.fn());
    // confirmMock 默认允许继续执行危险操作。
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true));
  });

  test('可以恢复任务、预检、启动、取消和重试批量发布', /* 当前回调验证批量流程的主要成功路径。 */ async () => {
    // loadItems 是完成批量任务后刷新商品列表的替身。
    const loadItems = vi.fn().mockResolvedValue(undefined);
    // loadShippingRules 是完成批量任务后刷新规则列表的替身。
    const loadShippingRules = vi.fn().mockResolvedValue(undefined);
    // hook 是批量发布 Hook 的渲染结果。
    const hook = renderHook(
      // batchHookFactory 创建默认账号场景的批量 Hook。
      () => useItemPublishBatch({ selectedAccount: 'account-1', loadItems, loadShippingRules }),
    );

    await act(
      // openAction 打开弹窗并恢复服务端任务。
      async () => hook.result.current.openBatchModal(),
    );
    expect(hook.result.current.showBatchModal).toBe(true);
    expect(hook.result.current.batchDetail).toEqual(runningBatchFixture);
    expect(hook.result.current.batchPhase).toBe('running');

    await act(
      // keywordAction 写入类目搜索词。
      () => hook.result.current.setBatchCategoryKeyword('  服饰  '),
    );
    await act(
      // recommendAction 请求推荐类目。
      async () => hook.result.current.handleRecommendBatchCategory(),
    );
    expect(recommendCategoryMock).toHaveBeenCalledWith('account-1', '服饰', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.batchFallbackCategory.catId).toBe('cat-1');

    await act(
      // fileAction 设置商品表格文件。
      () => hook.result.current.setBatchFile(new File(['title'], 'items.xlsx')),
    );
    await act(
      // previewAction 提交批量预检。
      async () => hook.result.current.handlePreviewBatch(),
    );
    expect(previewBatchMock).toHaveBeenCalled();
    expect(hook.result.current.batchPreview).toEqual(previewFixture);
    expect(hook.result.current.batchPhase).toBe('preview');

    await act(
      // startAction 启动预检通过的任务。
      async () => hook.result.current.handleStartBatch(),
    );
    expect(startBatchMock).toHaveBeenCalledWith('preview-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.batchDetail).toEqual(runningBatchFixture);

    await act(
      // cancelAction 请求安全取消任务。
      async () => hook.result.current.handleCancelBatch(),
    );
    expect(cancelBatchMock).toHaveBeenCalledWith('batch-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));

    getBatchMock.mockResolvedValueOnce(failedBatchFixture);
    await act(
      // recentAction 读取最近任务结果。
      async () => hook.result.current.openRecentBatchResult(),
    );
    await act(
      // failedDetailAction 注入可重试失败任务，隔离重试按钮状态机。
      () => hook.result.current.setBatchDetail(failedBatchFixture),
    );
    expect(hook.result.current.batchDetail).toMatchObject({ id: 'batch-1', status: 'failed', retryable: 1 });
    await act(
      // retryAction 重试失败行。
      async () => hook.result.current.handleRetryBatchFailed(),
    );
    expect(retryBatchMock).toHaveBeenCalledWith('batch-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));

    await act(
      // closeAction 关闭弹窗并清理临时预检。
      async () => hook.result.current.closeBatchModal(),
    );
    expect(hook.result.current.showBatchModal).toBe(false);
    hook.unmount();
  });

  test('无账号、空关键词、无文件和异常响应会阻止危险操作', /* 当前回调验证批量表单守卫和错误分支。 */ async () => {
    // loadItems 是错误场景下的列表刷新替身。
    const loadItems = vi.fn().mockResolvedValue(undefined);
    // loadShippingRules 是错误场景下的规则刷新替身。
    const loadShippingRules = vi.fn().mockResolvedValue(undefined);
    // hook 是无默认账号场景的 Hook 渲染结果。
    const hook = renderHook(
      // noAccountHookFactory 创建无默认账号场景的批量 Hook。
      () => useItemPublishBatch({ selectedAccount: '', loadItems, loadShippingRules }),
    );
    await act(
      // emptyKeywordAction 触发空关键词校验。
      async () => hook.result.current.handleRecommendBatchCategory(),
    );
    expect(recommendCategoryMock).not.toHaveBeenCalled();
    await act(
      // noFileAction 触发无文件校验。
      async () => hook.result.current.handlePreviewBatch(),
    );
    expect(previewBatchMock).not.toHaveBeenCalled();

    await act(
      // keywordAction 写入关键词以覆盖无账号守卫。
      () => hook.result.current.setBatchCategoryKeyword('服饰'),
    );
    await act(
      // noAccountAction 触发无默认账号校验。
      async () => hook.result.current.handleRecommendBatchCategory(),
    );
    expect(recommendCategoryMock).not.toHaveBeenCalled();

    getBatchesMock.mockRejectedValueOnce(new Error('恢复失败'));
    await act(
      // openErrorAction 验证恢复任务失败仍能结束加载。
      async () => hook.result.current.openBatchModal(),
    );
    expect(hook.result.current.batchLoading).toBe(false);

    await act(
      // abandonAction 清理没有预检的上传状态。
      async () => hook.result.current.abandonBatchPreview(),
    );
    expect(deleteBatchMock).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('类目、预检、启动、取消和重试失败都会提示错误', /* 当前回调验证批量动作的异常响应分支。 */ async () => {
    // hook 是默认账号异常动作场景的批量 Hook 渲染结果。
    const hook = renderHook(/* errorHookFactory 创建异常动作场景的 Hook。 */ () => useItemPublishBatch({ selectedAccount: 'account-1', loadItems: vi.fn(), loadShippingRules: vi.fn() }));
    await act(
      // keywordAction 写入类目关键词。
      () => hook.result.current.setBatchCategoryKeyword('服饰'),
    );
    recommendCategoryMock.mockRejectedValueOnce(new Error('类目失败'));
    await act(
      // recommendAction 触发失败的类目推荐。
      async () => hook.result.current.handleRecommendBatchCategory(),
    );
    expect(alert).toHaveBeenCalledWith('类目失败');

    await act(
      // fileAction 设置商品表格文件。
      () => hook.result.current.setBatchFile(new File(['title'], 'items.xlsx')),
    );
    previewBatchMock.mockRejectedValueOnce(new Error('预检失败'));
    await act(
      // previewAction 触发失败的批量预检。
      async () => hook.result.current.handlePreviewBatch(),
    );
    expect(alert).toHaveBeenCalledWith('预检失败');

    await act(
      // previewStateAction 注入可启动预检结果。
      () => hook.result.current.setBatchPreview(previewFixture),
    );
    startBatchMock.mockRejectedValueOnce(new Error('启动失败'));
    await act(
      // startAction 触发失败的批量启动。
      async () => hook.result.current.handleStartBatch(),
    );
    expect(alert).toHaveBeenCalledWith('启动失败');

    await act(
      // detailStateAction 注入运行中的任务详情。
      () => hook.result.current.setBatchDetail(runningBatchFixture),
    );
    cancelBatchMock.mockRejectedValueOnce(new Error('取消失败'));
    await act(
      // cancelAction 触发失败的批量取消。
      async () => hook.result.current.handleCancelBatch(),
    );
    expect(alert).toHaveBeenCalledWith('取消失败');

    await act(
      // failedDetailAction 注入存在失败行的任务详情。
      () => hook.result.current.setBatchDetail(failedBatchFixture),
    );
    retryBatchMock.mockRejectedValueOnce(new Error('重试失败'));
    await act(
      // retryAction 触发失败行重试错误。
      async () => hook.result.current.handleRetryBatchFailed(),
    );
    expect(alert).toHaveBeenCalledWith('重试失败');

    await act(
      // recentStateAction 注入最近任务以覆盖结果读取异常。
      () => hook.result.current.setRecentBatch(runningBatchFixture),
    );
    getBatchMock.mockRejectedValueOnce(new Error('结果读取失败'));
    await act(
      // recentAction 触发最近任务读取错误。
      async () => hook.result.current.openRecentBatchResult(),
    );
    expect(hook.result.current.batchLoading).toBe(false);

    await act(
      // previewStateAction 注入临时预检任务。
      () => {
        hook.result.current.setBatchPreview(previewFixture);
        hook.result.current.setBatchPhase('preview');
      },
    );
    deleteBatchMock.mockRejectedValueOnce(new Error('清理失败'));
    await act(
      // abandonAction 触发临时预检清理错误。
      async () => hook.result.current.abandonBatchPreview(),
    );
    expect(hook.result.current.batchPhase).toBe('upload');
    hook.unmount();
  });

  test('轮询完成后刷新商品和发货规则列表', /* 当前回调验证批量轮询完成分支。 */ async () => {
    vi.useFakeTimers();
    // loadItems 是轮询完成后的商品列表刷新替身。
    const loadItems = vi.fn().mockResolvedValue(undefined);
    // loadShippingRules 是轮询完成后的规则列表刷新替身。
    const loadShippingRules = vi.fn().mockResolvedValue(undefined);
    // doneBatchFixture 是轮询返回的完成任务详情。
    const doneBatchFixture = { ...runningBatchFixture, status: 'completed' as const, running: 0, success: 1 };
    getBatchMock.mockResolvedValueOnce(doneBatchFixture);
    // hook 是批量轮询场景的 Hook 渲染结果。
    const hook = renderHook(/* pollingHookFactory 创建批量轮询场景的 Hook。 */ () => useItemPublishBatch({ selectedAccount: 'account-1', loadItems, loadShippingRules }));
    await act(
      // stateAction 打开弹窗并注入运行中的任务。
      () => {
        hook.result.current.setShowBatchModal(true);
        hook.result.current.setBatchDetail(runningBatchFixture);
      },
    );
    await act(/* timerAction 推进批量轮询计时器。 */ async () => {
      await vi.advanceTimersByTimeAsync(3_000);
    });
    expect(getBatchMock).toHaveBeenCalledWith('batch-1', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(loadItems).toHaveBeenCalledOnce();
    expect(loadShippingRules).toHaveBeenCalledOnce();
    hook.unmount();
    vi.useRealTimers();
  });

  test('批量结果和取消重试守卫阻止无效操作', /* 当前回调验证批量发布结果、取消和重试的前置条件。 */ async () => {
    // hook 是批量操作守卫场景的 Hook 渲染结果。
    const hook = renderHook(/* guardHookFactory 创建批量操作守卫场景的 Hook。 */ () => useItemPublishBatch({ selectedAccount: 'account-1', loadItems: vi.fn(), loadShippingRules: vi.fn() }));
    await act(
      // emptyKeywordAction 在有账号但没有关键词时阻止类目推荐。
      async () => hook.result.current.handleRecommendBatchCategory(),
    );
    expect(recommendCategoryMock).not.toHaveBeenCalled();
    await act(
      // emptyRecentAction 在没有最近任务时保持状态稳定。
      async () => hook.result.current.openRecentBatchResult(),
    );
    expect(getBatchMock).not.toHaveBeenCalled();
    await act(
      // emptyStartAction 在没有预检结果时阻止启动。
      async () => hook.result.current.handleStartBatch(),
    );
    expect(startBatchMock).not.toHaveBeenCalled();
    await act(
      // noDetailCancelAction 在没有任务详情时阻止取消请求。
      async () => hook.result.current.handleCancelBatch(),
    );
    expect(cancelBatchMock).not.toHaveBeenCalled();
    await act(
      // invalidPreviewAction 注入没有有效行的预检结果。
      () => hook.result.current.setBatchPreview({ ...previewFixture, valid: 0 }),
    );
    await act(
      // invalidStartAction 阻止没有可发布行的任务启动。
      async () => hook.result.current.handleStartBatch(),
    );
    expect(startBatchMock).not.toHaveBeenCalled();
    expect(alert).toHaveBeenCalledWith('没有可发布的商品行');
    await act(
      // noAccountPreviewAction 验证有文件但没有默认账号时阻止预检。
      () => hook.result.current.setBatchFile(new File(['title'], 'items.xlsx')),
    );
    // noAccountHook 是没有默认账号的预检 Hook 渲染结果。
    const noAccountHook = renderHook(/* noAccountPreviewHookFactory 创建无账号预检场景的 Hook。 */ () => useItemPublishBatch({ selectedAccount: '', loadItems: vi.fn(), loadShippingRules: vi.fn() }));
    await act(
      // noAccountFileAction 为无账号预检场景写入文件。
      () => noAccountHook.result.current.setBatchFile(new File(['title'], 'items.xlsx')),
    );
    await act(
      // noAccountPreviewAction2 触发无默认账号预检守卫。
      async () => noAccountHook.result.current.handlePreviewBatch(),
    );
    expect(previewBatchMock).not.toHaveBeenCalled();
    noAccountHook.unmount();
    await act(
      // detailAction 注入可取消的任务详情。
      () => hook.result.current.setBatchDetail(runningBatchFixture),
    );
    vi.mocked(window.confirm).mockReturnValue(false);
    await act(
      // rejectedCancelAction 验证用户拒绝危险取消操作。
      async () => hook.result.current.handleCancelBatch(),
    );
    expect(cancelBatchMock).not.toHaveBeenCalled();
    await act(
      // completedDetailAction 注入不可重试的已完成任务。
      () => hook.result.current.setBatchDetail({ ...runningBatchFixture, status: 'completed', retryable: 0 }),
    );
    await act(
      // invalidRetryAction 阻止没有可重试失败行的任务重试。
      async () => hook.result.current.handleRetryBatchFailed(),
    );
    expect(retryBatchMock).not.toHaveBeenCalled();
    hook.unmount();
  });

  test('批量轮询失败时记录错误并保留任务状态', /* 当前回调验证批量任务轮询错误容错。 */ async () => {
    vi.useFakeTimers();
    // consoleError 是批量轮询错误日志的可控替身。
    const consoleError = vi.spyOn(console, 'error').mockImplementation(/* errorLogger 忽略测试日志输出。 */ () => undefined);
    try {
      // hook 是批量轮询失败场景的 Hook 渲染结果。
      const hook = renderHook(/* pollErrorHookFactory 创建批量轮询失败场景的 Hook。 */ () => useItemPublishBatch({ selectedAccount: 'account-1', loadItems: vi.fn(), loadShippingRules: vi.fn() }));
      getBatchMock.mockRejectedValueOnce(new Error('轮询失败'));
      await act(
        // stateAction 打开弹窗并注入运行中的任务。
        () => {
          hook.result.current.setShowBatchModal(true);
          hook.result.current.setBatchDetail(runningBatchFixture);
        },
      );
      await act(
        // timerAction 推进轮询错误定时器。
        async () => { await vi.advanceTimersByTimeAsync(3_000); },
      );
      expect(consoleError).toHaveBeenCalledWith('刷新批量铺货进度失败:', expect.any(Error));
      hook.unmount();
    } finally {
      consoleError.mockRestore();
      vi.useRealTimers();
    }
  });

  test('批量轮询避免并发请求并丢弃关闭弹窗后的旧响应', /* 当前回调验证批量轮询并发与代次隔离。 */ async () => {
    vi.useFakeTimers();
    // resolvePoll 是延迟轮询请求的完成控制器。
    let resolvePoll: (value: ItemPublishBatchResponse) => void = () => undefined;
    // pendingPoll 是保持轮询进行中的请求 Promise。
    const pendingPoll = new Promise<ItemPublishBatchResponse>(/* pollExecutor 保存轮询完成函数。 */ resolve => { resolvePoll = resolve; });
    getBatchMock.mockReset();
    getBatchMock.mockReturnValueOnce(pendingPoll);
    // hook 是批量轮询并发场景的 Hook 渲染结果。
    const hook = renderHook(/* stalePollHookFactory 创建批量轮询并发场景的 Hook。 */ () => useItemPublishBatch({ selectedAccount: 'account-1', loadItems: vi.fn(), loadShippingRules: vi.fn() }));
    await act(
      // stateAction 打开弹窗并注入运行中的任务详情。
      () => {
        hook.result.current.setShowBatchModal(true);
        hook.result.current.setBatchDetail(runningBatchFixture);
      },
    );
    await act(
      // firstTimerAction 推进首次轮询并保持请求未完成。
      async () => { await vi.advanceTimersByTimeAsync(3_000); },
    );
    await act(
      // secondTimerAction 推进下一次轮询但不允许并发请求。
      async () => { await vi.advanceTimersByTimeAsync(3_000); },
    );
    expect(getBatchMock).toHaveBeenCalledOnce();
    await act(
      // closeAction 关闭弹窗并使未完成轮询响应过期。
      async () => hook.result.current.closeBatchModal(),
    );
    resolvePoll(runningBatchFixture);
    await act(
      // staleResolveAction 完成已过期的轮询响应。
      async () => { await pendingPoll; },
    );
    expect(hook.result.current.showBatchModal).toBe(false);
    hook.unmount();
    vi.useRealTimers();
  });

  test('快速重开弹窗时丢弃上一次恢复请求的晚到响应', /* 当前回调验证用户动作请求代次隔离。 */ async () => {
    // resolveFirstOpen 是首次打开弹窗的延迟批次列表完成函数。
    let resolveFirstOpen: (value: ItemPublishBatchResponse[]) => void = () => undefined;
    // firstOpenBatches 是故意晚到的首次恢复请求。
    const firstOpenBatches = new Promise<ItemPublishBatchResponse[]>(/* firstOpenExecutor 保存首次恢复响应完成函数。 */ resolve => { resolveFirstOpen = resolve; });
    getBatchesMock.mockReset();
    getBatchesMock.mockReturnValueOnce(firstOpenBatches).mockResolvedValueOnce([]);
    // hook 是快速关闭并重新打开批量弹窗时的状态容器。
    const hook = renderHook(/* reopenHookFactory 创建快速重开场景的批量 Hook。 */ () => useItemPublishBatch({ selectedAccount: 'account-1', loadItems: vi.fn(), loadShippingRules: vi.fn() }));
    await act(
      // firstOpenAction 启动会延迟返回的首次恢复请求。
      async () => { void hook.result.current.openBatchModal(); },
    );
    await act(
      // closeAction 关闭首次弹窗并使其请求代次失效。
      async () => hook.result.current.closeBatchModal(),
    );
    await act(
      // secondOpenAction 创建新的弹窗会话；该会话没有待恢复任务。
      async () => hook.result.current.openBatchModal(),
    );
    resolveFirstOpen([runningBatchFixture]);
    await act(
      // staleResponseAction 交付首次弹窗的晚到响应。
      async () => { await firstOpenBatches; },
    );
    expect(hook.result.current.showBatchModal).toBe(true);
    expect(hook.result.current.batchDetail).toBeNull();
    expect(hook.result.current.batchPhase).toBe('upload');
    hook.unmount();
  });
});
