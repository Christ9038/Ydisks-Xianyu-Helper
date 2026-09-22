// Package updater 实现 Linux 宿主机上的受限 Docker 更新协议与事务。
//
// 该包只允许更新固定的闲鱼助手 Compose app 服务到官方最新稳定版本，
// 不接受调用方提供的路径、命令、服务名或镜像地址。
package updater

import "time"

const (
	// DefaultSocketPath 是应用容器与宿主机 updater 通信的固定 Unix Socket 路径。
	DefaultSocketPath = "/run/ydisks-xianyu-helper/updater.sock"
	// OfficialImage 是允许 updater 拉取和激活的唯一官方镜像仓库。
	OfficialImage = "ghcr.io/christ9038/ydisks-xianyu-helper"
)

// DeploymentKind 描述当前运行镜像能否安全进入官方稳定版更新通道。
type DeploymentKind string

const (
	// DeploymentStable 表示当前镜像来自官方仓库并使用明确稳定版本或摘要。
	DeploymentStable DeploymentKind = "stable"
	// DeploymentStableUnpinned 表示当前镜像来自官方 latest，但运行版本仍可验证为正式版本。
	DeploymentStableUnpinned DeploymentKind = "stable_unpinned"
	// DeploymentDevelopment 表示当前镜像来自 dev、main 或 SHA 开发通道。
	DeploymentDevelopment DeploymentKind = "dev"
	// DeploymentCustom 表示当前镜像来自本地标签或非官方仓库。
	DeploymentCustom DeploymentKind = "custom"
	// DeploymentUnknown 表示镜像来源或构建版本不足以做安全判断。
	DeploymentUnknown DeploymentKind = "unknown"
)

// BuildInfo 保存应用健康接口公开的非敏感构建标识。
type BuildInfo struct {
	// Status 是应用总体健康状态。
	Status string `json:"status"`
	// Database 是数据库连通状态。
	Database string `json:"database"`
	// Version 是构建时注入的版本字符串。
	Version string `json:"version"`
	// Commit 是构建时注入的提交标识。
	Commit string `json:"commit"`
	// BuildTime 是构建时注入的 UTC 或流水线时间字符串。
	BuildTime string `json:"build_time"`
}

// ReleaseManifest 是正式 Release 附件中声明的不可变 Docker 更新目标。
type ReleaseManifest struct {
	// Schema 是 manifest 契约版本，第一版固定为 1。
	Schema int `json:"schema"`
	// Version 是不带 v 前缀的稳定语义版本。
	Version string `json:"version"`
	// Tag 是与 GitHub Release 一致的 vX.Y.Z 标签。
	Tag string `json:"tag"`
	// Commit 是正式标签指向的完整 Git 提交号。
	Commit string `json:"commit"`
	// Image 是固定官方 GHCR 仓库，不允许由 API 调用方覆盖。
	Image string `json:"image"`
	// ManifestDigest 是多架构 OCI index 的不可变 sha256 摘要。
	ManifestDigest string `json:"manifest_digest"`
	// PublishedAt 是正式版本发布时间。
	PublishedAt time.Time `json:"published_at"`
	// Platforms 是发布流程验证并声明支持的平台列表。
	Platforms []string `json:"platforms"`
	// ReleaseURL 是 GitHub Release 页面地址，由检查器从 GitHub API 补充。
	ReleaseURL string `json:"release_url,omitempty"`
}

// ImageReference 返回固定仓库与不可变摘要组成的拉取引用。
func (manifest ReleaseManifest) ImageReference() string {
	return manifest.Image + "@" + manifest.ManifestDigest
}

// CurrentDeployment 描述 updater 从 Compose、Docker 和健康接口交叉确认的当前部署。
type CurrentDeployment struct {
	// ImageReference 是 Compose 当前声明的 app 镜像引用。
	ImageReference string `json:"image_reference"`
	// ImageID 是当前容器实际运行的本地镜像 ID。
	ImageID string `json:"image_id"`
	// RepositoryDigests 是该本地镜像已知的远端不可变摘要。
	RepositoryDigests []string `json:"repository_digests,omitempty"`
	// ContainerHealth 是 Docker 当前记录的容器健康状态。
	ContainerHealth string `json:"container_health"`
	// Build 是应用自身健康接口返回的构建信息。
	Build BuildInfo `json:"build"`
	// Kind 是依据镜像来源、标签、摘要和版本共同判定的部署类型。
	Kind DeploymentKind `json:"deployment_kind"`
	// DatabaseKind 是 sqlite、mysql、postgres 或 unknown，不包含连接地址和凭据。
	DatabaseKind string `json:"database_kind"`
	// DataMountSupported 表示 SQLite 数据目录是 updater 固定支持的宿主机绑定目录。
	DataMountSupported bool `json:"data_mount_supported"`
}

