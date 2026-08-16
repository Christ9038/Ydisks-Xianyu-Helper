package items

import (
	"context"
	"errors"
	"strings"
)

// SyncErrorKind 标识商品同步失败发生的基础设施阶段。
type SyncErrorKind string

const (
	// SyncErrorPlatform 表示平台商品接口或详情探测失败。
	SyncErrorPlatform SyncErrorKind = "platform"
	// SyncErrorPersistence 表示凭证或商品本地持久化失败。
	SyncErrorPersistence SyncErrorKind = "persistence"
	// SyncErrorCredential 表示账号凭证在同步期间发生并发变化。
	SyncErrorCredential SyncErrorKind = "credential"
)

var (
	// ErrSyncInvalidUser 表示请求缺少有效的当前用户身份。
	ErrSyncInvalidUser = errors.New("商品同步用户无效")
	// ErrSyncInvalidCookie 表示请求缺少有效的账号标识。
	ErrSyncInvalidCookie = errors.New("商品同步账号无效")
	// ErrSyncNotOwned 表示当前用户无权访问指定账号。
	ErrSyncNotOwned = errors.New("无权限操作该账号")
)

// SyncError 保留同步失败阶段，同时隐藏底层数据库和平台实现类型。
type SyncError struct {
	// Kind 表示错误发生的平台、凭证或持久化阶段。
	Kind SyncErrorKind
	// Err 保存供日志和错误链判断的底层原因。
	Err error
}

// Error 返回不包含敏感凭证的同步错误文本。
func (e *SyncError) Error() string {
	if e == nil || e.Err == nil {
		return "商品同步失败"
	}
	return e.Err.Error()
}

// Unwrap 暴露底层错误以支持 errors.Is 和 errors.As。
func (e *SyncError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// SyncQuery 描述一次商品同步请求的业务参数。
type SyncQuery struct {
	// UserID 是当前认证用户的本地标识。
	UserID int64
	// CookieID 是平台账号的稳定标识。
	CookieID string
	// PageNumber 是分页同步请求的平台页码；全量同步忽略该字段。
	PageNumber int
	// PageSize 是平台单页大小；非正值由适配器使用默认值。
	PageSize int
	// MaxPages 是全量同步允许读取的最大页数；非正值表示不设上限。
	MaxPages int
}

// SyncAllResult 是全量同步成功后的稳定应用结果。
type SyncAllResult struct {
	// TotalCount 是平台本次返回的商品总数。
	TotalCount int
	// TotalPages 是平台本次实际读取的页数。
	TotalPages int
	// SavedCount 是本地成功写入或更新的商品数。
	SavedCount int
	// DeletedCount 是本地被标记删除的商品数。
	DeletedCount int
}

// SyncPageResult 是分页同步成功后的稳定应用结果。
type SyncPageResult struct {
	// PageNumber 是平台实际返回的页码。
	PageNumber int
	// PageSize 是平台实际使用的单页大小。
	PageSize int
	// CurrentCount 是本页返回的商品数。
	CurrentCount int
	// SavedCount 是本页成功写入或更新的商品数。
	SavedCount int
}

// SyncRepository 是商品同步用例所需的最小基础设施 Port。
type SyncRepository interface {
	// OwnsAccount 判断当前用户是否拥有指定平台账号，不读取或解密凭证。
	OwnsAccount(context.Context, int64, string) (bool, error)
	// SyncAll 从平台读取账号全集并完成本地 reconcile。
	SyncAll(context.Context, SyncQuery) (SyncAllResult, error)
	// SyncPage 从平台读取指定页并保存本页商品。
	SyncPage(context.Context, SyncQuery) (SyncPageResult, error)
}

// SyncService 编排全量和分页商品同步用例，不依赖 HTTP、数据库或平台 SDK。
type SyncService struct {
	// repository 提供商品同步使用的窄基础设施 Port。
	repository SyncRepository
}

// NewSyncService 构造商品同步应用服务。
func NewSyncService(repository SyncRepository) *SyncService {
	return &SyncService{repository: repository}
}

// SyncAll 校验用户输入后执行商品全集同步。
func (s *SyncService) SyncAll(ctx context.Context, query SyncQuery) (SyncAllResult, error) {
	if query.UserID <= 0 {
		return SyncAllResult{}, ErrSyncInvalidUser
	}
	if strings.TrimSpace(query.CookieID) == "" {
		return SyncAllResult{}, ErrSyncInvalidCookie
	}
	if s == nil || s.repository == nil {
		return SyncAllResult{}, &SyncError{Kind: SyncErrorPersistence, Err: errors.New("商品同步 repository 未初始化")}
	}
	// owned、ownershipErr 保存当前用户的非敏感账号归属判断结果。
	owned, ownershipErr := s.repository.OwnsAccount(ctx, query.UserID, query.CookieID)
	if ownershipErr != nil {
		return SyncAllResult{}, ownershipErr
	}
	if !owned {
		return SyncAllResult{}, ErrSyncNotOwned
	}
	return s.repository.SyncAll(ctx, query)
}

// SyncPage 校验用户输入后执行指定页商品同步。
func (s *SyncService) SyncPage(ctx context.Context, query SyncQuery) (SyncPageResult, error) {
	if query.UserID <= 0 {
		return SyncPageResult{}, ErrSyncInvalidUser
	}
	if strings.TrimSpace(query.CookieID) == "" {
		return SyncPageResult{}, ErrSyncInvalidCookie
	}
	if s == nil || s.repository == nil {
		return SyncPageResult{}, &SyncError{Kind: SyncErrorPersistence, Err: errors.New("商品同步 repository 未初始化")}
	}
	// owned、ownershipErr 保存当前用户的非敏感账号归属判断结果。
	owned, ownershipErr := s.repository.OwnsAccount(ctx, query.UserID, query.CookieID)
	if ownershipErr != nil {
		return SyncPageResult{}, ownershipErr
	}
	if !owned {
		return SyncPageResult{}, ErrSyncNotOwned
	}
	return s.repository.SyncPage(ctx, query)
}
