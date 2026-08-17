import { get, post, put, del, postForm, type RequestControlOptions } from '../../../shared/http/client';
import {
  SessionResponse, AccountDetail, AccountSummaryResponse, Order, PaginatedResponse,
  AdminStats, DashboardStats, Card, SystemSettings, OrderAnalytics,
  Item, AIReplySettings, ShippingRule, ReplyRule, DefaultReply, AutomationAction, AutomationTriggerType,
  NotificationChannel, NotificationEventType, AccountTaskSettings, ChatSession, ChatMessage, ItemListEnvelope, AutomationIssuesEnvelope,
  CookieSettingsResponse, CookieProfileResponse, ItemDetailResponse, ItemPublishResponse, ItemSyncResponse, OrderDTOResponse, OrderDetailResponse, OrderSingleRefreshResponse, OrderBatchResponse, OrderRefreshResponse, OrderRefreshJobStartResponse, OrderRefreshJobStatusResponse, OrderRefreshJobCancelResponse, AutomationRuleResponse, AutomationRulePageResponse, AIReplySettingsResponse, AIModelsResponse, UserSettingResponse, CardBatchResponse, CardAppendResponse, CategoryRecommendationResponse, ItemPublishBatchPreviewResponse, ItemPublishBatchListResponse, BatchIDResponse, ItemPublishBatchResponse, BatchCancelResponse, MutationIDResponse, OperationResponse, NotificationChannelResponse, NotificationBinding, AccountBindingsResponse, CardListResponse, KeywordTypedResponse, DefaultReplyResponse, AccountTaskSettingsResponse, AccountTaskRunResponseEnvelope, AdminStatsResponse, DashboardStatsResponse, OrderAnalyticsResponse, QRLoginGenerateResponse, QRLoginStatusResponse, QRLoginVerificationResponse, ValidOrderResponse, ValidOrdersResponse
} from '../../../shared/api-contract';
import { collectionFrom, objectFrom } from '../../../shared/http/contract';

/** 订单筛选器读取非敏感账号摘要。 */
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => get('/api/v1/accounts/details', undefined, options);

/** 订单关联商品展示读取当前商品索引。 */
export const getItems = async (accountID?: string, options?: RequestControlOptions): Promise<Item[]> => get('/api/v1/items', accountID ? { cookie_id: accountID } : undefined, options);

/** 管理员统计仍由订单域兼容 API 提供给历史管理页面。 */
export const getAdminStats = async (): Promise<AdminStatsResponse> => get('/api/v1/admin/stats');
// Orders
// normalizeOrderStatus 归一化订单状态。
const normalizeOrderStatus = (value: unknown): Order['status'] => {
  // status 状态值，用于当前 API 处理流程。
  const status = String(value || '');
  if (status === 'paid') return 'pending_ship';
  return ['processing', 'pending_ship', 'shipped', 'completed', 'cancelled', 'refunding'].includes(status)
    ? status as Order['status']
    : 'unknown';
};

// getOrders 读取订单列表。
export const getOrders = async (
  cookieId?: string,
  status?: string,
  page: number = 1,
  pageSize: number = 20,
  search?: string,
  options?: RequestControlOptions,
): Promise<PaginatedResponse<Order>> => {
  // params 请求参数，用于当前 API 处理流程。
  const params: any = { page, page_size: pageSize };
  if (cookieId) params.cookie_id = cookieId;
  if (status && status !== 'all') params.status = status;
  if (search?.trim()) params.search = search.trim();

  // res 接口响应结果，用于当前 API 处理流程。
  const response = await get<unknown>('/api/v1/orders', params, options);
  // res 是兼容直接分页对象、orders 别名和 data 包裹后的订单响应。
  const res = objectFrom<Partial<PaginatedResponse<Order>> & { /** orders 是历史订单列表字段别名。 */ orders?: Order[] }>(response, ['data', 'result']) || {};

  // Handle backend response variations
  // rawOrders 原始订单列表，用于当前 API 处理流程。
  const rawOrders = Array.isArray(res.orders) ? res.orders : collectionFrom<Order>(res.data, ['data', 'orders', 'items']);
  // orders 订单列表，用于当前 API 处理流程。
  const orders = rawOrders.map(/* 当前回调用于处理集合元素或接口响应。 */ (item: any) => ({
    ...item,
    id: item.id || item.order_id,
    status: normalizeOrderStatus(item.status || item.order_status),
    quantity: Number(item.quantity || 1),
  }));
  return {
    success: true,
    data: orders,
    total: res.total || orders.length,
    page: res.page || page,
    page_size: res.page_size || pageSize,
    total_pages: res.total_pages || 1
  };
};

