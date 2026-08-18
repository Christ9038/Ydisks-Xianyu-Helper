import type { ApiErrorResponse } from '../api-contract/common';

type RequestMethod = 'GET' | 'POST' | 'PUT' | 'DELETE';

type QueryParams = Record<string, string | number | boolean | undefined | null>;

type JsonValue = unknown;

export type RequestControlOptions = {
// signal 表示signal。
    signal?: AbortSignal;
// timeoutMs 表示timeoutMs。
    timeoutMs?: number;
};

type RequestOptions = {
// params 表示params。
    params?: QueryParams;
// body 表示请求体。
    body?: JsonValue;
// skipAuthLogout 表示skipAuthLogout。
    skipAuthLogout?: boolean;
} & RequestControlOptions;

const defaultRequestTimeoutMs = 30_000; /* defaultRequestTimeoutMs 表示default接口请求对象TimeoutMs。 */
const uploadRequestTimeoutMs = 10 * 60_000; /* uploadRequestTimeoutMs 表示upload接口请求对象TimeoutMs。 */

let authLogoutPending = false; /* authLogoutPending 表示authLogoutPending。 */

// ApiError 保留服务端统一错误 envelope，调用方可按稳定错误码决定重试、冲突提示或人工核对流程。
export class ApiError extends Error {
  // status 是产生错误的 HTTP 状态码，供调用方区分授权、冲突和服务端失败。
  readonly status: number;
  // code 是服务端稳定机器错误码；非结构化响应退化为 http_<status>。
  readonly code: string;
  // requestId 是服务端请求追踪标识，用户反馈问题时可安全提供给运维。
  readonly requestId?: string;
  // request_id 保持服务端错误契约的原始字段名，供需要透传原始 HTTP 语义的 feature adapter 使用。
  readonly request_id?: string;
  // details 是服务端提供的结构化恢复或审计信息，不包含客户端主动拼接的兼容字段。
  readonly details?: Record<string, unknown>;
  // payload 是保留给既有上传流程和诊断代码的原始响应载荷。
  readonly payload: unknown;

  // constructor 根据 HTTP 响应和统一错误载荷构造可被所有请求路径复用的异常实例。
  constructor(status: number, payload: unknown) {
    super(errorMessageFromPayload(payload, status));
    this.name = 'ApiError';
    this.status = status;
    this.code = isApiErrorResponse(payload) ? payload.code : `http_${status}`;
    this.requestId = isApiErrorResponse(payload) ? payload.request_id : undefined;
    this.request_id = this.requestId;
    this.details = isApiErrorResponse(payload) ? payload.details : undefined;
    this.payload = payload;
  }
}

const notifyAuthExpired = () => {
  if (authLogoutPending || typeof window === 'undefined') return;
  authLogoutPending = true;
  window.dispatchEvent(new Event('auth:logout'));
  queueMicrotask(() => {
    authLogoutPending = false;
  } /* 微任务结束后允许下一次未授权响应重新触发登出通知。 */);
}; /* notifyAuthExpired 合并同一批 401 响应，避免重复触发会话清理。 */

const buildQueryString = (params?: QueryParams): string => {
  if (!params) return '';
  const searchParams = new URLSearchParams(); /* searchParams 表示searchParams。 */
  for (const [key, rawVal] /* [key, rawVal] 表示keyrawVal。 */ of Object.entries(params)) {
    if (rawVal === undefined || rawVal === null) continue;
    searchParams.set(key, String(rawVal));
  }
  const qs = searchParams.toString(); /* qs 表示qs。 */
  return qs ? `?${qs}` : '';
}; /* buildQueryString 表示buildQueryString。 */

const request = async <T>(method: RequestMethod, url: string, options: RequestOptions = {}): Promise<T> => {
  const qs = buildQueryString(options.params); /* qs 表示qs。 */
  const fullUrl = `${url}${qs}`; /* fullUrl 表示full请求地址。 */

	const control = controlledSignal(options.signal, options.timeoutMs ?? defaultRequestTimeoutMs); /* control 表示control。 */
	let res: Response; /* res 表示接口响应结果。 */
	try {
	  res = await fetch(fullUrl, {
		method,
		credentials: 'include',
		signal: control.signal,
		headers: {
		  ...(options.body === undefined ? {} : { 'Content-Type': 'application/json' }),
		},
		body: options.body === undefined ? undefined : JSON.stringify(options.body),
	  });
	} catch (error /* error 表示当前操作返回的错误。 */) {
	  if (control.signal.aborted) {
		throw new Error(options.signal?.aborted ? '请求已取消' : '请求超时，请稍后重试');
	  }
	  throw error;
	} finally {
	  control.cleanup();
	}

  const contentType = res.headers.get('content-type') || ''; /* contentType 表示contentType。 */
  const isJson = contentType.includes('application/json'); /* isJson 表示isJson。 */

  if (!res.ok) {
    if (res.status === 401 && !options.skipAuthLogout) notifyAuthExpired();
    // payload 是后端统一错误 DTO 或非 JSON 错误文本。
    const payload = await readResponsePayload(res, isJson);
    throw new ApiError(res.status, payload);
  }

  if (!isJson) {
    // 这里按现有后端习惯基本都会返回JSON；非JSON时直接返回text
    return (await res.text()) as unknown as T;
  }

  return (await res.json()) as T;
}; /* request 表示接口请求对象。 */

