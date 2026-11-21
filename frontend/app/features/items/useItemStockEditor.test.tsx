// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { Item,ItemStock } from './api';
import { getItemStock,updateItemStock } from './api';
import { useItemStockEditor } from './useItemStockEditor';

vi.mock('./api', /* stockApiMockFactory 提供库存编辑 Hook 的确定性 API 替身。 */ () => ({
  getItemStock: vi.fn(),
  updateItemStock: vi.fn(),
  isItemStockChangedError: /* changedMock 以错误消息标记识别库存已变化。 */ (error: unknown) => error instanceof Error && error.message === 'changed',
  itemErrorMessage: /* errorMessageMock 把异常转换为稳定文本。 */ (error: unknown, fallback: string) => (error instanceof Error ? error.message : fallback),
}));

// getMock、updateMock 是读取与提交接口的可控替身。
const getMock = vi.mocked(getItemStock);
const updateMock = vi.mocked(updateItemStock);
// itemA、itemB 是两件不同商品样本，用于切换场景。
const itemA = { cookie_id: 'acc', item_id: 'a' } as Item;
const itemB = { cookie_id: 'acc', item_id: 'b' } as Item;
// stockOf 构造指定商品的单规格库存。
const stockOf = (/* itemId 是商品标识。 */ itemId: string, /* quantity 是当前库存。 */ quantity: number): ItemStock => ({ itemId, multiSpec: false, quantity, skus: [] });
// multiStock 是两行规格的多规格库存样本，总库存 7。
const multiStock: ItemStock = { itemId: 'a', multiSpec: true, quantity: 7, skus: [{ key: 'k1', label: '红', priceCents: 1, quantity: 3 }, { key: 'k2', label: '蓝', priceCents: 1, quantity: 4 }] };

// mountEditor 渲染打开状态的库存编辑 Hook 并返回渲染句柄。
const mountEditor = (/* item 是被编辑的商品。 */ item: Item, /* onClose 是关闭回调。 */ onClose = vi.fn()) => renderHook(/* editorHook 调用被测 Hook。 */ () => useItemStockEditor({ item, open: true, onClose }));
// EditorHandle 是 mountEditor 返回的句柄类型。
type EditorHandle = ReturnType<typeof mountEditor>;
// loaded 等待库存读取完成。
const loaded = (/* handle 是 Hook 渲染句柄。 */ handle: EditorHandle) => waitFor(/* stockReady 断言库存快照已就绪。 */ () => expect(handle.result.current.stock).not.toBeNull());
// saveNow 在 act 中触发一次保存。
const saveNow = (/* handle 是 Hook 渲染句柄。 */ handle: EditorHandle) => act(/* saveAct 等待保存完成。 */ async () => { await handle.result.current.save(); });

