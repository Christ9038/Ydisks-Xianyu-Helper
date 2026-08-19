import {
AccountDetail,
AdminStatsResponse,
Item,
OperationResponse,
Order,
OrderBatchResponse,
OrderDTOResponse,
OrderRefreshJobCancelResponse,OrderRefreshJobStatusResponse,
OrderRefreshResponse,
OrderSingleRefreshResponse,
PaginatedResponse
} from '../../../shared/api-contract/orders';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';
import { collectionFrom, objectFrom } from '../../../shared/http/contract';
export type * from '../../../shared/api-contract/orders';

/** 订单刷新前端最多轮询约 90 秒，超过该预算必须请求取消后端 worker。 */
const orderRefreshPollLimit = 180;
/** 订单刷新取消和终态复查使用独立五秒网络预算，不能复用已超时或已 Abort 的主信号。 */
const orderRefreshCancelTimeoutMs = 5_000;

/** OrderListQuery 描述订单列表 operation 可传递的筛选和分页参数。 */
type OrderListQuery = {
  /** 账号筛选标识。 */ cookie_id?: string;
  /** 订单状态筛选值。 */ status?: string;
  /** 用户输入的文本搜索条件。 */ search?: string;
  /** 页码，从一开始。 */ page: number;
  /** 每页最大行数。 */ page_size: number;
};

/** OrderImportFormContract 描述订单文件导入 operation 的 multipart 字段。 */
type OrderImportFormContract = {
  /** 二进制导入文件字段。 */ file?: string;
};

/** OrderRefreshFormContract 描述订单刷新 operation 的 multipart 筛选字段。 */
type OrderRefreshFormContract = {
  /** 可选的账号筛选标识。 */ cookie_id?: string;
  /** 可选的订单状态筛选值。 */ status?: string;
};

/** OrderBatchTransportResponse 描述生成 operation 返回、尚未归一化状态枚举的批量订单结果。 */
type OrderBatchTransportResponse = {
  /** 批次是否包含失败项。 */ partial_failure: boolean;
  /** 可展示的批次执行说明。 */ message: string;
  /** 可选的输入订单总数。 */ total?: number;
  /** 已成功处理的订单数量。 */ success_count: number;
  /** 已失败处理的订单数量。 */ failed_count: number;
  /** 每个订单的 transport 结果。 */ results: Array<{
    /** 平台订单标识。 */ order_id?: string;
    /** 服务端返回的处理状态。 */ status?: string;
    /** 单项是否成功。 */ success?: boolean;
    /** 单项处理说明。 */ message: string;
    /** 所属账号标识。 */ cookie_id?: string;
    /** 不确定远端结果的补偿记录标识。 */ reconciliation_id?: string;
    /** 本地补偿警告。 */ reconciliation_warning?: string;
    /** 当前业务处理阶段。 */ stage?: string;
  }>;
};

/** 订单刷新轮询选项仅控制前端等待行为；不改变后端任务、HTTP 路径或请求体契约。 */
export interface OrderRefreshPollOptions extends RequestControlOptions {
  /** pollLimit 是本次前端最多读取任务状态的次数；省略时保持九十秒默认预算。 */
  pollLimit?: number;
  /** pollIntervalMs 是两次状态读取之间的等待毫秒数；默认五百毫秒。 */
  pollIntervalMs?: number;
}

/** 订单筛选器读取非敏感账号摘要。 */
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => {
  // response 是账号摘要 transport DTO 集合，转换后只向订单 UI 暴露非敏感字段。
  const response = await runContractRequest(/* signal 是本次订单账号摘要请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/accounts/details', { signal }), options);
  return response.map(/* item 是当前待转换的账号摘要 DTO。 */ item => ({
    id: item.id,
    enabled: item.enabled,
    auto_confirm: item.auto_confirm,
    remark: item.remark,
    pause_duration: item.pause_duration,
    paused_until: item.paused_until,
    paused: item.paused,
    username: item.username,
    show_browser: item.show_browser,
    nickname: item.nickname,
    avatar_url: item.avatar_url,
    profile_error: item.profile_error,
  }));
};

