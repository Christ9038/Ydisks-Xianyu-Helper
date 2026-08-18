import { useCallback,useRef,useState,type Dispatch,type SetStateAction } from 'react';
import type { Order } from './api';
import { deleteOrder,manualShipOrder,syncOrders,syncSingleOrder,updateOrder } from './api';

// OrderShipMode 表示订单发货操作的两种业务模式。
export type OrderShipMode = 'status_only' | 'full_delivery';

// OrderShipResult 描述订单发货弹窗展示的结果信息。
export interface OrderShipResult {
  // success 表示发货操作是否成功。
  success: boolean;
  // message 保存发货操作的用户可见说明。
  message: string;
}

// OrderActionsOptions 描述订单动作协调器依赖的查询状态和页面操作。
export interface OrderActionsOptions {
  // orders 保存当前分页中的订单，用于删除后的分页判断。
  orders: Order[];
  // page 保存当前订单列表页码。
  page: number;
  // accountFilter 保存当前账号筛选条件。
  accountFilter: string;
  // filter 保存当前订单状态筛选条件。
  filter: string;
  // setPage 更新订单列表页码。
  setPage: Dispatch<SetStateAction<number>>;
  // loadOrders 刷新当前筛选条件下的订单列表。
  loadOrders: () => Promise<void>;
}

// OrderActionsState 暴露订单页面动作、弹窗状态和异步结果。
export interface OrderActionsState {
  // showDetailModal 表示订单详情弹窗是否打开。
  showDetailModal: boolean;
  // selectedOrder 保存当前查看详情的订单。
  selectedOrder: Order | null;
  // showEditModal 表示订单编辑弹窗是否打开。
  showEditModal: boolean;
  // editingOrder 保存当前编辑中的订单草稿。
  editingOrder: Partial<Order> | null;
  // showShipModal 表示订单发货弹窗是否打开。
  showShipModal: boolean;
  // shipOrderId 保存当前待发货订单号。
  shipOrderId: string;
  // shipLoading 表示发货请求是否正在执行。
  shipLoading: boolean;
  // shipResult 保存最近一次发货操作结果。
  shipResult: OrderShipResult | null;
  // syncingOrderId 保存当前正在单笔同步的订单号。
  syncingOrderId: string | null;
  // deletingOrderId 保存当前正在删除的订单号。
  deletingOrderId: string | null;
  // handleSync 同步当前筛选条件下的订单。
  handleSync: () => Promise<void>;
  // handleShip 打开发货弹窗并选择待发货订单。
  handleShip: (orderId: string) => void;
  // executeShip 执行指定模式的订单发货。
  executeShip: (mode: OrderShipMode) => Promise<void>;
  // handleViewDetail 打开指定订单的详情弹窗。
  handleViewDetail: (order: Order) => void;
  // handleEdit 打开指定订单的编辑弹窗。
  handleEdit: (order: Order) => void;
  // handleSaveEdit 保存当前订单编辑草稿。
  handleSaveEdit: () => Promise<void>;
  // updateEditingOrder 更新当前订单编辑草稿的局部字段。
  updateEditingOrder: (patch: Partial<Order>) => void;
  // handleSyncSingle 同步指定的单笔订单。
  handleSyncSingle: (orderId: string) => Promise<void>;
  // handleDelete 删除指定订单并处理分页回退。
  handleDelete: (orderId: string) => Promise<void>;
  // closeDetailModal 关闭订单详情弹窗。
  closeDetailModal: () => void;
  // closeEditModal 关闭订单编辑弹窗。
  closeEditModal: () => void;
  // closeShipModal 关闭订单发货弹窗并清理结果。
  closeShipModal: () => void;
}

// orderErrorMessage 将未知异常转换为稳定的订单动作提示。
const orderErrorMessage = (error: unknown, fallback: string): string => error instanceof Error ? error.message : fallback;

