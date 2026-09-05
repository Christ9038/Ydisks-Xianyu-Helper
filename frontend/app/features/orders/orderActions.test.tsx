// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Order,OrderRefreshResponse } from './api';
import { useOrderActions } from './orderActions';

// orderActionMocks 保存订单动作 Hook 测试使用的 API 替身。
const orderActionMocks = vi.hoisted(/* orderActionMockFactory 创建订单动作 API 替身。 */ () => ({
  deleteOrder: vi.fn(),
  manualShipOrder: vi.fn(),
  syncOrders: vi.fn(),
  syncSingleOrder: vi.fn(),
  updateOrder: vi.fn(),
}));

vi.mock('./api', /* orderApiMockFactory 提供订单动作 API 替身。 */ () => ({
  deleteOrder: orderActionMocks.deleteOrder,
  manualShipOrder: orderActionMocks.manualShipOrder,
  syncOrders: orderActionMocks.syncOrders,
  syncSingleOrder: orderActionMocks.syncSingleOrder,
  updateOrder: orderActionMocks.updateOrder,
}));

// orderFixture 表示订单动作 Hook 使用的最小订单。
const orderFixture: Order = {
  id: 'row-1',
  order_id: 'order-1',
  cookie_id: 'account-1',
  item_id: 'item-1',
  item_title: '测试商品',
  buyer_id: 'buyer-1',
  quantity: 1,
  amount: '10.00',
  status: 'pending_ship',
};

// createActionHook 创建订单动作 Hook 并注入页面依赖替身。
const createActionHook = () => {
  // loadOrders 是订单动作完成后的列表刷新替身。
  const loadOrders = vi.fn().mockResolvedValue(undefined);
  // setPage 是订单删除后分页调整替身。
  const setPage = vi.fn();
  // hook 是订单动作 Hook 的渲染结果。
  const hook = renderHook(/* hookFactory 创建订单动作 Hook。 */ () => useOrderActions({
    orders: [orderFixture],
    page: 2,
    accountFilter: 'account-1',
    filter: 'pending_ship',
    setPage,
    loadOrders,
  }));
  return { hook, loadOrders, setPage };
};

