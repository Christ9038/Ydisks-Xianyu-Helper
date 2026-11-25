import { getPublicJSON,type RequestControlOptions } from '../../../shared/http/client';
import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import type { BuildInfo } from './types';

/** 读取服务健康检查与构建标识；请求支持随壳层卸载取消。 */
export const getHealth = async (options?: RequestControlOptions): Promise<BuildInfo> => runContractRequest(/* signal 控制健康检查请求的取消和超时。 */ signal => contractClient.GET('/health', { signal }), options);

/** latestReleaseURL 是 GitHub 官方仓库最新正式 Release 接口（自动排除草稿与预发布）。 */
const latestReleaseURL = 'https://api.github.com/repos/Christ9038/Ydisks-Xianyu-Helper/releases/latest';

/** 读取 GitHub 最新正式 Release 的标签；响应缺少字符串标签时返回 undefined。 */
export const getLatestReleaseTag = async (options?: RequestControlOptions): Promise<string | undefined> => {
  // release 是 GitHub 返回的最新 Release 摘要，仅关心 tag_name。
  const release = await getPublicJSON<{ /** tag_name 是 Release 的 Git 标签，如 v1.2.3。 */ tag_name?: unknown }>(latestReleaseURL, options);
  return typeof release.tag_name === 'string' ? release.tag_name : undefined;
};

export type { BuildInfo } from './types';