/** 订单关联商品展示读取当前商品索引。 */
export const getItems = async (accountID?: string, options?: RequestControlOptions): Promise<Item[]> => runContractRequest(/* signal 控制订单页商品读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items', { params: { query: { cookie_id: accountID } }, signal }), options) as unknown as Promise<Item[]>;

/** 管理员统计仍由订单域兼容 API 提供给历史管理页面。 */
export const getAdminStats = async (): Promise<AdminStatsResponse> =>
  runContractRequest(/* signal 是本次管理员统计请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/admin/stats', { signal }));
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
  // params 是生成契约约束的订单筛选与分页查询参数。
  const params: OrderListQuery = { page, page_size: pageSize };
  if (cookieId) params.cookie_id = cookieId;
  if (status && status !== 'all') params.status = status;
  if (search?.trim()) params.search = search.trim();

  // response 是生成契约约束的订单分页 transport DTO。
  const response = await runContractRequest(/* signal 是本次订单分页请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/orders', {
    params: { query: params },
    signal,
  }), options);
  // legacyResponse 是保留给旧 handler 包装和测试夹具的适配视图，不扩散到 UI model。
  const legacyResponse = response as unknown;
  // pageResponse 是直接分页对象或 data/result 包装后的页面元数据。
  const pageResponse = objectFrom<Partial<PaginatedResponse<Order>> & { /** orders 是历史订单列表字段别名。 */ orders?: Order[] }>(legacyResponse, ['data', 'result']) || {};
  // rawOrders 是当前分页中的订单 transport DTO；兼容归一只在 adapter 内完成。
  const rawOrders = Array.isArray(pageResponse.orders) ? pageResponse.orders : collectionFrom<Order>(pageResponse.data, ['data', 'orders', 'items']);
  // orders 订单列表，用于当前 API 处理流程。
  const orders = rawOrders.map(/* item 是当前需要归一化状态与数量的订单 DTO。 */ item => ({
    ...item,
    id: item.order_id,
    order_status: normalizeOrderStatus(item.order_status),
    status: normalizeOrderStatus(item.status || item.order_status),
    quantity: Number(item.quantity || 1),
  }));
  return {
    success: true,
    data: orders,
    total: pageResponse.total || orders.length,
    page: pageResponse.page || page,
    page_size: pageResponse.page_size || pageSize,
    total_pages: pageResponse.total_pages || 1
  };
};

// getOrderDetail 读取订单详情。
export const getOrderDetail = async (orderId: string): Promise<{ /** success 表示是否成功。 */ success: boolean; /** data 表示数据。 */ data?: OrderDTOResponse }> => {
  // result 接口响应结果，用于当前 API 处理流程。
  const result = await runContractRequest(/* signal 是本次订单详情请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/orders/{order_id}', {
    params: { path: { order_id: orderId } },
    signal,
  }));
  return {
    success: true,
    data: result.data
  };
};

// updateOrder 更新订单。
export const updateOrder = async (orderId: string, data: Partial<Order>): Promise<OperationResponse> => {
  return runContractRequest(/* signal 是本次订单更新请求的超时与取消控制信号。 */ signal => contractClient.PUT('/api/v1/orders/{order_id}', {
    params: { path: { order_id: orderId } },
    body: data,
    signal,
  }));
};

// deleteOrder 删除订单。
export const deleteOrder = async (orderId: string): Promise<OperationResponse> => {
  return runContractRequest(/* signal 是本次订单删除请求的超时与取消控制信号。 */ signal => contractClient.DELETE('/api/v1/orders/{order_id}', {
    params: { path: { order_id: orderId } },
    signal,
  }));
};

