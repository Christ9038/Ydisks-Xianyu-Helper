// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { Card } from '../../../shared/api-contract/cards';
import { appendCardData,batchCreateCards,getCards } from './api';
import { useCardBatchActions,useCardsData } from './hooks';

vi.mock('./api', /* cardsApiMockFactory 提供卡密 Hook 的确定性 API 替身。 */ () => ({
  appendCardData: vi.fn(),
  batchCreateCards: vi.fn(),
  getCards: vi.fn(),
}));

// appendCardMock 是卡密追加请求的可控替身。
const appendCardMock = vi.mocked(appendCardData);
// batchCreateMock 是卡密批量创建请求的可控替身。
const batchCreateMock = vi.mocked(batchCreateCards);
// getCardsMock 是卡密库存加载请求的可控替身。
const getCardsMock = vi.mocked(getCards);

// cardFixture 是 data 类型卡密组的完整测试对象。
const cardFixture: Card = { id: 1, name: '库存一', type: 'data', enabled: true, data_content: 'old', created_at: '2026-01-01', updated_at: '2026-01-01' };
// loadCardsFixture 是批量操作完成后的库存刷新替身。
const loadCardsFixture = vi.fn().mockResolvedValue(undefined);

describe('useCardsData 与 useCardBatchActions', /* 当前回调处理卡密库存和批量操作 Hook。 */ () => {
  beforeEach(/* 当前回调重置卡密 API 替身和日志输出。 */ () => {
    vi.clearAllMocks();
    getCardsMock.mockResolvedValue([cardFixture]);
    appendCardMock.mockResolvedValue({ success: true, added: 2 });
    batchCreateMock.mockResolvedValue({ success: true, total: 1, created: 1, failed: 0, rows: [{ row_no: 1, success: true, id: 2, name: '新库存' }] });
    vi.spyOn(console, 'error').mockImplementation(
      // errorImplementation 屏蔽卡密加载失败测试中的日志输出。
      () => undefined,
    );
  });

  test('库存加载成功并支持批量追加和创建', /* 当前回调验证卡密 Hook 的成功路径。 */ async () => {
    // dataHook 是卡密库存 Hook 的渲染结果。
    const dataHook = renderHook(
      // cardsHookFactory 创建卡密库存 Hook。
      () => useCardsData(),
    );
    await waitFor(
      // loadingAssertion 等待卡密库存加载完成。
      () => expect(dataHook.result.current.loading).toBe(false),
    );
    expect(dataHook.result.current.cards).toEqual([cardFixture]);
    dataHook.unmount();

    // batchHook 是批量操作 Hook 的渲染结果。
    const batchHook = renderHook(
      // batchHookFactory 创建批量操作 Hook。
      () => useCardBatchActions({ dataCards: [cardFixture], loadCards: loadCardsFixture }),
    );
    await act(
      // openAction 打开批量操作弹窗。
      () => batchHook.result.current.openBatchModal(),
    );
    expect(batchHook.result.current.showBatchModal).toBe(true);
    expect(batchHook.result.current.appendTargetId).toBe('1');
    await act(
      // appendContentAction 写入待追加的卡密文本。
      () => batchHook.result.current.setAppendContent(' line-1\n\nline-2 '),
    );
    expect(batchHook.result.current.appendPreview).toEqual(['line-1', 'line-2']);
    await act(
      // appendAction 提交卡密追加请求。
      async () => batchHook.result.current.handleBatchAppend(),
    );
    expect(appendCardMock).toHaveBeenCalledWith('1', ' line-1\n\nline-2 ', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(batchHook.result.current.appendResult).toEqual({ added: 2 });
    await act(
      // batchFileAction 写入批量创建文件。
      () => batchHook.result.current.setBatchFile(new File(['name'], 'cards.csv')),
    );
    await act(
      // createAction 提交批量创建请求。
      async () => batchHook.result.current.handleBatchCreate(),
    );
    expect(batchCreateMock).toHaveBeenCalledWith(expect.any(File), expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(batchHook.result.current.batchResult).toMatchObject({ created: 1 });
    await act(
      // createRetryAction 重试当前批量创建文件。
      async () => batchHook.result.current.handleRetryBatchCreate(),
    );
    expect(batchCreateMock).toHaveBeenCalledTimes(2);
    await act(
      // closeAction 关闭批量操作弹窗并取消当前请求。
      () => batchHook.result.current.closeBatchModal(),
    );
    expect(batchHook.result.current.showBatchModal).toBe(false);
    batchHook.unmount();
  });

  test('库存加载和追加失败时保留可见错误', /* 当前回调验证卡密 Hook 的失败路径。 */ async () => {
    getCardsMock.mockRejectedValueOnce(new Error('库存读取失败'));
    // dataHook 是库存加载失败场景的 Hook 渲染结果。
    const dataHook = renderHook(
      // failedCardsHookFactory 创建库存加载失败场景的 Hook。
      () => useCardsData(),
    );
    await waitFor(
      // loadingAssertion 等待库存失败请求收束。
      () => expect(dataHook.result.current.loading).toBe(false),
    );
    expect(dataHook.result.current.cards).toEqual([]);
    dataHook.unmount();

    appendCardMock.mockRejectedValueOnce(new Error('追加失败'));
    // batchHook 是追加失败场景的批量操作 Hook 渲染结果。
    const batchHook = renderHook(
      // failedBatchHookFactory 创建追加失败场景的批量 Hook。
      () => useCardBatchActions({ dataCards: [cardFixture], loadCards: loadCardsFixture }),
    );
    await act(
      // targetAction 设置追加目标卡密组。
      () => batchHook.result.current.setAppendTargetId('1'),
    );
    await act(
      // contentAction 写入追加失败测试内容。
      () => batchHook.result.current.setAppendContent('line-1'),
    );
    await act(
      // failedAppendAction 提交会失败的追加请求。
      async () => batchHook.result.current.handleBatchAppend(),
    );
    expect(batchHook.result.current.appendError).toBe('追加失败');
    expect(batchHook.result.current.batchBusy).toBe(false);

    batchCreateMock.mockRejectedValueOnce(new Error('创建失败'));
    await act(
      // fileAction 写入批量创建失败测试文件。
      () => batchHook.result.current.setBatchFile(new File(['name'], 'cards.csv')),
    );
    await act(
      // createErrorAction 提交失败的批量创建请求。
      async () => batchHook.result.current.handleBatchCreate(),
    );
    expect(batchHook.result.current.batchResult).toEqual({ error: '创建失败' });

    appendCardMock.mockResolvedValueOnce({ success: true, added: 1 });
    await act(
      // appendRetryAction 重试当前卡密追加。
      async () => batchHook.result.current.handleRetryBatchAppend(),
    );
    expect(appendCardMock).toHaveBeenCalledTimes(2);

    await act(
      // targetSwitchAction 切换追加目标以阻止旧目标重试。
      () => batchHook.result.current.setAppendTargetId('2'),
    );
    await act(
      // staleRetryAction 验证切换目标后不再提交旧目标请求。
      async () => batchHook.result.current.handleRetryBatchAppend(),
    );
    expect(appendCardMock).toHaveBeenCalledTimes(2);
    batchHook.unmount();
  });

  test('批量创建和追加的前置条件阻止空操作', /* 当前回调验证卡密批量操作的空输入守卫。 */ async () => {
    // hook 是空库存和空输入守卫场景的批量 Hook 渲染结果。
    const hook = renderHook(
      // guardHookFactory 创建批量操作守卫场景的 Hook。
      () => useCardBatchActions({ dataCards: [], loadCards: loadCardsFixture }),
    );
    await act(
      // emptyCreateAction 在没有文件时阻止批量创建。
      async () => hook.result.current.handleBatchCreate(),
    );
    expect(batchCreateMock).not.toHaveBeenCalled();
    await act(
      // emptyAppendAction 在没有目标和内容时阻止卡密追加。
      async () => hook.result.current.handleBatchAppend(),
    );
    expect(appendCardMock).not.toHaveBeenCalled();
    await act(
      // emptyRetryAction 在没有上次目标时阻止追加重试。
      async () => hook.result.current.handleRetryBatchAppend(),
    );
    expect(appendCardMock).not.toHaveBeenCalled();
    await act(
      // openAction 打开空库存批量弹窗并验证目标为空。
      () => hook.result.current.openBatchModal(),
    );
    expect(hook.result.current.appendTargetId).toBe('');
    hook.unmount();
  });

  test('库存刷新时丢弃先发出的旧响应', /* 当前回调验证卡密库存请求代次隔离。 */ async () => {
    // resolveFirst 是旧库存请求的完成控制器。
    let resolveFirst: (value: Card[]) => void = () => undefined;
    // firstRequest 是保持未完成的旧库存请求 Promise。
    const firstRequest = new Promise<Card[]>(/* firstExecutor 保存旧请求完成函数。 */ resolve => { resolveFirst = resolve; });
    getCardsMock.mockReset();
    getCardsMock.mockReturnValueOnce(firstRequest);
    getCardsMock.mockResolvedValueOnce([]);
    // hook 是库存刷新竞态场景的 Hook 渲染结果。
    const hook = renderHook(
      // staleCardsHookFactory 创建库存旧响应场景的 Hook。
      () => useCardsData(),
    );
    await act(
      // refreshAction 发起第二次库存刷新并使首次请求过期。
      async () => hook.result.current.loadCards(),
    );
    resolveFirst([cardFixture]);
    await act(
      // staleResolveAction 完成已过期的首次库存响应。
      async () => { await firstRequest; },
    );
    expect(hook.result.current.cards).toEqual([]);
    hook.unmount();
  });

  test('批量创建在卸载时取消未完成请求', /* 当前回调验证批量表单卸载后不再允许晚到上传响应更新状态。 */ async () => {
    // uploadSignal 保存批量创建接口收到的取消信号。
    let uploadSignal: AbortSignal | undefined;
    batchCreateMock.mockImplementation(
      // pendingCreate 保持上传请求未完成，以便验证 Hook 卸载时主动取消。
      (_file, requestOptions) => {
        uploadSignal = requestOptions?.signal;
        return new Promise(/* pendingCreateExecutor 故意不完成上传 Promise，直到 Hook 卸载取消请求。 */ () => undefined);
      },
    );
    // hook 是带有上传文件的批量创建 Hook。
    const hook = renderHook(
      // batchHookFactory 创建批量操作 Hook。
      () => useCardBatchActions({ dataCards: [cardFixture], loadCards: loadCardsFixture }),
    );
    await act(
      // fileAction 写入将要上传的卡密文件。
      () => hook.result.current.setBatchFile(new File(['name'], 'cards.csv')),
    );
    await act(
      // createAction 发起尚未完成的上传但不等待其结束。
      () => { void hook.result.current.handleBatchCreate(); },
    );
    await waitFor(
      // uploadStartedAssertion 等待上传请求拿到取消信号。
      () => expect(uploadSignal).toBeDefined(),
    );
    hook.unmount();
    expect(uploadSignal?.aborted).toBe(true);
  });
});
