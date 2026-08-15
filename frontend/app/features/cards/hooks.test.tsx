// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import type { Card } from '../../../types';
import { appendCardData, batchCreateCards, getCards } from './api';
import { useCardBatchActions, useCardsData } from './hooks';

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
  });
});
