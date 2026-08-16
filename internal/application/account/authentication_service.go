package account

import (
	"context"
	"errors"
)

// ErrPasswordMismatch 表示当前密码未通过校验，调用方应返回认证失败而不是基础设施错误。
var ErrPasswordMismatch = errors.New("密码错误")

// ErrUsernameTaken 表示新的用户名已经被其他用户占用。
var ErrUsernameTaken = errors.New("用户名已存在")

// AuthUser 是认证用例返回的非敏感用户身份，不包含密码哈希或邮箱等持久化字段。
type AuthUser struct {
	// ID 是本地用户的稳定身份标识，用于创建会话和归属校验。
	ID int64
	// Username 是登录用户名和会话展示名称。
	Username string
	// IsAdmin 表示用户是否拥有管理员权限。
	IsAdmin bool
}

// AuthenticationRepository 定义认证用例需要的最小用户与会话端口。
type AuthenticationRepository interface {
	// IsSystemInitialized 判断系统是否已经存在管理员账号。
	IsSystemInitialized(context.Context) (bool, error)
	// InitializeAdmin 创建或重置管理员账号，并返回本次是否新建。
	InitializeAdmin(context.Context, string, string) (bool, error)
	// UsernameByEmail 按邮箱取得登录用户名，不向应用层返回完整用户记录。
	UsernameByEmail(context.Context, string) (string, error)
	// VerifyPassword 校验用户密码并返回非敏感身份；密码错误返回 ErrPasswordMismatch 或 ok=false。
	VerifyPassword(context.Context, string, string) (AuthUser, bool, error)
	// UpdatePassword 更新指定用户名的密码并撤销旧会话。
	UpdatePassword(context.Context, string, string) (bool, error)
	// UpdateCredentials 原子更新用户名及可选密码，并撤销旧会话。
	UpdateCredentials(context.Context, int64, string, string) error
	// CreateSession 为已经通过密码校验的用户创建会话。
	CreateSession(context.Context, AuthUser) (string, error)
}

// AuthenticationService 编排登录、初始化、密码修改和登录凭据更新，不依赖 HTTP 或数据库模型。
type AuthenticationService struct {
	// repository 提供用户密码与会话的窄持久化能力。
	repository AuthenticationRepository
}

// NewAuthenticationService 构造认证应用服务并校验必需的持久化端口。
func NewAuthenticationService(repository AuthenticationRepository) (*AuthenticationService, error) {
	if repository == nil {
		return nil, errors.New("认证 repository 未初始化")
	}
	return &AuthenticationService{repository: repository}, nil
}

// IsSystemInitialized 返回系统是否已完成管理员初始化。
func (s *AuthenticationService) IsSystemInitialized(ctx context.Context) (bool, error) {
	if s == nil || s.repository == nil {
		return false, errors.New("认证服务未初始化")
	}
	return s.repository.IsSystemInitialized(ctx)
}

// InitializeAdmin 创建或重置管理员账号；初始化并发控制由 HTTP 层保留。
func (s *AuthenticationService) InitializeAdmin(ctx context.Context, email, password string) (bool, error) {
	if s == nil || s.repository == nil {
		return false, errors.New("认证服务未初始化")
	}
	return s.repository.InitializeAdmin(ctx, email, password)
}

// UsernameByEmail 取得邮箱对应的登录用户名，未找到时原样返回 repository 错误。
func (s *AuthenticationService) UsernameByEmail(ctx context.Context, email string) (string, error) {
	if s == nil || s.repository == nil {
		return "", errors.New("认证服务未初始化")
	}
	return s.repository.UsernameByEmail(ctx, email)
}

// Login 校验用户名密码并在成功后创建会话；认证失败返回空会话和 nil 错误以兼容旧 HTTP 行为。
func (s *AuthenticationService) Login(ctx context.Context, username, password string) (string, *AuthUser, error) {
	if s == nil || s.repository == nil {
		return "", nil, errors.New("认证服务未初始化")
	}
	// user、matched、verifyErr 保存密码校验得到的身份、匹配结果和基础设施错误。
	user, matched, verifyErr := s.repository.VerifyPassword(ctx, username, password)
	if verifyErr != nil {
		return "", nil, verifyErr
	}
	if !matched {
		return "", nil, nil
	}
	// sessionID、sessionErr 保存持久化会话的结果，密码不会进入会话端口。
	sessionID, sessionErr := s.repository.CreateSession(ctx, user)
	if sessionErr != nil {
		return "", nil, sessionErr
	}
	return sessionID, &user, nil
}

// VerifyPassword 校验当前用户密码并返回非敏感身份，供改密和凭据更新复用。
func (s *AuthenticationService) VerifyPassword(ctx context.Context, username, password string) (AuthUser, bool, error) {
	if s == nil || s.repository == nil {
		return AuthUser{}, false, errors.New("认证服务未初始化")
	}
	return s.repository.VerifyPassword(ctx, username, password)
}

// UpdatePassword 保存新密码并撤销旧会话，返回值保持数据库层“用户是否存在”的兼容语义。
func (s *AuthenticationService) UpdatePassword(ctx context.Context, username, password string) (bool, error) {
	if s == nil || s.repository == nil {
		return false, errors.New("认证服务未初始化")
	}
	return s.repository.UpdatePassword(ctx, username, password)
}

// UpdateCredentials 原子保存用户名及可选密码，并将用户名冲突转换为应用层稳定错误。
func (s *AuthenticationService) UpdateCredentials(ctx context.Context, userID int64, username, password string) error {
	if s == nil || s.repository == nil {
		return errors.New("认证服务未初始化")
	}
	return s.repository.UpdateCredentials(ctx, userID, username, password)
}
