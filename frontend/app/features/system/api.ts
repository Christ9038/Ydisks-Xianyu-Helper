import { get, type RequestControlOptions } from '../../../shared/http/client';
import type { BuildInfo } from './types';

/** 读取服务健康检查与构建标识；请求支持随壳层卸载取消。 */
export const getHealth = async (options?: RequestControlOptions): Promise<BuildInfo> => get('/health', undefined, options);

export type { BuildInfo } from './types';
