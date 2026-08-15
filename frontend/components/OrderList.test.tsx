// @vitest-environment jsdom
import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import type { Order } from '../types';

// orderListMocks 保存订单页面测试使用的 Hook、API 和子组件替身。
const orderListMocks = vi.hoisted(/* orderListMockFactory 创建订单页面共享替身。 */ () => ({
  orders: [] as Order[],
  setFilter: vi.fn(),
  setAccountFilter: vi.fn(),
  setSearchText: vi.fn(),
  setPage: vi.fn(),
  loadOrders: vi.fn(),
  openImportModal: vi.fn(),
  closeImportModal: vi.fn(),
  setImportFile: vi.fn(),
  handleImportOrders: vi.fn(),
  handleRetryImport: vi.fn(),
  syncOrders: vi.fn(),
  syncSingleOrder: vi.fn(),
  manualShipOrder: vi.fn(),
  updateOrder: vi.fn(),
  deleteOrder: vi.fn(),
}));

vi.mock('../app/features/orders/hooks', /* ordersHooksMockFactory 提供订单查询与导入 Hook 替身。 */ () => ({
  useOrderQuery: /* useOrderQueryMock 返回订单页面固定查询状态。 */ () => ({
    orders: orderListMocks.orders,
    accounts: [{ id: 'account-1', enabled: true, auto_confirm: false, nickname: 'Alpha', remark: '主账号' }],
    filter: 'all',
    setFilter: orderListMocks.setFilter,
    accountFilter: '',
    setAccountFilter: orderListMocks.setAccountFilter,
    searchText: '',
    setSearchText: orderListMocks.setSearchText,
    page: 2,
    setPage: orderListMocks.setPage,
    totalPages: 3,
    loading: false,
    loadOrders: orderListMocks.loadOrders,
    accountName: /* accountNameMock 返回订单筛选账号名称。 */ () => '主账号 · account',
    accountNickname: /* accountNicknameMock 返回订单行账号名称。 */ () => '主账号',
    getItemNameById: /* itemNameMock 返回订单商品名称。 */ (_cookieId: string, _itemId: string, orderItemTitle?: string) => orderItemTitle || '测试商品',
  }),
  useOrderImport: /* useOrderImportMock 返回订单导入弹窗状态。 */ () => ({
    showImportModal: false,
    importFile: null,
    setImportFile: orderListMocks.setImportFile,
    importResult: null,
    importing: false,
    importError: '',
    openImportModal: orderListMocks.openImportModal,
    closeImportModal: orderListMocks.closeImportModal,
    handleImportOrders: orderListMocks.handleImportOrders,
    handleRetryImport: orderListMocks.handleRetryImport,
  }),
}));

vi.mock('../app/features/orders/api', /* ordersApiMockFactory 提供订单页面动作 API 替身。 */ () => ({
  syncOrders: orderListMocks.syncOrders,
  syncSingleOrder: orderListMocks.syncSingleOrder,
  manualShipOrder: orderListMocks.manualShipOrder,
  updateOrder: orderListMocks.updateOrder,
  deleteOrder: orderListMocks.deleteOrder,
}));

vi.mock('../app/features/orders/components/OrderFilterBar', /* filterBarMockFactory 提供订单筛选栏替身。 */ () => {
  // OrderFilterBarMock 暴露筛选栏的状态切换和输入事件。
  const OrderFilterBarMock: React.FC<any> = (props /* props 表示筛选栏状态和事件回调。 */) => (
    <div data-testid="order-filter-bar">
      <button onClick={/* filterAction 切换待发货筛选。 */ () => props.onFilterChange('pending_ship')}>待发货筛选</button>
      <select aria-label="按账号筛选订单" value={props.accountFilter} onChange={/* accountAction 切换账号筛选。 */ event => props.onAccountFilterChange(event.target.value)}>
        <option value="">全部账号</option>
        <option value="account-1">主账号</option>
      </select>
      <input placeholder="搜索订单号/商品/买家..." value={props.searchText} onChange={/* searchAction 修改订单搜索词。 */ event => props.onSearchChange(event.target.value)} />
    </div>
  );
  return { OrderFilterBar: OrderFilterBarMock };
});

vi.mock('../app/features/orders/components/OrderImportModal', /* importModalMockFactory 提供订单导入弹窗替身。 */ () => ({
  OrderImportModal: /* OrderImportModalMock 表示订单导入弹窗替身。 */ (props: any) => props.showImportModal ? <div data-testid="import-modal">订单导入弹窗</div> : null,
}));

import OrderList from './OrderList';

// orderFixture 表示订单页面测试中的待发货订单。
const orderFixture: Order = {
  id: 'row-1',
  order_id: 'order-1',
  cookie_id: 'account-1',
  item_id: 'item-1',
  item_title: '测试商品',
  item_image: '',
  item_price: '10.00',
  buyer_id: 'buyer-1',
  quantity: 2,
  amount: '20.00',
  status: 'pending_ship',
  receiver_name: '收货人',
  receiver_phone: '13800000000',
  receiver_address: '测试地址',
  created_at: '2026-08-15T10:00:00Z',
};

