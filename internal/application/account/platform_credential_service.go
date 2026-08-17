package account

import (
	"context"
	"errors"
)

// ErrCredentialEmpty 表示账号存在但没有可供平台调用的 Cookie。
var ErrCredentialEmpty = errors.New("账号 Cookie 为空")

// PlatformCredentialViewPort 定义读取平台调用窄凭证视图的最小消费者端口。
// 实现方负责数据库查询与解密；调用方不得把返回的明文写入摘要、日志或 HTTP DTO。
type PlatformCredentialViewPort interface {
	// LoadPlatformDetail 读取指定账号的平台 Cookie 视图，不读取登录密码。
	LoadPlatformDetail(context.Context, string) (*CredentialDetail, error)
}

// PlatformCredentialService 编排平台凭证窄视图读取和当前用户归属复核。
// 该服务只在确有平台调用需要时返回 Cookie 明文，普通所有权检查不会返回敏感值。
type PlatformCredentialService struct {
	// port 保存由适配器实现的平台凭证读取端口；服务本身不依赖数据库类型。
	port PlatformCredentialViewPort
}

// NewPlatformCredentialService 构造平台凭证只读应用服务并校验端口。
func NewPlatformCredentialService(port PlatformCredentialViewPort) (*PlatformCredentialService, error) {
	if port == nil {
		return nil, errors.New("平台凭证读取端口未初始化")
	}
	return &PlatformCredentialService{port: port}, nil
}

// LoadPlatformDetail 读取平台运行所需的窄视图；登录密码不会进入该调用链。
func (s *PlatformCredentialService) LoadPlatformDetail(ctx context.Context, accountID string) (*CredentialDetail, error) {
	if s == nil || s.port == nil {
		return nil, errors.New("平台凭证读取服务未初始化")
	}
	return s.port.LoadPlatformDetail(ctx, accountID)
}

// ValidateOwned 验证平台账号归属和 Cookie 可用性，只返回非敏感用户标识。
func (s *PlatformCredentialService) ValidateOwned(ctx context.Context, userID int64, accountID string) (int64, error) {
	// detail、loadErr 保存平台凭证窄视图及其读取错误；Cookie 只在本方法内部用于可用性判断。
	detail, loadErr := s.LoadPlatformDetail(ctx, accountID)
	if loadErr != nil {
		return 0, loadErr
	}
	if detail == nil || detail.ID == "" || detail.UserID == 0 {
		return 0, ErrCredentialNotFound
	}
	if detail.UserID != userID {
		return 0, ErrForbidden
	}
	if detail.Value == "" {
		return 0, ErrCredentialEmpty
	}
	return detail.UserID, nil
}

// LoadOwnedValue 读取已通过所有权复核的 Cookie 明文；返回值只用于当前平台请求。
func (s *PlatformCredentialService) LoadOwnedValue(ctx context.Context, userID int64, accountID string) (string, error) {
	// detail、loadErr 保存所有权复核使用的平台凭证视图及其读取错误。
	detail, loadErr := s.LoadPlatformDetail(ctx, accountID)
	if loadErr != nil {
		return "", loadErr
	}
	if detail == nil || detail.ID == "" || detail.UserID == 0 {
		return "", ErrCredentialNotFound
	}
	if detail.UserID != userID {
		return "", ErrCredentialNotFound
	}
	if detail.Value == "" {
		return "", ErrCredentialEmpty
	}
	return detail.Value, nil
}
