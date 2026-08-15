// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, test, vi } from 'vitest';
import OrderList from './OrderList';
import { useOrderImport, useOrderQuery } from '../app/features/orders/hooks';

vi.mock('../app/features/orders/hooks', /* ordersHookMockFactory 提供订单页面查询和导入状态。 */ () => ({ useOrderImport: vi.fn(), useOrderQuery: vi.fn() }));
vi.mock('../app/features/orders/components/OrderFilterBar', /* filterBarMockFactory 隔离订单筛选子组件。 */ () => ({ OrderFilterBar: /* filterBarRenderer 渲染筛选占位节点。 */ () => <div data-testid="order-filter" /> }));
vi.mock('../app/features/orders/components/OrderImportModal', /* importModalMockFactory 隔离订单导入子组件。 */ () => ({ OrderImportModal: /* importModalRenderer 渲染导入占位节点。 */ () => <div data-testid="order-import-modal" /> }));

// useOrderQueryMock 是订单查询 Hook 的可控替身。
const useOrderQueryMock = vi.mocked(useOrderQuery);
// useOrderImportMock 是订单导入 Hook 的可控替身。
const useOrderImportMock = vi.mocked(useOrderImport);

describe('OrderList 页面空状态', /* 当前回调覆盖订单列表空数据和导入入口。 */ () => {
  test('没有订单时展示空列表并可以打开导入入口', /* 当前回调验证订单中心初始页面结构。 */ () => {
    // loadOrders 是订单列表刷新动作替身。
    const loadOrders = vi.fn().mockResolvedValue(undefined);
    // openImportModal 是订单导入弹窗打开动作替身。
    const openImportModal = vi.fn();
    useOrderQueryMock.mockReturnValue({ orders: [], accounts: [], filter: '', setFilter: vi.fn(), accountFilter: '', setAccountFilter: vi.fn(), searchText: '', setSearchText: vi.fn(), page: 1, setPage: vi.fn(), totalPages: 0, loading: false, loadOrders, accountName: vi.fn().mockReturnValue(''), accountNickname: vi.fn().mockReturnValue(''), getItemNameById: vi.fn().mockReturnValue('') } as never);
    useOrderImportMock.mockReturnValue({ openImportModal } as never);
    render(<OrderList />);
    expect(screen.getByText('订单中心')).toBeTruthy();
    expect(screen.getByTestId('order-filter')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '插入订单' }));
    expect(openImportModal).toHaveBeenCalled();
  });
});
