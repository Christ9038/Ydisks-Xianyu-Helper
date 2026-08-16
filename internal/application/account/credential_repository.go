package account

import (
	"context"
	"errors"
)

// ErrCredentialNotFound 表示平台凭证查询目标账号不存在；调用方无需依赖数据库错误类型。
var ErrCredentialNotFound = errors.New("平台凭证账号不存在")

// CredentialDetail 是账号凭证流程需要的最小平台视图；不包含登录用户名、密码或其他秘密。
type CredentialDetail struct {
	// ID 是账号稳定标识，用于凭证更新和运行时关联。
	ID string
	// UserID 是账号所属本地用户标识，用于所有权复核。
	UserID int64
	// Value 是仅在平台调用边界短暂存在的 Cookie 明文，禁止进入日志、HTTP DTO 或长期应用状态。
	Value string
	// MetadataJSON 是由数据库适配器管理的加密 Cookie 元数据，不应由 Server 解析或序列化。
	MetadataJSON string
	// ShowBrowser 表示平台登录流程是否允许显示浏览器。
	ShowBrowser bool
	// LastRefreshAt 是客户端乐观冲突检查使用的最近刷新时间，单位为 Unix 秒。
	LastRefreshAt int64
}

// CredentialRepository 定义账号 Cookie 登录与平台运行态更新所需的最小持久化端口。
type CredentialRepository interface {
	// LockCredentials 串行化同一账号的凭证写入和平台资料会话。
	LockCredentials(string) func()
	// CreateCookieOwned 原子校验账号归属并创建 Cookie；明文只在适配器内部进入加密存储。
	CreateCookieOwned(context.Context, string, string, int64) error
	// LoadPlatformDetail 读取平台调用所需的窄视图，不读取登录密码。
	LoadPlatformDetail(context.Context, string) (*CredentialDetail, error)
	// UpdateFlatCookieOwned 更新扁平 Cookie 并清除旧的完整快照。
	UpdateFlatCookieOwned(context.Context, *CredentialDetail, string) error
	// UpdateRenewalCookie 保存平台返回的 Cookie 与 metadata；明文只在凭证适配器内短暂存在。
	UpdateRenewalCookie(context.Context, string, string, string, int64) error
	// ClearTokens 清理凭证变化后失效的旧连接 Token。
	ClearTokens(context.Context, string) error
	// GetStatus 查询账号是否启用；数据库故障按停用处理。
	GetStatus(context.Context, string) bool
	// UpdateProfile 保存不含凭证的账号展示资料。
	UpdateProfile(context.Context, string, string, string) error
}