// getOrderDetail 读取订单详情。
export const getOrderDetail = async (orderId: string): Promise<{ /** success 表示是否成功。 */ success: boolean; /** data 表示数据。 */ data?: OrderDTOResponse }> => {
  // result 接口响应结果，用于当前 API 处理流程。
  const result = await get<OrderDetailResponse>(`/api/v1/orders/${orderId}`);
  return {
    success: true,
    data: result.data
  };
};

// updateOrder 更新订单。
export const updateOrder = async (orderId: string, data: Partial<Order>): Promise<OperationResponse> => {
  return put(`/api/v1/orders/${orderId}`, data);
};

// deleteOrder 删除订单。
export const deleteOrder = async (orderId: string): Promise<OperationResponse> => {
  return del(`/api/v1/orders/${orderId}`);
};

// syncOrders 同步订单。
export const syncOrders = async (cookieId?: string, status?: string, options?: RequestControlOptions): Promise<OrderRefreshResponse> => {
  // formData 表单数据，用于当前 API 处理流程。
  const formData = new FormData();
  if (cookieId) formData.append('cookie_id', cookieId);
  if (status) formData.append('status', status);

	// start 表示后台订单刷新任务创建响应。
	const start = await postForm<OrderRefreshJobStartResponse>('/api/v1/orders/refresh', formData, options);
	// cancelOnAbort 在调用方取消轮询时通知服务端停止同一后台任务。
	const cancelOnAbort = () => {
		void cancelOrderRefreshJob(start.job_id).catch(/* 取消请求失败时忽略网络错误，主请求仍按取消语义结束。 */ () => undefined);
	};
	options?.signal?.addEventListener('abort', cancelOnAbort, { once: true });
	// pollLimit 限制前端等待后台任务的轮询次数。
	const pollLimit = 180;
	// pollIndex 表示当前订单刷新任务状态轮询次数。
	let pollIndex = 0;
	try {
		while (pollIndex < pollLimit) {
		// job 表示当前轮询得到的后台任务状态。
		const job = await get<OrderRefreshJobStatusResponse>(`/api/v1/orders/refresh/${start.job_id}`, undefined, options);
		if (job.status === 'succeeded' && job.result) {
			return job.result;
		}
		if (job.status === 'failed' || job.status === 'cancelled') {
			throw new Error(job.error_message || '订单刷新任务失败');
		}
		// waitMs 是下一次任务状态轮询前的等待时间。
		const waitMs = 500;
		await new Promise<void>(/* 轮询等待器负责等待下一次任务状态查询。 */ (resolve, reject) => {
			// abort 负责响应调用方取消轮询。
			const abort = () => {
				globalThis.clearTimeout(timer);
				reject(new Error('请求已取消'));
			};
			// timer 表示当前轮询等待定时器。
			const timer = globalThis.setTimeout(/* 轮询完成回调清理取消监听并结束等待。 */ () => {
				options?.signal?.removeEventListener('abort', abort);
				resolve();
			}, waitMs);
			if (!options?.signal) return;
			if (options.signal.aborted) abort();
			else options.signal.addEventListener('abort', abort, { once: true });
		});
		pollIndex += 1;
		}
	} finally {
		options?.signal?.removeEventListener('abort', cancelOnAbort);
	}
	throw new Error('订单刷新任务等待超时');
};

// cancelOrderRefreshJob 请求取消当前用户的订单刷新后台任务。
export const cancelOrderRefreshJob = async (jobId: string): Promise<OrderRefreshJobCancelResponse> => {
	return del(`/api/v1/orders/refresh/${jobId}`);
};

// syncSingleOrder 同步单个订单。
export const syncSingleOrder = async (orderId: string): Promise<OrderSingleRefreshResponse> => {
  return post(`/api/v1/orders/${orderId}/refresh`);
};

// manualShipOrder 手动发货订单。
export const manualShipOrder = async (orderIds: string[], shipMode: 'status_only' | 'full_delivery'): Promise<OrderBatchResponse> => {
    return post('/api/v1/orders/manual-ship', {
        order_ids: orderIds,
        ship_mode: shipMode,
    });
}

// importOrders 导入订单。
export const importOrders = async (data: Partial<Order>[] | FormData, options?: RequestControlOptions): Promise<OrderBatchResponse> => {
	// isFormData 是否为表单请求，用于当前 API 处理流程。
	const isFormData = data instanceof FormData;
	return isFormData ? postForm('/api/v1/orders/import', data, options) : post('/api/v1/orders/import', data, options);
}
