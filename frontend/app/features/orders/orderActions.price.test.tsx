// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Order } from './api';
import { adjustOrderPrice } from './api';
import { useOrderActions } from './orderActions';

vi.mock('./api', /* orderApiMockFactory 提供订单动作 API 替身，仅改价会被调用。 */ () => ({
  adjustOrderPrice: vi.fn(),
  deleteOrder: vi.fn(),
  manualShipOrder: vi.fn(),
  syncOrders: vi.fn(),
  syncSingleOrder: vi.fn(),
  updateOrder: vi.fn(),
}));

// deferred 创建可由测试手动完成的 Promise，用于控制改价响应的到达时机。
const deferred = <T,>() => {
  // resolve、reject 是外部放行入口。
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  // promise 是被控制的异步结果。
  const promise = new Promise<T>(/* executor 捕获放行入口。 */ (res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
};

// orderFixture 是未付款订单夹具。
const orderFixture: Order = {
  id: 'row-1', order_id: 'order-1', cookie_id: 'account-1', item_id: 'item-1', item_title: '商品',
  buyer_id: 'buyer-1', quantity: 1, amount: '10.00', status: 'processing',
};

// adjustResult 是改价接口成功的归一结果夹具。
const adjustResult = { orderId: 'order-1', amount: '8.80', status: 'succeeded' as const, message: '已修改订单价格为 ¥8.80' };

// createHook 创建订单动作 Hook，并返回列表刷新替身。
const createHook = () => {
  // loadOrders 记录列表刷新次数。
  const loadOrders = vi.fn().mockResolvedValue(undefined);
  // hook 是被测订单动作 Hook。
  const hook = renderHook(/* 创建订单动作 Hook。 */ () => useOrderActions({ orders: [orderFixture], page: 1, accountFilter: '', filter: 'all', setPage: vi.fn(), loadOrders }));
  return { hook, loadOrders };
};

describe('useOrderActions 订单改价', /* 验证改价弹窗草稿、校验、成功、失败、重复提交和过期结果。 */ () => {
  beforeEach(/* 重置改价替身。 */ () => { vi.clearAllMocks(); });
  afterEach(/* 恢复全局替身。 */ () => { vi.restoreAllMocks(); });

  test('打开弹窗预填当前金额，关闭后清空结果', /* 验证弹窗生命周期。 */ () => {
    // hook 是被测订单动作 Hook。
    const { hook } = createHook();
    act(/* 打开订单改价弹窗。 */ () => hook.result.current.handleAdjustPrice(orderFixture));
    expect(hook.result.current.showPriceModal).toBe(true);
    expect(hook.result.current.priceOrder?.order_id).toBe('order-1');
    expect(hook.result.current.priceDraft).toBe('10.00');
    act(/* 关闭订单改价弹窗。 */ () => hook.result.current.closePriceModal());
    expect(hook.result.current.showPriceModal).toBe(false);
    expect(hook.result.current.priceResult).toBeNull();
  });

  test('成功时提交规范金额并刷新列表', /* 验证成功路径与提交内容。 */ async () => {
    vi.mocked(adjustOrderPrice).mockResolvedValue(adjustResult);
    // hook、loadOrders 是被测订单动作 Hook 及其列表刷新替身。
    const { hook, loadOrders } = createHook();
    act(/* 打开弹窗并输入带货币符号的金额。 */ () => { hook.result.current.handleAdjustPrice(orderFixture); hook.result.current.setPriceDraft(' ¥8.80 '); });
    await act(/* 提交改价。 */ async () => hook.result.current.executeAdjustPrice());
    expect(adjustOrderPrice).toHaveBeenCalledExactlyOnceWith('order-1', '8.80');
    expect(hook.result.current.priceResult).toEqual({ success: true, message: adjustResult.message });
    expect(hook.result.current.priceLoading).toBe(false);
    expect(loadOrders).toHaveBeenCalledTimes(1);
  });

  test('非法金额只提示不发请求', /* 验证校验失败不触达平台。 */ async () => {
    // hook、loadOrders 是被测订单动作 Hook 及其列表刷新替身。
    const { hook, loadOrders } = createHook();
    act(/* 打开弹窗并输入非法金额。 */ () => { hook.result.current.handleAdjustPrice(orderFixture); hook.result.current.setPriceDraft('0'); });
    await act(/* 提交非法改价。 */ async () => hook.result.current.executeAdjustPrice());
    expect(adjustOrderPrice).not.toHaveBeenCalled();
    expect(hook.result.current.priceResult).toEqual({ success: false, message: '金额必须大于 0' });
    expect(loadOrders).not.toHaveBeenCalled();
  });

  test('未打开弹窗时提交无效果', /* 验证没有目标订单时不会发请求。 */ async () => {
    // hook 是被测订单动作 Hook。
    const { hook } = createHook();
    await act(/* 在没有订单时提交。 */ async () => hook.result.current.executeAdjustPrice());
    expect(adjustOrderPrice).not.toHaveBeenCalled();
  });

  test.each([
    [new Error('平台拒绝'), '平台拒绝'],
    ['非 Error 异常', '改价失败，请重试'],
  ])('接口失败展示原因且不刷新列表', /* error 是接口抛出的异常，message 是期望展示的说明。 */ async (error, message) => {
    vi.mocked(adjustOrderPrice).mockRejectedValue(error);
    // hook、loadOrders 是被测订单动作 Hook 及其列表刷新替身。
    const { hook, loadOrders } = createHook();
    act(/* 打开弹窗并输入合法金额。 */ () => { hook.result.current.handleAdjustPrice(orderFixture); hook.result.current.setPriceDraft('5'); });
    await act(/* 提交改价。 */ async () => hook.result.current.executeAdjustPrice());
    expect(hook.result.current.priceResult).toEqual({ success: false, message });
    expect(hook.result.current.priceLoading).toBe(false);
    expect(loadOrders).not.toHaveBeenCalled();
  });

  test('在途时重复提交被忽略，只会发出一次平台请求', /* 验证单次在途保护。 */ async () => {
    // pending 控制改价响应到达的时机，用于复现关闭、换单和卸载后的迟到结果。
    const pending = deferred<typeof adjustResult>();
    vi.mocked(adjustOrderPrice).mockReturnValue(pending.promise);
    // hook 是被测订单动作 Hook。
    const { hook } = createHook();
    act(/* 打开弹窗并输入金额。 */ () => { hook.result.current.handleAdjustPrice(orderFixture); hook.result.current.setPriceDraft('5'); });
    // task 保存在途的改价提交，便于稍后放行响应并等待其收束。
    let task!: Promise<void>;
    act(/* 第一次提交。 */ () => { task = hook.result.current.executeAdjustPrice(); });
    expect(hook.result.current.priceLoading).toBe(true);
    await act(/* 在途时再次提交。 */ async () => { await hook.result.current.executeAdjustPrice(); });
    expect(adjustOrderPrice).toHaveBeenCalledTimes(1);
    await act(/* 放行响应。 */ async () => { pending.resolve(adjustResult); await task; });
    expect(hook.result.current.priceLoading).toBe(false);
  });

  test('弹窗关闭后到达的成功结果不展示，但仍刷新列表以反映已生效的改价', /* 验证取消后的过期成功结果。 */ async () => {
    // pending 控制改价响应到达的时机，用于复现关闭、换单和卸载后的迟到结果。
    const pending = deferred<typeof adjustResult>();
    vi.mocked(adjustOrderPrice).mockReturnValue(pending.promise);
    // hook、loadOrders 是被测订单动作 Hook 及其列表刷新替身。
    const { hook, loadOrders } = createHook();
    act(/* 打开弹窗并输入金额。 */ () => { hook.result.current.handleAdjustPrice(orderFixture); hook.result.current.setPriceDraft('5'); });
    // task 保存在途的改价提交，便于稍后放行响应并等待其收束。
    let task!: Promise<void>;
    act(/* 提交后立即关闭弹窗。 */ () => { task = hook.result.current.executeAdjustPrice(); hook.result.current.closePriceModal(); });
    await act(/* 放行旧结果。 */ async () => { pending.resolve(adjustResult); await task; });
    expect(hook.result.current.priceResult).toBeNull();
    expect(hook.result.current.priceLoading).toBe(false);
    expect(loadOrders).toHaveBeenCalledTimes(1);
  });

  test('换单后到达的旧失败结果不写入新弹窗', /* 验证切换订单的结果隔离。 */ async () => {
    // pending 控制改价响应到达的时机，用于复现关闭、换单和卸载后的迟到结果。
    const pending = deferred<typeof adjustResult>();
    vi.mocked(adjustOrderPrice).mockReturnValue(pending.promise);
    // hook、loadOrders 是被测订单动作 Hook 及其列表刷新替身。
    const { hook, loadOrders } = createHook();
    act(/* 打开订单 A 并提交。 */ () => { hook.result.current.handleAdjustPrice(orderFixture); hook.result.current.setPriceDraft('5'); });
    // task 保存在途的改价提交，便于稍后放行响应并等待其收束。
    let task!: Promise<void>;
    act(/* 提交订单 A 改价。 */ () => { task = hook.result.current.executeAdjustPrice(); });
    act(/* 关闭后打开订单 B。 */ () => { hook.result.current.closePriceModal(); hook.result.current.handleAdjustPrice({ ...orderFixture, order_id: 'order-2', amount: '' }); });
    expect(hook.result.current.priceDraft).toBe('');
    await act(/* 放行订单 A 的失败。 */ async () => { pending.reject(new Error('旧订单失败')); await task; });
    expect(hook.result.current.priceOrder?.order_id).toBe('order-2');
    expect(hook.result.current.priceResult).toBeNull();
    expect(loadOrders).not.toHaveBeenCalled();
  });

  test('卸载后到达的结果不再写入状态也不刷新列表', /* 验证卸载隔离。 */ async () => {
    // pending 控制改价响应到达的时机，用于复现关闭、换单和卸载后的迟到结果。
    const pending = deferred<typeof adjustResult>();
    vi.mocked(adjustOrderPrice).mockReturnValue(pending.promise);
    // hook、loadOrders 是被测订单动作 Hook 及其列表刷新替身。
    const { hook, loadOrders } = createHook();
    act(/* 打开弹窗并输入金额。 */ () => { hook.result.current.handleAdjustPrice(orderFixture); hook.result.current.setPriceDraft('5'); });
    // task 保存在途的改价提交，便于稍后放行响应并等待其收束。
    let task!: Promise<void>;
    act(/* 提交改价。 */ () => { task = hook.result.current.executeAdjustPrice(); });
    hook.unmount();
    await act(/* 卸载后放行响应。 */ async () => { pending.resolve(adjustResult); await task; });
    expect(loadOrders).not.toHaveBeenCalled();
  });
});
