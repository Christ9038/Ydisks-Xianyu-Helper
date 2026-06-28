type RequestMethod = 'GET' | 'POST' | 'PUT' | 'DELETE';

type QueryParams = Record<string, string | number | boolean | undefined | null>;

type JsonValue = unknown;

type RequestOptions = {
  params?: QueryParams;
  body?: JsonValue;
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

  const res = await fetch(fullUrl, {
    method,
    credentials: 'include',
    headers: {
      ...(options.body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });

  const contentType = res.headers.get('content-type') || '';
  const isJson = contentType.includes('application/json');

  if (!res.ok) {
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

export const get = async <T>(url: string, params?: QueryParams): Promise<T> => request<T>('GET', url, { params });
export const post = async <T>(url: string, body?: JsonValue): Promise<T> => request<T>('POST', url, { body });
export const put = async <T>(url: string, body?: JsonValue): Promise<T> => request<T>('PUT', url, { body });
export const del = async <T>(url: string, params?: QueryParams): Promise<T> => request<T>('DELETE', url, { params });

export const postForm = async <T>(url: string, body: FormData): Promise<T> => {
  const res = await fetch(url, {
    method: 'POST',
    credentials: 'include',
    body,
  });

  const contentType = res.headers.get('content-type') || '';
  const isJson = contentType.includes('application/json');
  const payload = isJson ? await res.json().catch(() => undefined) : await res.text().catch(() => undefined);

  if (!res.ok) {
    const detail = typeof payload === 'string' ? payload : (payload?.detail || payload?.message || payload?.msg);
    const err = new Error(detail || ('请求失败: ' + res.status)) as Error & { payload?: unknown };
    err.payload = payload;
    throw err;
  }

  return payload as T;
};
