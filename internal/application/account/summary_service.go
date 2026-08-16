package account

import (
	"context"
	"errors"
)

// AccountSummary 是账号列表与所有权用例使用的非敏感账号摘要，不包含 Cookie、密码或加密 metadata。
type AccountSummary struct {
	// ID 是闲鱼账号的稳定标识。
	ID string
	// UserID 是账号所属的本地用户标识。
	UserID int64
	// AutoConfirm 表示账号是否启用自动确认收货。
	AutoConfirm bool
	// Remark 是用户为账号设置的备注。
	Remark string
	// PauseDuration 是账号暂停时长，单位为分钟。
	PauseDuration int
	// PausedUntil 是暂停结束时间的 Unix 秒；零表示当前没有暂停截止时间。
	PausedUntil int64
	// Username 是账号关联的登录用户名，不包含登录密码。
	Username string
	// ShowBrowser 表示密码登录流程是否允许显示浏览器。
	ShowBrowser bool
	// Nickname 是平台账号昵称缓存。
	Nickname string
	// AvatarURL 是平台账号头像缓存地址。
	AvatarURL string
	// LastRefreshAt 是最近一次资料刷新时间的 Unix 秒。
	LastRefreshAt int64
	// LoginMethod 是最近一次成功登录方式。
	LoginMethod string
	// LastLoginAt 是最近一次成功登录时间的 Unix 秒。
	LastLoginAt int64
	// CreatedAt 是账号记录创建时间的数据库字符串。
	CreatedAt string
}

// AdminAccountSummary 是管理员账号列表使用的非敏感摘要。
type AdminAccountSummary struct {
	// ID 是闲鱼账号的稳定标识。
	ID string
	// UserID 是账号所属的本地用户标识。
	UserID int64
	// Remark 是用户为账号设置的备注。
	Remark string
	// CreatedAt 是账号记录创建时间的数据库字符串。
	CreatedAt string
	// Owner 是账号所属用户的展示名。
	Owner string
	// Enabled 表示账号当前是否启用。
	Enabled bool
}

// AccountSummaryRepository 定义账号摘要和所有权用例需要的最小持久化端口。
type AccountSummaryRepository interface {
	// ListOwnedIDs 返回指定用户拥有的账号 ID，不读取凭证字段。
	ListOwnedIDs(context.Context, int64) ([]string, error)
	// ListSummaries 返回指定用户的非敏感账号摘要列表。
	ListSummaries(context.Context, int64) ([]AccountSummary, error)
	// GetOwnedSummary 返回指定用户拥有的单个非敏感账号摘要。
	GetOwnedSummary(context.Context, int64, string) (AccountSummary, error)
	// ExistsOwned 判断账号是否属于指定用户，仅返回存在性结论。
	ExistsOwned(context.Context, int64, string) (bool, error)
	// GetOwnerID 返回指定账号所属用户标识，不读取凭证字段。
	GetOwnerID(context.Context, string) (int64, error)
	// StatusOwned 返回指定用户账号的启用状态，并在读取前复核归属。
	StatusOwned(context.Context, int64, string) (bool, error)
}

// AdminSummaryRepository 定义管理员读取全局账号摘要所需的最小持久化端口。
type AdminSummaryRepository interface {
	// ListAdminSummaries 返回所有账号的非敏感管理员摘要。
	ListAdminSummaries(context.Context) ([]AdminAccountSummary, error)
}

// SummaryService 编排账号摘要读取、所有权判断和管理员账号列表，不依赖 HTTP 或数据库模型。
type SummaryService struct {
	// repository 保存普通用户账号摘要与所有权查询端口。
	repository AccountSummaryRepository
	// adminRepository 保存管理员全局账号摘要查询端口。
	adminRepository AdminSummaryRepository
}

// NewSummaryService 构造账号摘要应用服务并校验普通用户查询端口。
func NewSummaryService(repository AccountSummaryRepository, adminRepository AdminSummaryRepository) (*SummaryService, error) {
	if repository == nil {
		return nil, errors.New("账号摘要 repository 未初始化")
	}
	return &SummaryService{repository: repository, adminRepository: adminRepository}, nil
}

