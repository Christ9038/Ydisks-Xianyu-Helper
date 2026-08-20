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

// contractFetch 为 openapi-fetch 注入浏览器 Cookie；必须原样转发 Request，避免重新编码 FormData 后令 body boundary 与请求头失配。
export const contractFetch: typeof fetch = (input, init) => fetch(input, { ...init, credentials: 'include' });

// contractClient 是唯一持有生成 paths 类型并执行版本化 HTTP operation 的共享实例。
// contractBaseUrl 是浏览器生产环境的当前 origin；Node 测试使用可被 fetch fixture 还原的占位 origin。
const contractBaseUrl = typeof location === 'undefined' ? 'http://localhost' : location.origin;

export const contractClient = createClient<paths>({ baseUrl: contractBaseUrl, fetch: contractFetch });

// contractMultipartBody 将原生 FormData 作为对应 OpenAPI multipart operation 的运行时载荷交给客户端。
// OpenAPI 3.1 的 binary 字段会生成 string 类型，而浏览器必须保留 FormData 边界和 File 对象；泛型仅由调用位置的生成 operation 请求体推导。
export function contractMultipartBody<T>(form: FormData): NonNullable<T> {
  return form as unknown as NonNullable<T>;
}

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
