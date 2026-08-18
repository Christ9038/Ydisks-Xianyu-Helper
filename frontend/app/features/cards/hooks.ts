import { useCallback,useEffect,useMemo,useRef,useState,type Dispatch,type SetStateAction } from 'react';
import type { Card } from './api';
import { appendCardData,batchCreateCards,getCards } from './api';
import { canSubmitAppend,isCurrentCardRequest,previewAppendContent } from './batchState';
import type { CardBatchState } from './types';

// CardsDataResult 暴露卡密库存、加载状态和可复用刷新动作。
export interface CardsDataResult {
  // cards 是当前库存中的全部卡密组。
  cards: Card[];
  // setCards 允许页面在编辑、删除和批量操作后局部更新库存。
  setCards: Dispatch<SetStateAction<Card[]>>;
  // loading 表示卡密库存是否正在加载。
  loading: boolean;
  // loadCards 刷新卡密库存并丢弃旧请求响应。
  loadCards: () => Promise<void>;
}

// CardBatchOptions 描述批量操作 Hook 所需的库存数据和刷新回调。
export interface CardBatchOptions {
  // dataCards 是可用于追加库存的 data 类型卡密组。
  dataCards: Card[];
  // loadCards 在批量操作完成后刷新库存。
  loadCards: () => Promise<void>;
}

// useCardsData 统一管理卡密库存首次加载、刷新、取消和请求代次。
export const useCardsData = (): CardsDataResult => {
  // cards 保存当前卡密组列表。
  const [cards, setCards] = useState<Card[]>([]);
  // loading 表示库存加载状态。
  const [loading, setLoading] = useState(true);
  // loadGeneration 用于隔离连续刷新产生的旧响应。
  const loadGeneration = useRef(0);
  // loadAbort 保存当前库存请求的取消控制器。
  const loadAbort = useRef<AbortController | null>(null);

  // loadCards 并行无依赖请求只有一个，但通过代次和 AbortController 保证刷新安全。
  const loadCards = useCallback(
    // 货架加载回调读取最新卡密库存。
    async () => {
      // generation 标记本次库存加载请求。
      const generation = ++loadGeneration.current;
      loadAbort.current?.abort();
      // controller 允许新刷新取消旧的网络请求。
      const controller = new AbortController();
      loadAbort.current = controller;
      setLoading(true);
      try {
        // result 是当前请求返回的规范化卡密列表。
        const result = await getCards({ signal: controller.signal });
        if (generation !== loadGeneration.current) return;
        setCards(result);
      } catch (error /* 卡密库存加载错误 */) {
        if (generation === loadGeneration.current && !controller.signal.aborted) {
          console.error('加载卡密库存失败:', error);
        }
      } finally {
        if (generation === loadGeneration.current) setLoading(false);
      }
    },
    [],
  );

  useEffect(
    // 库存副作用负责首次加载，并在组件卸载时取消请求。
    () => {
      void loadCards();
      // cleanup 在库存页面卸载时终止当前请求。
      const cleanup = () => {
        loadGeneration.current += 1;
        loadAbort.current?.abort();
      };
      return cleanup;
    },
    [loadCards],
  );

  return { cards, setCards, loading, loadCards };
};