// ListOwnedIDs 返回指定用户拥有的账号 ID；数据库适配器错误原样向上返回。
func (s *SummaryService) ListOwnedIDs(ctx context.Context, userID int64) ([]string, error) {
	// validationErr 表示当前用户身份或摘要服务状态无效。
	validationErr := s.validateUser(userID)
	if validationErr != nil {
		return nil, validationErr
	}
	return s.repository.ListOwnedIDs(ctx, userID)
}

// ListSummaries 返回指定用户的非敏感账号摘要列表。
func (s *SummaryService) ListSummaries(ctx context.Context, userID int64) ([]AccountSummary, error) {
	// validationErr 表示当前用户身份或摘要服务状态无效。
	validationErr := s.validateUser(userID)
	if validationErr != nil {
		return nil, validationErr
	}
	return s.repository.ListSummaries(ctx, userID)
}

// GetOwnedSummary 返回指定用户拥有的单个账号摘要；不会读取或解密凭证。
func (s *SummaryService) GetOwnedSummary(ctx context.Context, userID int64, accountID string) (AccountSummary, error) {
	// validationErr 表示账号摘要请求的用户或账号标识无效。
	validationErr := s.validateRequest(userID, accountID)
	if validationErr != nil {
		return AccountSummary{}, validationErr
	}
	return s.repository.GetOwnedSummary(ctx, userID, accountID)
}

// ExistsOwned 返回账号是否属于指定用户；查询只返回存在性结论，不读取或解密凭证。
func (s *SummaryService) ExistsOwned(ctx context.Context, userID int64, accountID string) (bool, error) {
	// validationErr 表示账号归属请求的用户或账号标识无效。
	validationErr := s.validateRequest(userID, accountID)
	if validationErr != nil {
		return false, validationErr
	}
	return s.repository.ExistsOwned(ctx, userID, accountID)
}

// StatusOwned 返回指定用户账号的启用状态；归属和数据库错误均向上返回。
func (s *SummaryService) StatusOwned(ctx context.Context, userID int64, accountID string) (bool, error) {
	// validationErr 表示账号状态请求的用户或账号标识无效。
	validationErr := s.validateRequest(userID, accountID)
	if validationErr != nil {
		return false, validationErr
	}
	return s.repository.StatusOwned(ctx, userID, accountID)
}

// RequireOwnership 校验账号是否属于用户，并区分账号不存在、越权和基础设施故障。
func (s *SummaryService) RequireOwnership(ctx context.Context, userID int64, accountID string) error {
	// validationErr 表示账号所有权请求的用户或账号标识无效。
	validationErr := s.validateRequest(userID, accountID)
	if validationErr != nil {
		return validationErr
	}
	// owned、queryErr 保存联合所有权查询结果及其基础设施错误。
	owned, queryErr := s.repository.ExistsOwned(ctx, userID, accountID)
	if queryErr != nil {
		return queryErr
	}
	if owned {
		return nil
	}
	// ownerID、ownerErr 用于区分账号不存在和账号属于其他用户。
	ownerID, ownerErr := s.repository.GetOwnerID(ctx, accountID)
	if ownerErr != nil {
		return ownerErr
	}
	if ownerID != userID {
		return ErrForbidden
	}
	return ErrNotFound
}

// ListAdminSummaries 返回管理员账号摘要；管理员端口未装配时明确失败。
func (s *SummaryService) ListAdminSummaries(ctx context.Context) ([]AdminAccountSummary, error) {
	if s == nil || s.adminRepository == nil {
		return nil, errors.New("管理员账号摘要 repository 未初始化")
	}
	return s.adminRepository.ListAdminSummaries(ctx)
}

// validateUser 校验所有权查询必须使用真实用户身份，拒绝管理员隐式用户零值。
func (s *SummaryService) validateUser(userID int64) error {
	if s == nil || s.repository == nil {
		return errors.New("账号摘要服务未初始化")
	}
	if userID <= 0 {
		return errors.New("账号用户身份无效")
	}
	return nil
}

// validateRequest 校验账号摘要查询的用户身份和账号标识。
func (s *SummaryService) validateRequest(userID int64, accountID string) error {
	// validationErr 表示账号摘要请求的用户身份无效。
	validationErr := s.validateUser(userID)
	if validationErr != nil {
		return validationErr
	}
	if accountID == "" {
		return errors.New("缺少账号 ID")
	}
	return nil
}