export const get = async <T>(url: string, params?: QueryParams, options?: RequestControlOptions): Promise<T> => request<T>('GET', url, { params, ...options }); /* get 表示get。 */
export const post = async <T>(url: string, body?: JsonValue, options?: RequestControlOptions & { /* skipAuthLogout 表示skipAuthLogout。 */ skipAuthLogout?: boolean }): Promise<T> => request<T>('POST', url, { body, ...options }); /* post 表示post。 */
export const put = async <T>(url: string, body?: JsonValue, options?: RequestControlOptions): Promise<T> => request<T>('PUT', url, { body, ...options }); /* put 表示put。 */
export const del = async <T>(url: string, params?: QueryParams, options?: RequestControlOptions): Promise<T> => request<T>('DELETE', url, { params, ...options }); /* del 表示del。 */

export const postForm = async <T>(url: string, body: FormData, options: RequestControlOptions = {}): Promise<T> => {
	const control = controlledSignal(options.signal, options.timeoutMs ?? uploadRequestTimeoutMs); /* control 表示control。 */
	let res: Response; /* res 表示接口响应结果。 */
	try {
	  res = await fetch(url, {
		method: 'POST',
		credentials: 'include',
		signal: control.signal,
		body,
	  });
	} catch (error /* error 表示当前操作返回的错误。 */) {
	  if (control.signal.aborted) {
		throw new Error(options.signal?.aborted ? '请求已取消' : '上传超时，请稍后重试');
	  }
	  throw error;
	} finally {
	  control.cleanup();
	}

  const contentType = res.headers.get('content-type') || ''; /* contentType 表示contentType。 */
  const isJson = contentType.includes('application/json'); /* isJson 表示isJson。 */
  const payload = await readResponsePayload(res, isJson); /* payload 是上传接口返回的成功或错误载荷。 */

  if (!res.ok) {
    if (res.status === 401) notifyAuthExpired();
    throw new ApiError(res.status, payload);
  }

  return payload as T;
}; /* postForm 表示postForm。 */

const controlledSignal = (external: AbortSignal | undefined, timeoutMs: number) => {
	const controller = new AbortController(); /* controller 表示controller。 */
	const abortFromExternal = () => controller.abort(external?.reason); /* abortFromExternal 表示abortFromExternal。 */
	if (external?.aborted) abortFromExternal();
	else external?.addEventListener('abort', abortFromExternal, { once: true });
	const timer = globalThis.setTimeout(() => controller.abort(new DOMException('timeout', 'TimeoutError')) /* 到达请求时限后主动取消底层请求。 */, Math.max(1, timeoutMs)); /* timer 保存请求超时定时器，完成或取消时必须清理。 */
	return {
	  signal: controller.signal,
	  cleanup: () => {
		globalThis.clearTimeout(timer);
		external?.removeEventListener('abort', abortFromExternal);
	  } /* 外部取消或请求结束时移除监听并清理超时定时器。 */,
	};
}; /* controlledSignal 表示controlledSignal。 */

// readResponsePayload 按响应类型读取一次响应体；解析失败返回 undefined，由 ApiError 生成稳定回退信息。
const readResponsePayload = async (response: Response, isJson: boolean): Promise<unknown> => (
  isJson
    ? response.json().catch(() => undefined /* 错误响应 JSON 无法解析时交由状态码兜底。 */)
    : response.text().catch(() => undefined /* 错误响应文本无法读取时交由状态码兜底。 */)
);

/** 判断响应体是否符合统一 HTTP 错误 DTO。 */
const isApiErrorResponse = (payload: unknown): payload is ApiErrorResponse => {
  if (typeof payload !== 'object' || payload === null) return false;
  // candidate 是待校验的未知 JSON 对象视图。
  const candidate = payload as Record<string, unknown>;
  return typeof candidate.code === 'string' && typeof candidate.message === 'string';
};

/** 从统一错误 DTO 提取用户可见消息，拒绝继续依赖 detail 或 msg 别名。 */
const errorMessageFromPayload = (payload: unknown, status: number): string => {
  if (typeof payload === 'string') return payload;
  if (isApiErrorResponse(payload)) return payload.message;
  return `请求失败: ${status}`;
};