describe('useItemStockEditor', /* editorSuite 覆盖读取、提交、取消、切换与迟到响应。 */ () => {
  beforeEach(/* resetMocks 重置替身。 */ () => { vi.resetAllMocks(); });

  test('打开时读取一次并以当前库存初始化草稿', /* loadCase 验证初始状态。 */ async () => {
    getMock.mockResolvedValue(stockOf('a', 5));
    // handle 是被测状态机的渲染句柄。
    const handle = mountEditor(itemA);
    await loaded(handle);
    expect(handle.result.current.singleInput).toBe('5');
    expect(getMock).toHaveBeenCalledTimes(1);
  });

  test('未打开时不发起请求', /* closedCase 验证列表加载不触发平台请求。 */ () => {
    renderHook(/* closedHook 以关闭状态调用 Hook。 */ () => useItemStockEditor({ item: itemA, open: false, onClose: vi.fn() }));
    expect(getMock).not.toHaveBeenCalled();
  });

  test('读取失败时禁止提交并可重新加载', /* loadFailCase 验证失败与重试。 */ async () => {
    getMock.mockRejectedValueOnce(new Error('boom')).mockResolvedValueOnce(stockOf('a', 2));
    // handle 是被测状态机的渲染句柄。
    const handle = mountEditor(itemA);
    await waitFor(/* failedReady 断言读取失败状态。 */ () => expect(handle.result.current.loadFailed).toBe(true));
    expect(handle.result.current.errorMessage).toBe('boom');
    await saveNow(handle);
    expect(updateMock).not.toHaveBeenCalled();
    act(/* reloadAct 触发重新加载。 */ () => handle.result.current.reload());
    await waitFor(/* reloaded 断言重读成功。 */ () => expect(handle.result.current.stock?.quantity).toBe(2));
    expect(handle.result.current.loadFailed).toBe(false);
  });

  test('提交成功后以新库存作为后续原库存', /* saveCase 验证带原库存提交并更新快照。 */ async () => {
    getMock.mockResolvedValue(stockOf('a', 5));
    updateMock.mockResolvedValue(undefined);
    // handle 是被测状态机的渲染句柄。
    const handle = mountEditor(itemA);
    await loaded(handle);
    act(/* typeAct 写入新库存草稿。 */ () => handle.result.current.setSingleInput('9'));
    await saveNow(handle);
    expect(updateMock).toHaveBeenCalledWith('acc', 'a', { quantity: 9, expectedQuantity: 5 }, expect.anything());
    expect(handle.result.current.stock?.quantity).toBe(9);
    expect(handle.result.current.successMessage).toBe('库存已更新。');
  });

  test('本地校验失败不发请求', /* invalidCase 验证非法输入被拦截。 */ async () => {
    getMock.mockResolvedValue(stockOf('a', 5));
    // handle 是被测状态机的渲染句柄。
    const handle = mountEditor(itemA);
    await loaded(handle);
    act(/* typeAct 写入非法草稿。 */ () => handle.result.current.setSingleInput('abc'));
    await saveNow(handle);
    expect(updateMock).not.toHaveBeenCalled();
    expect(handle.result.current.errorMessage).toContain('1 到');
  });

  test('库存已变化时要求重新读取并阻止再次提交', /* conflictCase 验证 409 处理。 */ async () => {
    getMock.mockResolvedValue(stockOf('a', 5));
    updateMock.mockRejectedValue(new Error('changed'));
    // handle 是被测状态机的渲染句柄。
    const handle = mountEditor(itemA);
    await loaded(handle);
    act(/* typeAct 写入新库存草稿。 */ () => handle.result.current.setSingleInput('9'));
    await saveNow(handle);
    expect(handle.result.current.needsReload).toBe(true);
    await saveNow(handle);
    expect(updateMock).toHaveBeenCalledTimes(1);
  });

  test('提交失败保留草稿且可再次提交', /* failCase 验证普通失败。 */ async () => {
    getMock.mockResolvedValue(stockOf('a', 5));
    updateMock.mockRejectedValue(new Error('平台失败'));
    // handle 是被测状态机的渲染句柄。
    const handle = mountEditor(itemA);
    await loaded(handle);
    act(/* typeAct 写入新库存草稿。 */ () => handle.result.current.setSingleInput('9'));
    await saveNow(handle);
    expect(handle.result.current.errorMessage).toBe('平台失败');
    expect(handle.result.current.singleInput).toBe('9');
    expect(handle.result.current.saving).toBe(false);
  });

  test('多规格草稿按行更新并整体提交', /* multiCase 验证 setSkuInput。 */ async () => {
    getMock.mockResolvedValue(multiStock);
    updateMock.mockResolvedValue(undefined);
    // handle 是被测状态机的渲染句柄。
    const handle = mountEditor(itemA);
    await loaded(handle);
    act(/* typeAct 修改红色规格库存草稿。 */ () => handle.result.current.setSkuInput('k1', '6'));
    await saveNow(handle);
    expect(updateMock).toHaveBeenCalledWith('acc', 'a', { skus: [{ key: 'k1', quantity: 6, expectedQuantity: 3 }, { key: 'k2', quantity: 4, expectedQuantity: 4 }] }, expect.anything());
    expect(handle.result.current.stock?.quantity).toBe(10);
  });

  test('切换商品后丢弃旧商品的迟到响应', /* staleCase 验证代次保护。 */ async () => {
    // resolveA 用于手动完成商品 A 的读取。
    let resolveA: (value: ItemStock) => void = /* noopResolve 是读取发起前的占位回调。 */ () => undefined;
    getMock.mockImplementationOnce(/* pendingRead 返回由测试手动完成的读取。 */ () => new Promise<ItemStock>(/* pendingExecutor 保存 resolve。 */ resolve => { resolveA = resolve; })).mockResolvedValueOnce(stockOf('b', 8));
    // handle 是带可切换商品属性的渲染句柄。
    const handle = renderHook(/* switchHook 以当前商品调用 Hook。 */ (props: { /** item 是当前编辑的商品。 */ item: Item }) => useItemStockEditor({ item: props.item, open: true, onClose: vi.fn() }), { initialProps: { item: itemA } });
    handle.rerender({ item: itemB });
    await waitFor(/* switched 断言已读到商品 B。 */ () => expect(handle.result.current.stock?.itemId).toBe('b'));
    await act(/* lateAct 让旧商品的读取迟到完成。 */ async () => { resolveA(stockOf('a', 1)); });
    expect(handle.result.current.stock?.itemId).toBe('b');
    expect(handle.result.current.singleInput).toBe('8');
  });

  test('关闭会取消在途读取并调用 onClose', /* closeCase 验证取消与回调。 */ () => {
    // signalSeen 保存读取请求收到的取消信号。
    let signalSeen: AbortSignal | undefined;
    getMock.mockImplementation(/* hangingRead 记录取消信号并永不完成。 */ (_cookieId, _itemId, options) => { signalSeen = options?.signal; return new Promise<ItemStock>(/* neverExecutor 永不 resolve。 */ () => undefined); });
    // onClose 是关闭回调。
    const onClose = vi.fn();
    // handle 是被测状态机的渲染句柄。
    const handle = mountEditor(itemA, onClose);
    act(/* closeAct 触发关闭。 */ () => handle.result.current.close());
    expect(signalSeen?.aborted).toBe(true);
    expect(onClose).toHaveBeenCalled();
    expect(handle.result.current.loading).toBe(false);
  });
});
