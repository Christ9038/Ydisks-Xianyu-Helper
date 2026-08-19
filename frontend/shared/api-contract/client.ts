import createClient from 'openapi-fetch';

import { ApiError, controlledSignal, notifyAuthExpired, type RequestControlOptions } from '../http/client';

import type { paths } from './generated/schema';

// contractRequestTimeoutMs 是非上传契约请求沿用的默认超时，保持旧 HTTP client 的用户体验。
const contractRequestTimeoutMs = 30_000;

// ContractRequestOptions 是类型化 operation 运行时使用的取消、超时与登录态控制参数。
export type ContractRequestOptions = RequestControlOptions & {
  // skipAuthLogout 防止登录和首次初始化失败时错误清理现有页面状态。
  skipAuthLogout?: boolean;
};

// ContractResult 是 openapi-fetch 对单个 operation 的成功数据、错误数据和原始响应封装。
type ContractResult<T> = {
  // data 是成功状态码且符合 OpenAPI 响应类型时解析出的数据。
  data?: T;
  // error 是非成功状态码解析出的统一错误 envelope 或未知载荷。
  error?: unknown;
  // response 是保留状态码和响应头的底层响应对象。
  response: Response;
};

// contractFetch 为 openapi-fetch 注入现有浏览器 Cookie 与请求地址兼容语义。
const contractFetch: typeof fetch = async (input, init) => {
  // response 是使用既有 Cookie 策略发出的 HTTP 响应。
  // requestURL 是 openapi-fetch 根据 baseUrl 生成的绝对地址；测试环境需还原为相对路径以复用既有 fetch fixture。
  const requestURL = new URL(typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url);
  // request 是合并 openapi-fetch 已构造请求与可选 init 后的完整请求对象。
  const request = input instanceof Request ? input : new Request(input, init);
  // body 是恢复为文本的 JSON 请求载荷；GET/HEAD 不能携带请求体。
  const body = request.method === 'GET' || request.method === 'HEAD' ? undefined : await request.clone().text();
  // fetchInput 是测试环境还原后的相对 API 地址，浏览器环境继续保留绝对 origin。
  const fetchInput = requestURL.origin === 'http://localhost' ? `${requestURL.pathname}${requestURL.search}` : input;
  // response 是使用旧 client 等价方法、请求体、取消信号和 Cookie 策略发出的 HTTP 响应。
  const response = await fetch(fetchInput, {
    method: request.method,
    headers: request.headers,
    body,
    signal: request.signal,
    credentials: 'include',
  });
  return response;
};

// contractClient 是唯一持有生成 paths 类型并执行版本化 HTTP operation 的共享实例。
// contractBaseUrl 是浏览器生产环境的当前 origin；Node 测试使用可被 fetch fixture 还原的占位 origin。
const contractBaseUrl = typeof location === 'undefined' ? 'http://localhost' : location.origin;

export const contractClient = createClient<paths>({ baseUrl: contractBaseUrl, fetch: contractFetch });

// runContractRequest 执行类型化 operation，并恢复旧 client 的超时、取消和 ApiError 行为。
export async function runContractRequest<T>(
  execute: (signal: AbortSignal) => Promise<ContractResult<T>>,
  options: ContractRequestOptions = {},
): Promise<T> {
  // control 统一管理外部 AbortSignal 与默认请求超时。
  const control = controlledSignal(options.signal, options.timeoutMs ?? contractRequestTimeoutMs);
  try {
    // result 是 openapi-fetch 返回的成功或错误响应封装。
    const result = await execute(control.signal);
    if (result.response.status === 401 && !options.skipAuthLogout) notifyAuthExpired();
    if (result.error !== undefined) {
      throw new ApiError(result.response.status, result.error);
    }
    if (result.data === undefined) {
      throw new ApiError(result.response.status, undefined);
    }
    return result.data;
  } catch (error /* error 是底层 fetch、响应解析或 ApiError 抛出的原始失败原因。 */) {
	// error 是底层 fetch、响应解析或 ApiError 抛出的原始失败原因。
    if (control.signal.aborted) {
      throw new Error(options.signal?.aborted ? '请求已取消' : '请求超时，请稍后重试');
    }
    throw error;
  } finally {
    control.cleanup();
  }
}