describe('OrderList 页面组合行为', /* 当前回调验证订单筛选、编辑、发货、同步、删除和分页流程。 */ () => {
  beforeEach(/* 当前回调重置订单页面 API、Hook 状态和浏览器提示替身。 */ () => {
    vi.clearAllMocks();
    orderListMocks.orders = [orderFixture];
    orderListMocks.loadOrders.mockResolvedValue(undefined);
    orderListMocks.syncOrders.mockResolvedValue({ success: true, message: '同步完成' });
    orderListMocks.syncSingleOrder.mockResolvedValue({ success: true, message: '订单已同步' });
    orderListMocks.manualShipOrder.mockResolvedValue({ results: [{ success: true, message: '发货成功' }] });
    orderListMocks.updateOrder.mockResolvedValue({ success: true });
    orderListMocks.deleteOrder.mockResolvedValue({ success: true });
    vi.spyOn(window, 'alert').mockImplementation(/* alertImplementation 屏蔽订单页面提示。 */ () => undefined);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
  });

  afterEach(/* 当前回调清理订单页面 DOM 和浏览器提示替身。 */ () => {
    cleanup();
    vi.restoreAllMocks();
  });

  test('筛选栏、同步和导入按钮转发页面操作', /* 当前回调验证订单页面顶部操作组合边界。 */ async () => {
    render(<OrderList />);
    fireEvent.click(screen.getByText('待发货筛选'));
    expect(orderListMocks.setFilter).toHaveBeenCalledWith('pending_ship');
    expect(orderListMocks.setPage).toHaveBeenCalledWith(1);
    expect(orderListMocks.setSearchText).toHaveBeenCalledWith('');

    fireEvent.change(screen.getByLabelText('按账号筛选订单'), { target: { value: 'account-1' } });
    expect(orderListMocks.setAccountFilter).toHaveBeenCalledWith('account-1');
    fireEvent.change(screen.getByPlaceholderText('搜索订单号/商品/买家...'), { target: { value: 'order-1' } });
    expect(orderListMocks.setSearchText).toHaveBeenCalledWith('order-1');

    fireEvent.click(screen.getByText('一键同步订单'));
    await waitFor(/* syncAssertion 等待订单同步 API 完成。 */ () => expect(orderListMocks.syncOrders).toHaveBeenCalledWith(undefined, 'all'));
    expect(orderListMocks.loadOrders).toHaveBeenCalled();
    expect(window.alert).toHaveBeenCalledWith('同步完成');

    fireEvent.click(screen.getByText('插入订单'));
    expect(orderListMocks.openImportModal).toHaveBeenCalledTimes(1);
  });

  test('订单详情和编辑保存保持字段映射', /* 当前回调验证订单详情展示和编辑提交边界。 */ async () => {
    render(<OrderList />);
    fireEvent.click(screen.getByTitle('查看详情'));
    expect(screen.getByText('订单详情')).toBeTruthy();
    fireEvent.click(screen.getByText('关闭'));
    expect(screen.queryByText('订单详情')).toBeNull();

    fireEvent.click(screen.getByTitle('编辑订单'));
    fireEvent.change(screen.getByDisplayValue('buyer-1'), { target: { value: 'buyer-2' } });
    fireEvent.change(screen.getByDisplayValue('20.00'), { target: { value: '30.00' } });
    fireEvent.click(screen.getByText('保存更改'));
    await waitFor(/* updateAssertion 等待订单编辑 API 完成。 */ () => expect(orderListMocks.updateOrder).toHaveBeenCalled());
    expect(orderListMocks.updateOrder).toHaveBeenCalledWith('order-1', expect.objectContaining({ order_status: 'pending_ship', buyer_id: 'buyer-2', amount: '30.00' }));
    expect(orderListMocks.loadOrders).toHaveBeenCalled();
  });

  test('发货、单笔同步和删除操作分别调用对应 API', /* 当前回调验证订单行动作和结果收束。 */ async () => {
    render(<OrderList />);
    fireEvent.click(screen.getByText('立即发货'));
    expect(screen.getByText('请选择发货方式：')).toBeTruthy();
    fireEvent.click(screen.getByText('仅修改闲鱼发货状态'));
    await waitFor(/* shipAssertion 等待订单发货 API 完成。 */ () => expect(orderListMocks.manualShipOrder).toHaveBeenCalledWith(['order-1'], 'status_only'));
    expect(screen.getByText('✓ 发货成功')).toBeTruthy();
    expect(orderListMocks.loadOrders).toHaveBeenCalled();

    fireEvent.click(screen.getByTitle('同步订单'));
    await waitFor(/* singleSyncAssertion 等待单笔订单同步 API 完成。 */ () => expect(orderListMocks.syncSingleOrder).toHaveBeenCalledWith('order-1'));
    expect(orderListMocks.loadOrders).toHaveBeenCalledTimes(2);

    fireEvent.click(screen.getByTitle('删除订单'));
    await waitFor(/* deleteAssertion 等待订单删除 API 完成。 */ () => expect(orderListMocks.deleteOrder).toHaveBeenCalledWith('order-1'));
    expect(orderListMocks.loadOrders).toHaveBeenCalledTimes(2);
    expect(orderListMocks.setPage).toHaveBeenCalledWith(expect.any(Function));
  });

  test('分页按钮使用函数式更新并遵守边界状态', /* 当前回调验证订单分页控件的可操作边界。 */ () => {
    render(<OrderList />);
    fireEvent.click(screen.getByRole('button', { name: '上一页' }));
    fireEvent.click(screen.getByRole('button', { name: '下一页' }));
    expect(orderListMocks.setPage).toHaveBeenCalledWith(expect.any(Function));
    // pageUpdaters 保存页面翻页使用的函数式更新回调。
    const pageUpdaters = orderListMocks.setPage.mock.calls.filter(/* pageUpdaterCall 筛选分页函数式更新调用。 */ call => typeof call[0] === 'function');
    expect(pageUpdaters).toHaveLength(2);
    expect(pageUpdaters[0][0](2)).toBe(1);
    expect(pageUpdaters[1][0](2)).toBe(3);
  });
});