// useCardBatchActions 统一管理批量创建、追加、取消和失败重试状态。
export const useCardBatchActions = (options: CardBatchOptions): CardBatchState => {
  // dataCards 是当前可用于追加库存的 data 类型卡密组。
  const dataCards = options.dataCards;
  // loadCards 是批量操作完成后的库存刷新动作。
  const loadCards = options.loadCards;
  // showBatchModal 表示批量导入弹窗是否打开。
  const [showBatchModal, setShowBatchModal] = useState(false);
  // batchTab 保存当前批量操作页签。
  const [batchTab, setBatchTab] = useState<'create' | 'append'>('create');
  // batchFile 保存最近一次选择的批量文件。
  const [batchFile, setBatchFile] = useState<File | null>(null);
  // batchResult 保存批量创建成功或失败结果。
  const [batchResult, setBatchResult] = useState<CardBatchState['batchResult']>(null);
  // batchBusy 表示批量请求正在执行。
  const [batchBusy, setBatchBusy] = useState(false);
  // appendTargetId 保存当前追加目标卡密组。
  const [appendTargetId, setAppendTargetId] = useState('');
  // appendContent 保存待追加的原始卡密文本。
  const [appendContent, setAppendContent] = useState('');
  // appendResult 保存追加成功数量。
  const [appendResult, setAppendResult] = useState<CardBatchState['appendResult']>(null);
  // appendError 保存追加失败说明。
  const [appendError, setAppendError] = useState('');
  // requestGeneration 让关闭弹窗或切换目标后旧响应失效。
  const requestGeneration = useRef(0);
  // requestAbort 取消当前批量创建或追加请求。
  const requestAbort = useRef<AbortController | null>(null);
  // lastAppendTarget 保存最近一次提交追加的目标 ID，供失败重试使用。
  const lastAppendTarget = useRef('');

  useEffect(
    // 批量操作生命周期副作用在弹窗页面卸载时使未完成请求失效，防止晚到响应写入已销毁的表单状态。
    () => (
      // batchRequestCleanup 取消创建或追加请求，并推进代次屏蔽不响应 AbortSignal 的旧响应。
      () => {
        requestGeneration.current += 1;
        requestAbort.current?.abort();
      }
    ),
    [],
  );

  // appendPreview 按行派生追加预览，不重复存储派生状态。
  const appendPreview = useMemo(
    // 追加预览回调根据当前文本派生有效卡密行。
    () => previewAppendContent(appendContent),
    [appendContent],
  );

  // openBatchModal 重置批量表单，并默认选择第一个 data 卡密组。
  const openBatchModal = useCallback(
    // 批量弹窗打开回调重置上一次操作的短暂状态。
    () => {
    setBatchTab('create');
    setBatchFile(null);
    setBatchResult(null);
    setAppendTargetId(dataCards[0]?.id ? String(dataCards[0].id) : '');
    setAppendContent('');
    setAppendResult(null);
    setAppendError('');
    setShowBatchModal(true);
    },
    [dataCards],
  );

  // closeBatchModal 使当前请求失效并关闭批量操作弹窗。
  const closeBatchModal = useCallback(
    // 批量弹窗关闭回调取消网络请求并使旧响应失效。
    () => {
    requestGeneration.current += 1;
    requestAbort.current?.abort();
    setBatchBusy(false);
    setShowBatchModal(false);
    },
    [],
  );

  // handleBatchCreate 执行批量创建并在成功后刷新库存。
  const handleBatchCreate = useCallback(
    // 批量创建回调上传文件并刷新库存。
    async () => {
    if (!batchFile || batchBusy) return;
    // generation 标记当前批量创建请求的代次。
    const generation = ++requestGeneration.current;
    requestAbort.current?.abort();
    // controller 取消上一轮批量创建请求。
    const controller = new AbortController();
    requestAbort.current = controller;
    setBatchBusy(true);
    setBatchResult(null);
    try {
      // result 是批量创建接口返回的逐行统计结果。
      const result = await batchCreateCards(batchFile, { signal: controller.signal });
      if (!isCurrentCardRequest(generation, requestGeneration.current, '', '')) return;
      setBatchResult(result);
      if (result.created > 0) await loadCards();
    } catch (error: any /* 批量创建错误 */) {
      if (!isCurrentCardRequest(generation, requestGeneration.current, '', '')) return;
      setBatchResult({ error: error?.message || '上传失败' });
    } finally {
      if (isCurrentCardRequest(generation, requestGeneration.current, '', '')) setBatchBusy(false);
    }
    },
    [batchBusy, batchFile, loadCards],
  );

  // handleBatchAppend 预览并提交当前卡密组的追加内容。
  const handleBatchAppend = useCallback(
    // 批量追加回调提交当前目标卡密组的有效行。
    async () => {
    if (!canSubmitAppend(appendTargetId, appendContent, batchBusy)) return;
    // targetId 保存提交时的目标卡密组，防止切换目标后误写状态。
    const targetId = appendTargetId;
    // generation 标记当前追加请求的代次。
    const generation = ++requestGeneration.current;
    lastAppendTarget.current = targetId;
    requestAbort.current?.abort();
    // controller 取消上一轮追加库存请求。
    const controller = new AbortController();
    requestAbort.current = controller;
    setBatchBusy(true);
    setAppendResult(null);
    setAppendError('');
    try {
      // result 是当前卡密组追加成功的数量。
      const result = await appendCardData(targetId, appendContent, { signal: controller.signal });
      if (!isCurrentCardRequest(generation, requestGeneration.current, targetId, appendTargetId)) return;
      setAppendResult({ added: result.added });
      setAppendContent('');
      await loadCards();
    } catch (error: any /* 追加库存错误 */) {
      if (!isCurrentCardRequest(generation, requestGeneration.current, targetId, appendTargetId)) return;
      setAppendError(error?.message || '追加失败');
    } finally {
      if (isCurrentCardRequest(generation, requestGeneration.current, targetId, appendTargetId)) setBatchBusy(false);
    }
    },
    [appendContent, appendTargetId, batchBusy, loadCards],
  );

  // handleRetryBatchCreate 重试仍然保留在表单中的批量文件。
  const handleRetryBatchCreate = useCallback(
    // 批量创建重试回调复用当前文件。
    async () => {
    await handleBatchCreate();
    },
    [handleBatchCreate],
  );

  // handleRetryBatchAppend 重试最近一次追加目标的当前内容。
  const handleRetryBatchAppend = useCallback(
    // 追加重试回调只允许重试仍处于当前目标的请求。
    async () => {
    if (lastAppendTarget.current && appendTargetId !== lastAppendTarget.current) return;
    await handleBatchAppend();
    },
    [appendTargetId, handleBatchAppend],
  );

  return {
    showBatchModal,
    setShowBatchModal,
    batchTab,
    setBatchTab,
    batchFile,
    setBatchFile,
    batchResult,
    batchBusy,
    appendTargetId,
    setAppendTargetId,
    appendContent,
    setAppendContent,
    appendResult,
    appendError,
    appendPreview,
    openBatchModal,
    closeBatchModal,
    handleBatchCreate,
    handleBatchAppend,
    handleRetryBatchCreate,
    handleRetryBatchAppend,
  };
};