// syncOrders 同步订单。
export const syncOrders = async (cookieId?: string, status?: string, options?: OrderRefreshPollOptions): Promise<OrderRefreshResponse> => {
  // formData 表单数据，用于当前 API 处理流程。
  const formData = new FormData();
  if (cookieId) formData.append('cookie_id', cookieId);
  if (status) formData.append('status', status);

	// start 表示后台订单刷新任务创建响应。
	const start = await runContractRequest(/* signal 是本次订单刷新任务创建请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/orders/refresh', {
    body: formData as unknown as OrderRefreshFormContract,
    signal,
  }), options);
	// cancelOnAbort 在调用方取消轮询时通知服务端停止同一后台任务；取消命令使用独立信号，主请求已取消也能发出。
	const cancelOnAbort = () => {
		void cancelOrderRefreshJob(start.job_id, { timeoutMs: orderRefreshCancelTimeoutMs }).catch(/* 取消请求失败时忽略网络错误，主请求仍按取消语义结束。 */ () => undefined);
	};
	options?.signal?.addEventListener('abort', cancelOnAbort, { once: true });
	// pollLimit 限制前端等待后台任务的轮询次数。
	const pollLimit = options?.pollLimit ?? orderRefreshPollLimit;
	// pollIntervalMs 是本次状态查询之间的等待时长，测试可缩短它验证超时取消而不改变默认产品体验。
	const pollIntervalMs = options?.pollIntervalMs ?? 500;
	// pollIndex 表示当前订单刷新任务状态轮询次数。
	let pollIndex = 0;
	try {
		while (pollIndex < pollLimit) {
		// job 表示当前轮询得到的后台任务状态。
		const job = await runContractRequest(/* signal 是本次订单刷新状态轮询请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/orders/refresh/{job_id}', {
      params: { path: { job_id: start.job_id } },
      signal,
    }), options);
		if (job.status === 'succeeded' && job.result) {
			return job.result;
		}
		if (job.status === 'failed' || job.status === 'cancelled') {
			throw new Error(job.error_message || '订单刷新任务失败');
		}
		// waitMs 是下一次任务状态轮询前的等待时间。
			const waitMs = pollIntervalMs;
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
	// finalJob 保存取消命令与终态竞争后的最终任务状态；成功或失败终态优先于“等待超时”展示。
	const finalJob = await cancelAndReadOrderRefreshJob(start.job_id);
	if (finalJob.status === 'succeeded' && finalJob.result) {
		return finalJob.result;
	}
	if (finalJob.status === 'failed') {
		throw new Error(finalJob.error_message || '订单刷新任务失败');
	}
	throw new Error('订单刷新任务等待超时，已请求取消');
};

/** 取消订单刷新任务后读取一次独立终态，解决取消响应和 worker 完成响应同时到达的竞态。 */
const cancelAndReadOrderRefreshJob = async (jobId: string): Promise<OrderRefreshJobStatusResponse> => {
	try {
		await cancelOrderRefreshJob(jobId, { timeoutMs: orderRefreshCancelTimeoutMs });
	} catch {
		// 取消返回冲突或网络错误时仍读取终态：任务可能已经在取消命令到达前结束。
	}
	return runContractRequest(/* signal 是本次订单刷新终态查询请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/orders/refresh/{job_id}', {
    params: { path: { job_id: jobId } },
    signal,
  }), { timeoutMs: orderRefreshCancelTimeoutMs });
};

// cancelOrderRefreshJob 请求取消当前用户的订单刷新后台任务。
export const cancelOrderRefreshJob = async (jobId: string, options?: RequestControlOptions): Promise<OrderRefreshJobCancelResponse> => {
	return runContractRequest(/* signal 是本次订单刷新取消请求的超时与取消控制信号。 */ signal => contractClient.DELETE('/api/v1/orders/refresh/{job_id}', {
    params: { path: { job_id: jobId } },
    signal,
  }), options);
};

// syncSingleOrder 同步单个订单。
export const syncSingleOrder = async (orderId: string): Promise<OrderSingleRefreshResponse> => {
  return runContractRequest(/* signal 是本次单订单刷新请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/orders/{order_id}/refresh', {
    params: { path: { order_id: orderId } },
    signal,
  }));
};

// manualShipOrder 手动发货订单。
export const manualShipOrder = async (orderIds: string[], shipMode: 'status_only' | 'full_delivery'): Promise<OrderBatchResponse> => {
    // response 是生成契约约束的批量发货 transport DTO，随后归一化为旧 UI 模型。
    const response = await runContractRequest(/* signal 是本次批量发货请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/orders/manual-ship', {
      body: {
        order_ids: orderIds,
        ship_mode: shipMode,
      },
      signal,
    }));
    return normalizeOrderBatchResponse(response);
}

// importOrders 导入订单。
export const importOrders = async (data: Partial<Order>[] | FormData, options?: RequestControlOptions): Promise<OrderBatchResponse> => {
	// isFormData 是否为表单请求，用于当前 API 处理流程。
	const isFormData = data instanceof FormData;
	if (isFormData) {
		// response 是生成契约约束的文件导入 transport DTO。
		const response = await runContractRequest(/* signal 是本次订单表格导入请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/orders/import', {
			body: data as unknown as OrderImportFormContract,
			signal,
		}), options);
		return normalizeOrderBatchResponse(response);
	}
	// response 是生成契约约束的 JSON 导入 transport DTO。
	const response = await runContractRequest(/* signal 是本次订单 JSON 导入请求的超时与取消控制信号。 */ signal => contractClient.POST('/api/v1/orders/import', {
		body: data,
		signal,
	}), options);
	return normalizeOrderBatchResponse(response);
}

// normalizeOrderBatchResponse 将生成 transport DTO 转为兼容历史订单页面的受限结果状态。
const normalizeOrderBatchResponse = (response: OrderBatchTransportResponse): OrderBatchResponse => ({
  partial_failure: response.partial_failure,
  message: response.message,
  total: response.total,
  success_count: response.success_count,
  failed_count: response.failed_count,
  results: (response.results || []).map(/* item 是当前待归一化的批量订单结果 DTO。 */ item => ({
    ...item,
    status: item.status === 'failed' || item.status === 'succeeded' || item.status === 'reconciliation_required' ? item.status : undefined,
  })),
});
