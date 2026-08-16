package account

import (
	"context"
	"errors"
)

// ErrLongLoginPlatform 表示长登录平台请求失败；响应 Cookie 仍可能已经被适配器持久化。
var ErrLongLoginPlatform = errors.New("长登录平台请求失败")

// LongLoginResult 是长登录设置接口的非敏感结果，不携带平台 Cookie 或响应头。
type LongLoginResult struct {
	// CanOpenLongLogin 表示平台是否允许当前账号使用长登录。
	CanOpenLongLogin bool
	// Enabled 表示平台当前是否已开启长登录。
	Enabled bool
}

// LongLoginPort 定义长登录平台请求及凭证持久化所需的最小端口。
type LongLoginPort interface {
	// QueryLongLogin 查询指定账号的长登录状态，并在适配器内处理响应 Cookie。
	QueryLongLogin(context.Context, string) (LongLoginResult, error)
	// SetLongLogin 更新指定账号的长登录开关，并在适配器内处理响应 Cookie。
	SetLongLogin(context.Context, string, bool) (LongLoginResult, error)
}

// LongLoginService 编排长登录设置的账号归属校验和平台操作。
type LongLoginService struct {
	// repository 提供不解密凭证的账号归属查询能力。
	repository ProfileSummaryRepository
	// port 承担平台请求、Cookie 快照合并和凭证持久化。
	port LongLoginPort
}

// NewLongLoginService 构造长登录应用服务并校验必需端口。
func NewLongLoginService(repository ProfileSummaryRepository, port LongLoginPort) (*LongLoginService, error) {
	if repository == nil {
		return nil, errors.New("长登录账号摘要 repository 未初始化")
	}
	if port == nil {
		return nil, errors.New("长登录平台端口未初始化")
	}
	return &LongLoginService{repository: repository, port: port}, nil
}

// Query 查询当前用户拥有账号的长登录状态。
func (s *LongLoginService) Query(ctx context.Context, userID int64, accountID string) (LongLoginResult, error) {
	if s == nil || s.repository == nil || s.port == nil {
		return LongLoginResult{}, errors.New("长登录服务未初始化")
	}
	// summary 保存归属校验使用的非敏感账号摘要。
	if _, err := s.repository.GetOwnedSummary(ctx, userID, accountID); err != nil {
		return LongLoginResult{}, err
	}
	return s.port.QueryLongLogin(ctx, accountID)
}

// Set 更新当前用户拥有账号的长登录开关。
func (s *LongLoginService) Set(ctx context.Context, userID int64, accountID string, enabled bool) (LongLoginResult, error) {
	if s == nil || s.repository == nil || s.port == nil {
		return LongLoginResult{}, errors.New("长登录服务未初始化")
	}
	// summary 保存归属校验使用的非敏感账号摘要。
	if _, err := s.repository.GetOwnedSummary(ctx, userID, accountID); err != nil {
		return LongLoginResult{}, err
	}
	return s.port.SetLongLogin(ctx, accountID, enabled)
}