// CheckResult 描述最新稳定版本与当前部署之间的安全更新结论。
type CheckResult struct {
	// Current 是当前实际运行部署的只读快照。
	Current CurrentDeployment `json:"current"`
	// Latest 是 GitHub 最新稳定 Release 的已验证 manifest。
	Latest ReleaseManifest `json:"latest"`
	// Available 表示存在与当前版本不同的正式版本；custom 构建也可看到提示。
	Available bool `json:"available"`
	// CanUpdate 表示当前部署满足自动切换到官方稳定版的全部前置条件。
	CanUpdate bool `json:"can_update"`
	// BlockedReason 是不能自动更新时供 UI 稳定识别的机器可读原因。
	BlockedReason string `json:"blocked_reason,omitempty"`
}

// OperationStatus 描述一次异步更新事务的生命周期状态。
type OperationStatus string

const (
	// OperationIdle 表示 daemon 启动后尚未受理更新。
	OperationIdle OperationStatus = "idle"
	// OperationQueued 表示请求已受理但后台事务尚未取得文件锁。
	OperationQueued OperationStatus = "queued"
	// OperationRunning 表示更新事务正在备份、预检或切换。
	OperationRunning OperationStatus = "running"
	// OperationSucceeded 表示新镜像已通过生产健康检查。
	OperationSucceeded OperationStatus = "succeeded"
	// OperationFailed 表示更新未触碰生产或回滚也未能完整恢复。
	OperationFailed OperationStatus = "failed"
	// OperationRolledBack 表示新版本切换失败但旧镜像和数据库已恢复。
	OperationRolledBack OperationStatus = "rolled_back"
)

// OperationState 是 status API 暴露的非敏感更新进度。
type OperationState struct {
	// RequestID 是 daemon 生成的单次操作标识，不包含用户输入。
	RequestID string `json:"request_id,omitempty"`
	// Status 是事务生命周期状态。
	Status OperationStatus `json:"status"`
	// Phase 是当前执行阶段的稳定英文标识。
	Phase string `json:"phase,omitempty"`
	// TargetVersion 是本次事务准备安装的稳定版本。
	TargetVersion string `json:"target_version,omitempty"`
	// Message 是不包含路径、命令输出或秘密的用户可读结果。
	Message string `json:"message,omitempty"`
	// StartedAt 是事务开始时间。
	StartedAt *time.Time `json:"started_at,omitempty"`
	// FinishedAt 是成功、失败或回滚完成时间。
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// StatusResponse 汇总当前部署和 daemon 内存中的最近更新状态。
type StatusResponse struct {
	// Current 是当前部署；探测失败时保持空值并由 Error 解释。
	Current *CurrentDeployment `json:"current,omitempty"`
	// Operation 是 daemon 拥有的最近一次异步操作状态。
	Operation OperationState `json:"operation"`
	// Latest 是最近一次成功检查到的正式版本。
	Latest *ReleaseManifest `json:"latest,omitempty"`
	// Available 表示最近一次检查发现更新。
	Available bool `json:"available"`
	// CanUpdate 表示当前部署允许执行自动更新。
	CanUpdate bool `json:"can_update"`
	// BlockedReason 是当前策略阻断自动更新的稳定原因码。
	BlockedReason string `json:"blocked_reason,omitempty"`
	// Error 是当前部署探测失败时的脱敏说明。
	Error string `json:"error,omitempty"`
}

// ApplyResponse 是 apply_latest_stable 成功受理后的响应。
type ApplyResponse struct {
	// Accepted 表示请求已进入 daemon 后台事务队列。
	Accepted bool `json:"accepted"`
	// Operation 是刚创建的异步操作状态。
	Operation OperationState `json:"operation"`
}

// ErrorResponse 是 Unix HTTP API 的稳定错误契约。
type ErrorResponse struct {
	// Code 是调用方可分支处理的机器可读错误码。
	Code string `json:"code"`
	// Message 是不包含宿主机秘密或任意命令输出的错误说明。
	Message string `json:"message"`
}