describe('useOrderActions 订单动作协调器', /* 当前回调验证订单动作成功、失败、取消和弹窗清理边界。 */ () => {
  beforeEach(/* 当前回调重置订单 API 和浏览器提示替身。 */ () => {
    vi.clearAllMocks();
    orderActionMocks.syncOrders.mockResolvedValue({ message: '同步完成' });
    orderActionMocks.manualShipOrder.mockResolvedValue({ results: [{ success: true, message: '发货成功' }] });
    orderActionMocks.syncSingleOrder.mockResolvedValue({ success: true, message: '同步完成' });
    orderActionMocks.updateOrder.mockResolvedValue({ success: true });
    orderActionMocks.deleteOrder.mockResolvedValue({ success: true });
    vi.spyOn(window, 'alert').mockImplementation(/* alertImplementation 屏蔽订单动作提示。 */ () => undefined);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  afterEach(/* 当前回调恢复浏览器提示替身。 */ () => {
    vi.restoreAllMocks();
  });

  test('同步失败和单笔同步失败均返回稳定提示', /* 当前回调验证订单同步异常分支。 */ async () => {
    orderActionMocks.syncOrders.mockRejectedValueOnce(new Error('批量同步失败'));
    // actionContext 保存订单动作 Hook 和刷新依赖。
    const { hook } = createActionHook();
    await act(/* syncAction 执行失败的订单批量同步。 */ async () => hook.result.current.handleSync());
    expect(window.alert).toHaveBeenCalledWith('批量同步失败');

    orderActionMocks.syncSingleOrder.mockResolvedValueOnce({ success: false, message: '单笔同步失败' });
    await act(/* singleSyncAction 执行失败的单笔同步。 */ async () => hook.result.current.handleSyncSingle('order-1'));
    expect(window.alert).toHaveBeenCalledWith('单笔同步失败');
    expect(hook.result.current.syncingOrderId).toBeNull();
  });

  test('晚到的旧批量同步结果不会覆盖最新请求的列表刷新和提示', /* 当前回调验证重复点击同步时旧任务响应的代次隔离。 */ async () => {
    // resolveFirst 保存首次同步 API 适配器的延迟结果完成函数。
    let resolveFirst: ((value: OrderRefreshResponse) => void) | undefined;
    // firstCompletion 保存首次同步尚未完成的 API 适配器 Promise。
    const firstCompletion = new Promise<OrderRefreshResponse>(/* resolve 保存首次同步的结果完成器。 */ resolve => { resolveFirst = resolve; });
    orderActionMocks.syncOrders.mockImplementationOnce(/* firstSync 模拟较早请求尚未返回。 */ () => firstCompletion)
      .mockResolvedValueOnce({ message: '最新同步完成' });
    // actionContext 保存待验证代次隔离的订单动作 Hook。
    const { hook, loadOrders } = createActionHook();

    // firstTask 启动旧同步但不等待它完成。
    const firstTask = hook.result.current.handleSync();
    // secondTask 启动并完成后发起的最新同步。
    const secondTask = hook.result.current.handleSync();
    await act(/* completeSecond 完成最新同步并等待它刷新列表。 */ async () => { await secondTask; });
    expect(loadOrders).toHaveBeenCalledTimes(1);
    expect(window.alert).toHaveBeenCalledWith('最新同步完成');

    resolveFirst?.({ partial_failure: false, message: '旧同步完成', summary: { restored: 0, reassigned: 0, discovered: 0, list_updated: 0, soft_deleted: 0, detail_total: 0, total: 0, updated: 0, no_change: 0, failed: 0 }, results: [] });
    await act(/* completeFirst 放行旧任务；它不能再刷新列表或展示提示。 */ async () => { await firstTask; });
    expect(loadOrders).toHaveBeenCalledTimes(1);
    expect(window.alert).not.toHaveBeenCalledWith('旧同步完成');
  });

  test.each([true, false])('失败计数非零时不展示旧成功文案，partial_failure=%s', /* partialFailure 是后端批次标记，失败计数需独立生效。 */ async (partialFailure) => {
    orderActionMocks.syncOrders.mockResolvedValueOnce({
      partial_failure: partialFailure, message: '旧后端同步成功',
      summary: { failed: 2, restored: 3, reassigned: 1 },
      results: [
        { success: false, cookie_id: 'account-a', error: '列表读取失败', message: '请重试账号' },
        { success: false, cookie_id: 'account-b', order_id: 'order-b', error: '归属冲突', message: '核对卖家' },
      ],
    });
    // hook 是接受结构化失败结果的订单动作协调器。
    const { hook } = createActionHook();
    await act(/* 等待同步及刷新完成后检查用户实际收到的提示。 */ async () => hook.result.current.handleSync());
    expect(window.alert).toHaveBeenCalledWith(expect.stringContaining('未完成'));
    // expected 是每条失败提示和恢复统计都必须保留的业务信息。
    for (const /* expected 是逐项失败和恢复统计中的必需提示内容。 */ expected of ['account-a', '列表读取失败', '请重试账号', 'account-b', 'order-b', '归属冲突', '核对卖家', '恢复 3', '修正 1']) {
      expect(window.alert).toHaveBeenCalledWith(expect.stringContaining(expected));
    }
    expect(window.alert).not.toHaveBeenCalledWith(expect.stringContaining('旧后端同步成功'));
  });

  test.each(['account', 'status', 'unmount'])('切换筛选或卸载后忽略旧同步结果：%s', /* change 决定触发哪一种同步结果失效边界。 */ async (change) => {
    // resolveSync 用于在筛选变更或卸载之后放行旧请求。
    let resolveSync!: (value: OrderRefreshResponse) => void;
    orderActionMocks.syncOrders.mockImplementationOnce(/* 模拟尚未返回的批量同步。 */ () => new Promise<OrderRefreshResponse>(/* resolve 保存旧同步请求的完成器。 */ resolve => { resolveSync = resolve; }));
    // loadOrders、setPage 分别记录列表刷新与分页操作。
    const loadOrders = vi.fn().mockResolvedValue(undefined), setPage = vi.fn();
    // hook 的筛选属性可在请求过程中切换。
    const hook = renderHook(/* props 保存当前账号与状态筛选条件。 */ (props) => useOrderActions({ orders: [], page: 1, setPage, loadOrders, ...props }), {
      initialProps: { accountFilter: 'account-a', filter: 'pending_ship' },
    });
    // pending 是离开旧筛选上下文前发起的同步操作。
    const pending = hook.result.current.handleSync();
    if (change === 'unmount') hook.unmount();
    else hook.rerender({ accountFilter: change === 'account' ? 'account-b' : 'account-a', filter: change === 'status' ? 'completed' : 'pending_ship' });
    resolveSync({ partial_failure: true, message: '旧同步失败', summary: { restored: 0, reassigned: 0, discovered: 0, list_updated: 0, soft_deleted: 0, detail_total: 0, total: 0, updated: 0, no_change: 0, failed: 1 }, results: [] });
    await act(/* 放行旧响应，验证不会刷新旧列表或弹出旧结果。 */ async () => { await pending; });
    expect(loadOrders).not.toHaveBeenCalled();
    expect(window.alert).not.toHaveBeenCalled();
  });

  test.each(['sync-error', 'list-success', 'list-error'])('切换筛选后，旧异常与列表加载完成均不能弹提示：%s', /* phase 决定旧同步在哪一个异步边界暂停。 */ async (phase) => {
    // complete、fail 分别控制旧请求完成或异常。
    let complete!: () => void, fail!: (error: Error) => void;
    // pendingResponse 在切换账号并切回后才结束，验证同值筛选仍属于新代次。
    const pendingResponse = new Promise<void>(/* resolve、reject 保存旧异步请求完成器。 */ (resolve, reject) => { complete = resolve; fail = reject; });
    // loadOrders、setPage 分别记录列表刷新和分页副作用。
    const loadOrders = vi.fn().mockResolvedValue(undefined), setPage = vi.fn();
    if (phase === 'sync-error') orderActionMocks.syncOrders.mockReturnValueOnce(pendingResponse);
    else loadOrders.mockReturnValueOnce(pendingResponse);
    // hook 使用动态账号筛选以模拟离开再返回同一账号。
    const hook = renderHook(/* accountFilter 是本次渲染的账号标识。 */ ({ accountFilter }) => useOrderActions({ orders: [], page: 1, setPage, loadOrders, accountFilter, filter: 'all' }), { initialProps: { accountFilter: 'account-a' } });
    // pending 是需要跨筛选上下文隔离的旧任务。
    const pending = hook.result.current.handleSync();
    if (phase !== 'sync-error') await act(/* 等待同步成功后进入尚未完成的列表读取。 */ async () => { await Promise.resolve(); });
    hook.rerender({ accountFilter: 'account-b' });
    hook.rerender({ accountFilter: 'account-a' });
    if (phase === 'list-success') complete();
    else fail(new Error('旧请求异常'));
    await act(/* 旧请求完成或失败后均不得再提示。 */ async () => { await pending; });
    expect(window.alert).not.toHaveBeenCalled();
    await act(/* 新上下文中的同步仍能正常完成并提示。 */ async () => hook.result.current.handleSync());
    expect(window.alert).toHaveBeenCalledExactlyOnceWith('同步完成');
  });

  test('发货结果失败和异常均保留错误结果', /* 当前回调验证订单发货异常分支。 */ async () => {
    // actionContext 保存发货动作 Hook 和刷新依赖。
    const { hook } = createActionHook();
    act(/* openShipAction 打开发货弹窗。 */ () => hook.result.current.handleShip('order-1'));
    orderActionMocks.manualShipOrder.mockResolvedValueOnce({ results: [{ success: false, message: '卡券不足' }] });
    await act(/* failedShipAction 执行返回失败结果的发货。 */ async () => hook.result.current.executeShip('full_delivery'));
    expect(hook.result.current.shipResult).toEqual({ success: false, message: '卡券不足' });

    orderActionMocks.manualShipOrder.mockRejectedValueOnce(new Error('发货网络失败'));
    await act(/* errorShipAction 执行抛出异常的发货。 */ async () => hook.result.current.executeShip('status_only'));
    expect(hook.result.current.shipResult).toEqual({ success: false, message: '发货网络失败' });
  });

  test('编辑草稿使用函数式补丁并在保存失败时保留弹窗', /* 当前回调验证订单编辑草稿和失败收束。 */ async () => {
    // actionContext 保存订单编辑动作 Hook 和刷新依赖。
    const { hook } = createActionHook();
    act(/* openEditAction 打开订单编辑弹窗。 */ () => hook.result.current.handleEdit(orderFixture));
    act(/* patchAction 更新订单编辑草稿。 */ () => hook.result.current.updateEditingOrder({ buyer_id: 'buyer-2' }));
    expect(hook.result.current.editingOrder?.buyer_id).toBe('buyer-2');

    orderActionMocks.updateOrder.mockRejectedValueOnce(new Error('编辑失败'));
    await act(/* saveEditAction 执行失败的订单编辑保存。 */ async () => hook.result.current.handleSaveEdit());
    expect(window.alert).toHaveBeenCalledWith('更新失败，请重试');
    expect(hook.result.current.showEditModal).toBe(true);
  });

  test('删除确认取消不调用 API，删除异常会刷新列表', /* 当前回调验证订单删除取消和异常分支。 */ async () => {
    // actionContext 保存订单删除动作 Hook、刷新函数和分页 Setter。
    const { hook, loadOrders, setPage } = createActionHook();
    vi.mocked(window.confirm).mockReturnValueOnce(false);
    await act(/* cancelDeleteAction 取消订单删除确认。 */ async () => hook.result.current.handleDelete('order-1'));
    expect(orderActionMocks.deleteOrder).not.toHaveBeenCalled();

    orderActionMocks.deleteOrder.mockRejectedValueOnce(new Error('删除失败'));
    await act(/* errorDeleteAction 执行抛出异常的订单删除。 */ async () => hook.result.current.handleDelete('order-1'));
    expect(window.alert).toHaveBeenCalledWith('删除失败');
    expect(loadOrders).toHaveBeenCalledTimes(1);
    expect(setPage).not.toHaveBeenCalled();
  });

  test('关闭三个弹窗会清理对应状态', /* 当前回调验证订单弹窗状态收束。 */ () => {
    // actionContext 保存订单弹窗动作 Hook 和刷新依赖。
    const { hook } = createActionHook();
    act(/* detailAction 打开订单详情弹窗。 */ () => hook.result.current.handleViewDetail(orderFixture));
    act(/* editAction 打开订单编辑弹窗。 */ () => hook.result.current.handleEdit(orderFixture));
    act(/* shipAction 打开发货弹窗。 */ () => hook.result.current.handleShip('order-1'));
    act(/* closeAction 关闭订单详情、编辑和发货弹窗。 */ () => {
      hook.result.current.closeDetailModal();
      hook.result.current.closeEditModal();
      hook.result.current.closeShipModal();
    });
    expect(hook.result.current.showDetailModal).toBe(false);
    expect(hook.result.current.showEditModal).toBe(false);
    expect(hook.result.current.showShipModal).toBe(false);
    expect(hook.result.current.shipResult).toBeNull();
  });
});
