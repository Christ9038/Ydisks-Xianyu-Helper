import { useCallback, useEffect, useRef, useState } from 'react';
import {
  cancelItemPublishBatch,
  deleteItemPublishBatch,
  getItemPublishBatch,
  getItemPublishBatches,
  recommendPublishCategory,
  previewItemPublishBatch,
  retryFailedItemPublishBatch,
  startItemPublishBatch,
} from './api';
import { canRetryBatch, canStartBatch, isBatchInProgress, isCurrentBatchRequest, selectActivePublishBatch } from './batchState';
import type {
  BatchFallbackCategory,
  BatchPhase,
  ItemPublishBatchOptions,
  ItemPublishBatchState,
  PublishBatchDetail,
  PublishBatchPreview,
} from './types';

// useItemPublishBatch 集中管理批量铺货的表单、任务恢复、轮询和重试状态。
export const useItemPublishBatch = (options: ItemPublishBatchOptions): ItemPublishBatchState => {
  // showBatchModal 表示批量铺货弹窗是否打开。
  const [showBatchModal, setShowBatchModal] = useState(false);
  // batchPhase 表示批量流程当前所处步骤。
  const [batchPhase, setBatchPhase] = useState<BatchPhase>('upload');
  // batchFile 保存待预检的商品表格文件。
  const [batchFile, setBatchFile] = useState<File | null>(null);
  // batchImagesZip 保存可选的商品图片压缩包。
  const [batchImagesZip, setBatchImagesZip] = useState<File | null>(null);
  // batchCategoryKeyword 保存默认类目搜索词。
  const [batchCategoryKeyword, setBatchCategoryKeyword] = useState('');
  // batchCategoryLoading 表示默认类目推荐请求是否正在执行。
  const [batchCategoryLoading, setBatchCategoryLoading] = useState(false);
  // batchFallbackCategory 保存默认类目配置。
  const [batchFallbackCategory, setBatchFallbackCategory] = useState<BatchFallbackCategory>({ catId: '', catName: '', channelCatId: '', tbCatId: '' });
  // batchPreview 保存最近一次批量预检结果。
  const [batchPreview, setBatchPreview] = useState<PublishBatchPreview | null>(null);
  // batchDetail 保存当前批量任务的服务端详情。
  const [batchDetail, setBatchDetail] = useState<PublishBatchDetail | null>(null);
  // recentBatch 保存页面入口处展示的最近任务结果。
  const [recentBatch, setRecentBatch] = useState<PublishBatchDetail | null>(null);
  // batchLocations 保存批量任务可选的发货地列表。
  const [batchLocations, setBatchLocations] = useState<NonNullable<ItemPublishBatchState['batchLocations']>>([]);
  // batchLocation 保存批量任务当前选中的发货地。
  const [batchLocation, setBatchLocation] = useState<ItemPublishBatchState['batchLocation']>(null);
  // batchLoading 表示批量任务请求是否正在执行。
  const [batchLoading, setBatchLoading] = useState(false);
  // batchPollInFlight 防止同一批次的轮询请求重叠。
  const batchPollInFlight = useRef(false);
  // batchRequestGeneration 用于丢弃弹窗关闭后返回的过期轮询响应。
  const batchRequestGeneration = useRef(0);

  // openBatchModal 初始化上传表单，并恢复仍在执行的批量任务。
  const openBatchModal = useCallback(
    // 批量弹窗打开器负责初始化临时表单和恢复任务。
    async () => {
    setBatchPhase('upload');
    setBatchPreview(null);
    setBatchDetail(null);
    setBatchFile(null);
    setBatchImagesZip(null);
    setBatchCategoryKeyword('');
    setBatchFallbackCategory({ catId: '', catName: '', channelCatId: '', tbCatId: '' });
    setBatchLocations([]);
    setBatchLocation(null);
    setShowBatchModal(true);
    setBatchLoading(true);
    try {
      // batches 是最近批量任务摘要，用于寻找可恢复任务。
      const batches = await getItemPublishBatches(20);
      // recoverable 是仍处于运行或安全取消阶段的任务。
      const recoverable = selectActivePublishBatch(batches);
      if (recoverable?.id) {
        // detail 是可恢复任务的完整详情。
        const detail = await getItemPublishBatch(recoverable.id);
        setRecentBatch(detail);
        setBatchDetail(detail);
        setBatchPhase(['running', 'canceling'].includes(detail.status) ? 'running' : 'done');
      }
    } catch (error /* 恢复任务错误 */) {
      console.error('恢复最近批量铺货任务失败:', error);
    } finally {
      setBatchLoading(false);
    }
    },
    [],
  );

  // handleRecommendBatchCategory 请求默认发布账号对应的推荐类目。
  const handleRecommendBatchCategory = useCallback(
    // 类目推荐动作响应用户点击或回车提交。
    async () => {
    // keyword 是去除空白后的类目搜索词。
    const keyword = batchCategoryKeyword.trim();
    if (!options.selectedAccount) {
      alert('请先选择默认发布账号');
      return;
    }
    if (!keyword) {
      alert('请输入类目关键词');
      return;
    }
    setBatchCategoryLoading(true);
    try {
      // result 是类目推荐接口返回的具名响应。
      const result = await recommendPublishCategory(options.selectedAccount, keyword);
      // category 是后端返回的推荐类目。
      const category = result.category;
      setBatchFallbackCategory({
        catId: category.cat_id,
        catName: category.cat_name,
        channelCatId: category.channel_cat_id,
        tbCatId: category.tb_cat_id || '',
      });
    } catch (error: any /* 推荐类目错误 */) {
      console.error('获取推荐类目失败:', error);
      alert(error?.message || '没有匹配到类目，请换一个更具体的关键词');
    } finally {
      setBatchCategoryLoading(false);
    }
    },
    [batchCategoryKeyword, options.selectedAccount],
  );

  // openRecentBatchResult 加载最近批量任务详情并打开结果弹窗。
  const openRecentBatchResult = useCallback(
    // 最近结果打开器只读取已存在的批量任务。
    async () => {
    if (!recentBatch?.id) return;
    setBatchLoading(true);
    setShowBatchModal(true);
    try {
      // detail 是最近任务的最新服务端状态。
      const detail = await getItemPublishBatch(recentBatch.id);
      setBatchDetail(detail);
      setBatchPhase(['running', 'canceling'].includes(detail.status) ? 'running' : 'done');
    } catch (error /* 最近结果读取错误 */) {
      console.error('加载最近批量铺货结果失败:', error);
    } finally {
      setBatchLoading(false);
    }
    },
    [recentBatch?.id],
  );

  // handlePreviewBatch 上传表格并执行批量预检。
  const handlePreviewBatch = useCallback(
    // 批量预检动作提交上传文件和默认配置。
    async () => {
    if (!batchFile) {
      alert('请先上传商品表格');
      return;
    }
    if (!options.selectedAccount) {
      alert('请先选择默认发布账号');
      return;
    }
    setBatchLoading(true);
    try {
      // result 是批量预检接口返回的行级校验结果。
      const result = await previewItemPublishBatch({
        file: batchFile,
        imagesZip: batchImagesZip,
        defaultCookieId: options.selectedAccount,
        fallbackCategory: batchFallbackCategory,
        location: batchLocation || undefined,
      });
      setBatchPreview(result);
      setBatchDetail(null);
      setBatchPhase('preview');
    } catch (error: any /* 预检失败错误 */) {
      console.error('批量铺货预检失败:', error);
      alert(error?.message || '预检失败，请检查表格和图片 zip');
    } finally {
      setBatchLoading(false);
    }
    },
    [batchFallbackCategory, batchFile, batchImagesZip, batchLocation, options.selectedAccount],
  );

  // handleStartBatch 启动预检通过的批量任务并读取首个详情。
  const handleStartBatch = useCallback(
    // 批量启动动作只允许预检通过的有效行进入执行阶段。
    async () => {
    if (!batchPreview?.preview_id) return;
    if (!canStartBatch(batchPreview)) {
      alert('没有可发布的商品行');
      return;
    }
    setBatchLoading(true);
    try {
      // started 是批量任务启动接口返回的任务标识。
      const started = await startItemPublishBatch(batchPreview.preview_id);
      // detail 是启动后用于驱动轮询的任务详情。
      const detail = await getItemPublishBatch(started.batch_id || batchPreview.preview_id);
      setBatchDetail(detail);
      setRecentBatch(detail);
      setBatchPhase(detail.status === 'running' ? 'running' : 'done');
    } catch (error: any /* 启动失败错误 */) {
      console.error('启动批量铺货失败:', error);
      alert(error?.message || '启动发布任务失败');
    } finally {
      setBatchLoading(false);
    }
    },
    [batchPreview],
  );

  // handleCancelBatch 请求安全取消当前批量任务，并保留远端收尾状态。
  const handleCancelBatch = useCallback(
    // 批量取消动作遵循后端的安全取消语义。
    async () => {
    if (!batchDetail?.id) return;
    if (!confirm('确认取消当前批量铺货任务吗？正在发布的单个商品可能会继续完成。')) return;
    setBatchLoading(true);
    try {
      // result 是取消请求返回的过渡状态。
      const result = await cancelItemPublishBatch(batchDetail.id);
      // detail 是取消请求后的最新任务状态。
      const detail = await getItemPublishBatch(batchDetail.id);
      setBatchDetail(detail);
      setBatchPhase(result?.status === 'canceling' || detail.status === 'canceling' ? 'running' : 'done');
    } catch (error: any /* 取消失败错误 */) {
      alert(error?.message || '取消失败');
    } finally {
      setBatchLoading(false);
    }
    },
    [batchDetail?.id],
  );

  // abandonBatchPreview 删除未启动的预检任务并恢复上传步骤。
  const abandonBatchPreview = useCallback(
    // 预检放弃动作删除未启动任务并恢复上传步骤。
    async () => {
    // previewId 是当前临时预检任务标识。
    const previewId = batchPreview?.preview_id;
    if (previewId && batchPhase === 'preview') {
      try {
        await deleteItemPublishBatch(previewId);
      } catch (error /* 删除预检错误 */) {
        console.error('清理批量铺货预检失败:', error);
      }
    }
    setBatchPreview(null);
    setBatchPhase('upload');
    },
    [batchPhase, batchPreview?.preview_id],
  );

  // closeBatchModal 清理临时预检后关闭批量弹窗。
  const closeBatchModal = useCallback(
    // 批量弹窗关闭动作先清理临时预检任务。
    async () => {
    await abandonBatchPreview();
    setShowBatchModal(false);
    },
    [abandonBatchPreview],
  );

  // handleRetryBatchFailed 重新执行当前批次中可重试的失败行。
  const handleRetryBatchFailed = useCallback(
    // 失败重试动作只提交后端允许重试的行。
    async () => {
    if (!batchDetail?.id || !canRetryBatch(batchDetail)) return;
    setBatchLoading(true);
    try {
      await retryFailedItemPublishBatch(batchDetail.id);
      setBatchDetail(await getItemPublishBatch(batchDetail.id));
      setBatchPhase('running');
    } catch (error: any /* 重试失败错误 */) {
      alert(error?.message || '重试失败');
    } finally {
      setBatchLoading(false);
    }
    },
    [batchDetail?.id],
  );

  useEffect(
    // 批量轮询副作用只在弹窗展示运行任务时启动。
    () => {
      if (!showBatchModal || !batchDetail?.id || !isBatchInProgress(batchDetail.status)) return;
      // requestGeneration 标记本次轮询生命周期，弹窗关闭时会失效。
      const requestGeneration = ++batchRequestGeneration.current;
      // pollBatch 读取任务最新进度并在结束后刷新商品和规则列表。
      const pollBatch = async () => {
        if (batchPollInFlight.current) return;
        batchPollInFlight.current = true;
        try {
          // detail 是轮询返回的最新任务详情。
          const detail = await getItemPublishBatch(batchDetail.id);
          if (!isCurrentBatchRequest(requestGeneration, batchRequestGeneration.current)) return;
          setBatchDetail(detail);
          setRecentBatch(detail);
          if (!isBatchInProgress(detail.status)) {
            setBatchPhase('done');
            await Promise.all([options.loadItems(), options.loadShippingRules()]);
          }
        } catch (error /* 轮询读取错误 */) {
          console.error('刷新批量铺货进度失败:', error);
        } finally {
          batchPollInFlight.current = false;
        }
      };
      // timer 是当前批次的轮询计时器。
      const timer = window.setInterval(
        // 轮询回调异步读取任务进度。
        () => void pollBatch(),
        3000,
      );
      return (
        // 轮询清理器停止计时器并使未完成响应失效。
        () => {
        window.clearInterval(timer);
        batchRequestGeneration.current += 1;
        }
      );
    },
    [batchDetail?.id, batchDetail?.status, options.loadItems, options.loadShippingRules, showBatchModal],
  );

  // result 汇总批量状态、更新器和流程动作，保持页面只消费 feature 边界。
  const result: ItemPublishBatchState = {
    showBatchModal,
    setShowBatchModal,
    batchLoading,
    batchPhase,
    setBatchPhase,
    batchFile,
    setBatchFile,
    batchImagesZip,
    setBatchImagesZip,
    batchCategoryKeyword,
    setBatchCategoryKeyword,
    batchCategoryLoading,
    batchFallbackCategory,
    setBatchFallbackCategory,
    batchPreview,
    setBatchPreview,
    batchDetail,
    setBatchDetail,
    recentBatch,
    setRecentBatch,
    batchLocations,
    batchLocation,
    setBatchLocations,
    setBatchLocation,
    openBatchModal,
    handleRecommendBatchCategory,
    openRecentBatchResult,
    handlePreviewBatch,
    handleStartBatch,
    handleCancelBatch,
    abandonBatchPreview,
    closeBatchModal,
    handleRetryBatchFailed,
  };
  return result;
};