// useOrderActions 集中管理订单同步、发货、编辑、删除和弹窗生命周期。
export const useOrderActions = ({ orders, page, accountFilter, filter, setPage, loadOrders }: OrderActionsOptions): OrderActionsState => {
  // showDetailModal 表示订单详情弹窗是否打开。
  const [showDetailModal, setShowDetailModal] = useState(false);
  // selectedOrder 保存当前查看详情的订单。
  const [selectedOrder, setSelectedOrder] = useState<Order | null>(null);
  // showEditModal 表示订单编辑弹窗是否打开。
  const [showEditModal, setShowEditModal] = useState(false);
  // editingOrder 保存当前编辑中的订单草稿。
  const [editingOrder, setEditingOrder] = useState<Partial<Order> | null>(null);
  // showShipModal 表示订单发货弹窗是否打开。
  const [showShipModal, setShowShipModal] = useState(false);
  // shipOrderId 保存当前待发货订单号。
  const [shipOrderId, setShipOrderId] = useState('');
  // shipLoading 表示发货请求是否正在执行。
  const [shipLoading, setShipLoading] = useState(false);
  // shipResult 保存最近一次发货操作结果。
  const [shipResult, setShipResult] = useState<OrderShipResult | null>(null);
  // syncingOrderId 保存当前正在单笔同步的订单号。
  const [syncingOrderId, setSyncingOrderId] = useState<string | null>(null);
  // deletingOrderId 保存当前正在删除的订单号。
  const [deletingOrderId, setDeletingOrderId] = useState<string | null>(null);
  // syncGeneration 区分连续发起的批量刷新，旧任务完成后不得覆盖最新一次用户操作的列表和提示。
  const syncGeneration = useRef(0);

  // handleSync 同步当前筛选条件下的订单并刷新列表。
  const handleSync = useCallback(/* syncAction 执行当前筛选条件下的订单同步。 */ async () => {
		// generation 是本次批量同步的代次；仅仍为最新代次的结果允许更新界面。
		const generation = ++syncGeneration.current;
    try {
      // result 保存订单同步接口返回的结果说明。
      const result = await syncOrders(accountFilter || undefined, filter);
			if (generation !== syncGeneration.current) return;
      await loadOrders();
			if (generation !== syncGeneration.current) return;
      if (result.message) alert(result.message);
    } catch (/* error 表示订单同步请求异常。 */ error: unknown) {
			if (generation !== syncGeneration.current) return;
      console.error('同步订单失败:', error);
      alert(orderErrorMessage(error, '同步失败，请重试'));
    }
  }, [accountFilter, filter, loadOrders]);

  // handleShip 打开发货弹窗并选择待发货订单。
  const handleShip = useCallback(/* shipAction 打开发货弹窗并保存订单号。 */ (orderId: string) => {
    setShipOrderId(orderId);
    setShipResult(null);
    setShowShipModal(true);
  }, []);

  // executeShip 执行指定模式的订单发货并更新结果。
  const executeShip = useCallback(/* executeShipAction 执行指定模式的订单发货。 */ async (mode: OrderShipMode) => {
    setShipLoading(true);
    setShipResult(null);
    try {
      // response 保存订单批量发货接口响应。
      const response = await manualShipOrder([shipOrderId], mode);
      // result 保存当前订单的发货结果行。
      const result = response.results?.[0];
      if (result?.success) {
        setShipResult({ success: true, message: result.message });
        void loadOrders();
      } else {
        setShipResult({ success: false, message: result?.message || '发货失败' });
      }
    } catch (/* error 表示订单发货请求异常。 */ error: unknown) {
      setShipResult({ success: false, message: orderErrorMessage(error, '请求失败') });
    } finally {
      setShipLoading(false);
    }
  }, [loadOrders, shipOrderId]);

  // handleViewDetail 打开指定订单的详情弹窗。
  const handleViewDetail = useCallback(/* detailAction 打开订单详情弹窗。 */ (order: Order) => {
    setSelectedOrder(order);
    setShowDetailModal(true);
  }, []);

  // handleEdit 打开指定订单的编辑弹窗并复制编辑草稿。
  const handleEdit = useCallback(/* editAction 打开订单编辑弹窗。 */ (order: Order) => {
    setEditingOrder({ ...order });
    setShowEditModal(true);
  }, []);

  // updateEditingOrder 使用函数式更新合并订单编辑草稿字段。
  const updateEditingOrder = useCallback(/* draftAction 合并订单编辑草稿字段。 */ (patch: Partial<Order>) => {
    setEditingOrder(/* currentDraft 当前订单编辑草稿。 */ current => current ? { ...current, ...patch } : current);
  }, []);

  // handleSaveEdit 保存当前订单编辑草稿并刷新列表。
  const handleSaveEdit = useCallback(/* saveEditAction 保存订单编辑草稿。 */ async () => {
    if (!editingOrder?.order_id) return;
    try {
      // updateData 保存映射到订单更新接口的字段。
      const updateData: Partial<Order> = {};
      if (editingOrder.status !== undefined) updateData.order_status = editingOrder.status;
      if (editingOrder.buyer_id !== undefined) updateData.buyer_id = editingOrder.buyer_id;
      if (editingOrder.amount !== undefined) updateData.amount = editingOrder.amount;
      if (editingOrder.receiver_name !== undefined) updateData.receiver_name = editingOrder.receiver_name;
      if (editingOrder.receiver_phone !== undefined) updateData.receiver_phone = editingOrder.receiver_phone;
      if (editingOrder.receiver_address !== undefined) updateData.receiver_address = editingOrder.receiver_address;
      if (editingOrder.item_id !== undefined) updateData.item_id = editingOrder.item_id;
      if (editingOrder.quantity !== undefined) updateData.quantity = editingOrder.quantity;
      if (editingOrder.item_title !== undefined) updateData.item_title = editingOrder.item_title;

      await updateOrder(editingOrder.order_id, updateData);
      setShowEditModal(false);
      setEditingOrder(null);
      await loadOrders();
    } catch (/* error 表示订单编辑请求异常。 */ error: unknown) {
      console.error('更新订单失败:', error);
      alert('更新失败，请重试');
    }
  }, [editingOrder, loadOrders]);

  // handleSyncSingle 同步指定的单笔订单并刷新列表。
  const handleSyncSingle = useCallback(/* singleSyncAction 执行单笔订单同步。 */ async (orderId: string) => {
    setSyncingOrderId(orderId);
    try {
      // result 保存单笔订单同步接口响应。
      const result = await syncSingleOrder(orderId);
      if (result.success) {
        await loadOrders();
      } else {
        alert(result.message || '同步失败');
      }
    } catch (/* error 表示单笔订单同步请求异常。 */ error: unknown) {
      console.error('同步订单失败:', error);
      alert(orderErrorMessage(error, '同步失败，请重试'));
    } finally {
      setSyncingOrderId(null);
    }
  }, [loadOrders]);

  // handleDelete 删除指定订单并在当前页为空时回退页码。
  const handleDelete = useCallback(/* deleteAction 删除订单并处理分页回退。 */ async (orderId: string) => {
    if (!confirm('确认删除该订单吗？删除后无法恢复。')) return;
    setDeletingOrderId(orderId);
    try {
      await deleteOrder(orderId);
      if (orders.length === 1 && page > 1) {
        setPage(/* currentPage 当前订单页码。 */ current => current - 1);
      } else {
        await loadOrders();
      }
    } catch (/* error 表示订单删除请求异常。 */ error: unknown) {
      console.error('删除订单失败:', error);
      alert(orderErrorMessage(error, '删除失败，请重试'));
      await loadOrders();
    } finally {
      setDeletingOrderId(null);
    }
  }, [loadOrders, orders.length, page, setPage]);

  // closeDetailModal 关闭订单详情弹窗。
  const closeDetailModal = useCallback(/* closeDetailAction 关闭订单详情弹窗。 */ () => setShowDetailModal(false), []);
  // closeEditModal 关闭订单编辑弹窗。
  const closeEditModal = useCallback(/* closeEditAction 关闭订单编辑弹窗。 */ () => setShowEditModal(false), []);
  // closeShipModal 关闭订单发货弹窗并清理结果。
  const closeShipModal = useCallback(/* closeShipAction 关闭订单发货弹窗。 */ () => {
    setShowShipModal(false);
    setShipResult(null);
  }, []);

  return {
    showDetailModal,
    selectedOrder,
    showEditModal,
    editingOrder,
    showShipModal,
    shipOrderId,
    shipLoading,
    shipResult,
    syncingOrderId,
    deletingOrderId,
    handleSync,
    handleShip,
    executeShip,
    handleViewDetail,
    handleEdit,
    handleSaveEdit,
    updateEditingOrder,
    handleSyncSingle,
    handleDelete,
    closeDetailModal,
    closeEditModal,
    closeShipModal,
  };
};
