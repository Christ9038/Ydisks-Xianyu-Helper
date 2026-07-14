type RequestMethod = 'GET' | 'POST' | 'PUT' | 'DELETE';

type QueryParams = Record<string, string | number | boolean | undefined | null>;

type JsonValue = unknown;

export type RequestControlOptions = {
  signal?: AbortSignal;
  timeoutMs?: number;
};

type RequestOptions = {
  params?: QueryParams;
  body?: JsonValue;
  skipAuthLogout?: boolean;
} & RequestControlOptions;

const defaultRequestTimeoutMs = 30_000;
const uploadRequestTimeoutMs = 10 * 60_000;

let authLogoutPending = false;

const notifyAuthExpired = () => {
  if (authLogoutPending || typeof window === 'undefined') return;
  authLogoutPending = true;
  window.dispatchEvent(new Event('auth:logout'));
  queueMicrotask(() => {
    authLogoutPending = false;
  });
};

const buildQueryString = (params?: QueryParams): string => {
  if (!params) return '';
  const searchParams = new URLSearchParams();
  for (const [key, rawVal] of Object.entries(params)) {
    if (rawVal === undefined || rawVal === null) continue;
    searchParams.set(key, String(rawVal));
  }
  const qs = searchParams.toString();
  return qs ? `?${qs}` : '';
};

const request = async <T>(method: RequestMethod, url: string, options: RequestOptions = {}): Promise<T> => {
  const qs = buildQueryString(options.params);
  const fullUrl = `${url}${qs}`;

	const control = controlledSignal(options.signal, options.timeoutMs ?? defaultRequestTimeoutMs);
	let res: Response;
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
	} catch (error) {
	  if (control.signal.aborted) {
		throw new Error(options.signal?.aborted ? '请求已取消' : '请求超时，请稍后重试');
	  }
	  throw error;
	} finally {
	  control.cleanup();
	}

  const contentType = res.headers.get('content-type') || '';
  const isJson = contentType.includes('application/json');

  if (!res.ok) {
    if (res.status === 401 && !options.skipAuthLogout) notifyAuthExpired();
    // 尽量返回后端的detail/message，避免吞错
    const payload = isJson ? await res.json().catch(() => undefined) : await res.text().catch(() => undefined);
    const detail = typeof payload === 'string' ? payload : (payload?.detail || payload?.message || payload?.msg);
    throw new Error(detail || `请求失败: ${res.status}`);
  }

  if (!isJson) {
    // 这里按现有后端习惯基本都会返回JSON；非JSON时直接返回text
    return (await res.text()) as unknown as T;
  }

  return (await res.json()) as T;
};

export const get = async <T>(url: string, params?: QueryParams, options?: RequestControlOptions): Promise<T> => request<T>('GET', url, { params, ...options });
export const post = async <T>(url: string, body?: JsonValue, options?: RequestControlOptions & { skipAuthLogout?: boolean }): Promise<T> => request<T>('POST', url, { body, ...options });
export const put = async <T>(url: string, body?: JsonValue, options?: RequestControlOptions): Promise<T> => request<T>('PUT', url, { body, ...options });
export const del = async <T>(url: string, params?: QueryParams, options?: RequestControlOptions): Promise<T> => request<T>('DELETE', url, { params, ...options });

export const postForm = async <T>(url: string, body: FormData, options: RequestControlOptions = {}): Promise<T> => {
	const control = controlledSignal(options.signal, options.timeoutMs ?? uploadRequestTimeoutMs);
	let res: Response;
	try {
	  res = await fetch(url, {
		method: 'POST',
		credentials: 'include',
		signal: control.signal,
		body,
	  });
	} catch (error) {
	  if (control.signal.aborted) {
		throw new Error(options.signal?.aborted ? '请求已取消' : '上传超时，请稍后重试');
	  }
	  throw error;
	} finally {
	  control.cleanup();
	}

  const contentType = res.headers.get('content-type') || '';
  const isJson = contentType.includes('application/json');
  const payload = isJson ? await res.json().catch(() => undefined) : await res.text().catch(() => undefined);

  if (!res.ok) {
    if (res.status === 401) notifyAuthExpired();
    const detail = typeof payload === 'string' ? payload : (payload?.detail || payload?.message || payload?.msg);
    const err = new Error(detail || ('请求失败: ' + res.status)) as Error & { payload?: unknown };
    err.payload = payload;
    throw err;
  }

  return payload as T;
};

const controlledSignal = (external: AbortSignal | undefined, timeoutMs: number) => {
	const controller = new AbortController();
	const abortFromExternal = () => controller.abort(external?.reason);
	if (external?.aborted) abortFromExternal();
	else external?.addEventListener('abort', abortFromExternal, { once: true });
	const timer = globalThis.setTimeout(() => controller.abort(new DOMException('timeout', 'TimeoutError')), Math.max(1, timeoutMs));
	return {
	  signal: controller.signal,
	  cleanup: () => {
		globalThis.clearTimeout(timer);
		external?.removeEventListener('abort', abortFromExternal);
	  },
	};
};
