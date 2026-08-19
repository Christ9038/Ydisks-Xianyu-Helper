// BuildInfo 描述健康检查接口返回的构建版本信息。
export interface BuildInfo {
  // version 是当前服务构建版本。
  version: string;
  // commit 是当前服务对应的提交标识。
  commit: string;
  /** status 表示服务总体健康状态。 */
  status?: string;
  /** database 表示数据库连接状态。 */
  database?: string;
  /** build_time 表示构建时间。 */
  build_time?: string;
}
