// BuildInfo 描述健康检查接口返回的构建版本信息。
export interface BuildInfo {
  // version 是当前服务构建版本。
  version: string;
  // commit 是当前服务对应的提交标识。
  commit: string;
}
