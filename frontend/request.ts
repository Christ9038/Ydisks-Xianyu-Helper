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

const notifyAuthExpired = () => {
  if (authLogoutPending || typeof window === 'undefined') return;
  authLogoutPending = true;
  window.dispatchEvent(new Event('auth:logout'));
  queueMicrotask(() => {
    authLogoutPending = false;
  } /* 回调函数负责当前业务流程。 */);
}; /* notifyAuthExpired 表示notifyAuthExpired。 */

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
    const payload = isJson ? await res.json().catch(() => undefined /* 回调函数负责当前业务流程。 */) : await res.text().catch(() => undefined /* 回调函数负责当前业务流程。 */);
    const message = errorMessageFromPayload(payload, res.status); // message 是统一错误 DTO 提取出的用户可见说明。
    throw new Error(message);
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
  const payload = isJson ? await res.json().catch(() => undefined /* 回调函数负责当前业务流程。 */) : await res.text().catch(() => undefined /* 回调函数负责当前业务流程。 */); /* payload 表示payload。 */

  if (!res.ok) {
    if (res.status === 401) notifyAuthExpired();
    const message = errorMessageFromPayload(payload, res.status); // message 是上传失败时统一错误 DTO 提取出的说明。
    const err = new Error(message) as Error & { /* payload 表示payload。 */ payload?: unknown }; /* err 表示当前操作返回的错误。 */
    err.payload = payload;
    throw err;
  }

  return payload as T;
}; /* postForm 表示postForm。 */

const controlledSignal = (external: AbortSignal | undefined, timeoutMs: number) => {
	const controller = new AbortController(); /* controller 表示controller。 */
	const abortFromExternal = () => controller.abort(external?.reason); /* abortFromExternal 表示abortFromExternal。 */
	if (external?.aborted) abortFromExternal();
	else external?.addEventListener('abort', abortFromExternal, { once: true });
	const timer = globalThis.setTimeout(() => controller.abort(new DOMException('timeout', 'TimeoutError')) /* 回调函数负责当前业务流程。 */, Math.max(1, timeoutMs)); /* timer 表示timer。 */
	return {
	  signal: controller.signal,
	  cleanup: () => {
		globalThis.clearTimeout(timer);
		external?.removeEventListener('abort', abortFromExternal);
	  } /* 回调函数负责当前业务流程。 */,
	};
}; /* controlledSignal 表示controlledSignal。 */

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

import type { ApiErrorResponse } from './types';
