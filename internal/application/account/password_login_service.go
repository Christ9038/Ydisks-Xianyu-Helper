package account

import (
	"context"
	"errors"
)

// ErrPasswordLoginDisabled 表示当前 Go 客户端不允许使用浏览器密码登录账号。
var ErrPasswordLoginDisabled = errors.New("密码登录已禁用")

// PasswordLoginStartInput 是密码登录启动请求的非敏感身份输入；不接收用户名、密码或 Cookie。
type PasswordLoginStartInput struct {
	// UserID 是已经通过 HTTP 鉴权的本地用户标识，供未来启用登录会话时执行归属隔离。
	UserID int64
}

// PasswordLoginSessionInput 是密码登录会话操作的非敏感输入；会话标识不包含平台凭证。
type PasswordLoginSessionInput struct {
	// UserID 是已经通过 HTTP 鉴权的本地用户标识。
	UserID int64
	// SessionID 是待查询或取消的进程内登录会话标识。
	SessionID string
}

// PasswordLoginService 编排密码登录能力策略；当前产品明确关闭浏览器密码登录。
// 浏览器 PasswordLogin 实现仍由 internal/browser 独立保留，避免绕过冻结的验证码行为。
type PasswordLoginService struct{}

// NewPasswordLoginService 构造不保存凭证和会话状态的密码登录应用服务。
func NewPasswordLoginService() *PasswordLoginService {
	return &PasswordLoginService{}
}

// Start 拒绝启动密码登录；调用方不应把请求体中的密码传入应用层。
func (s *PasswordLoginService) Start(ctx context.Context, input PasswordLoginStartInput) error {
	// ctx 和 input 预留给未来启用会话时的取消与归属校验；当前关闭策略不读取敏感请求体。
	_ = ctx
	_ = input
	return ErrPasswordLoginDisabled
}

// Check 拒绝查询密码登录会话；当前不存在可供 Go 客户端轮询的密码登录会话。
func (s *PasswordLoginService) Check(ctx context.Context, input PasswordLoginSessionInput) error {
	// ctx 和 input 仅表达调用边界，当前关闭策略不会创建或读取会话状态。
	_ = ctx
	_ = input
	return ErrPasswordLoginDisabled
}

// Cancel 拒绝取消密码登录会话；关闭策略确保不会启动浏览器或留下后台任务。
func (s *PasswordLoginService) Cancel(ctx context.Context, input PasswordLoginSessionInput) error {
	// ctx 和 input 仅表达调用边界，当前没有需要释放的密码登录资源。
	_ = ctx
	_ = input
	return ErrPasswordLoginDisabled
}
