import { type RequestControlOptions } from '../../../shared/http/client';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import type { BuildInfo } from './types';

/** 读取服务健康检查与构建标识；请求支持随壳层卸载取消。 */
export const getHealth = async (options?: RequestControlOptions): Promise<BuildInfo> => runContractRequest(/* signal 控制健康检查请求的取消和超时。 */ signal => contractClient.GET('/health', { signal }), options);

export type { BuildInfo } from './types';
