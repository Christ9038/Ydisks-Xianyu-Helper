package account

import (
	"context"
	"errors"
	"strings"
)

// ErrQRLoginIncomplete 表示平台扫码成功结果缺少可持久化的账号标识或 Cookie。
var ErrQRLoginIncomplete = errors.New("扫码结果缺少账号标识或 cookies")

// ErrQRLoginAccountMismatch 表示扫码得到的平台账号与待重新授权账号不一致。
var ErrQRLoginAccountMismatch = errors.New("扫码账号与待重新授权账号不一致")

// ErrAlreadyExists 表示扫码创建账号时目标标识已被并发请求占用。
var ErrAlreadyExists = errors.New("账号已存在")

// CookieSnapshot 是浏览器返回的单个 Cookie 快照；明文值只在当前凭证适配端口调用期间存在。
type CookieSnapshot struct {
	// Name 是浏览器 Cookie 名称。
	Name string
	// Value 是浏览器 Cookie 明文值，禁止日志记录、序列化到 HTTP 或进入长期应用状态。
	Value string
	// Domain 是浏览器返回的 Cookie 作用域。
	Domain string
	// Path 是浏览器返回的 Cookie 路径。
	Path string
	// Expires 是浏览器返回的 Unix 时间戳；零值表示会话 Cookie。
	Expires float64
	// HTTPOnly 表示浏览器是否禁止脚本访问该 Cookie。
	HTTPOnly bool
	// Secure 表示该 Cookie 是否只允许通过安全连接发送。
	Secure bool
	// SameSite 是浏览器返回的跨站请求策略。
	SameSite string
	// PartitionKey 是浏览器返回的分区键，供 Cookie 快照合并时保持原样。
	PartitionKey string
}

// QRLoginInput 是扫码登录成功后持久化用例的纯业务输入；不依赖 HTTP 或平台 DTO。
type QRLoginInput struct {
	// UserID 是发起扫码并拥有目标账号的本地用户标识。
	UserID int64
	// ScannedAccountID 是扫码结果解析出的平台账号标识。
	ScannedAccountID string
	// TargetAccountID 是可选的待重新授权账号标识；为空时按扫码账号创建或更新。
	TargetAccountID string
	// Cookies 是平台返回的登录 Cookie 明文，仅由凭证端口在最小作用域内消费。
	Cookies string
	// Snapshot 是可选的完整浏览器 Cookie 快照；存在时由端口负责合并到加密 metadata。
	Snapshot []CookieSnapshot
}

// QRLoginResult 是扫码登录持久化后返回的非敏感结果。
type QRLoginResult struct {
	// AccountID 是最终创建或更新的本地账号标识。
	AccountID string
	// IsNew 表示本次是否创建了新账号。
	IsNew bool
}

// QRLoginAccount 是账号归属查询返回的非敏感身份信息，不包含 Cookie、密码或 metadata。
type QRLoginAccount struct {
	// ID 是本地账号标识。
	ID string
	// UserID 是账号所属本地用户标识。
	UserID int64
}

// QRLoginRepository 定义扫码登录成功后写凭证所需的最小端口。
type QRLoginRepository interface {
	// LockCredentials 串行化同一账号的凭证创建或更新；解锁函数必须由调用方执行。
	LockCredentials(string) func()
	// FindAccount 只查询账号存在性与归属，不读取或解密任何凭证字段。
	FindAccount(context.Context, string) (QRLoginAccount, error)
	// CreateCookieOwned 创建当前用户拥有的账号 Cookie；明文只在端口内部进入加密存储。
	CreateCookieOwned(context.Context, string, string, int64) error
	// UpdateFlatCookieOwned 更新已有账号的扁平 Cookie，并由实现方清理旧快照。
	UpdateFlatCookieOwned(context.Context, string, string) error
	// UpdateCookieSnapshotOwned 更新 Cookie 并由实现方合并完整浏览器快照到加密 metadata。
	UpdateCookieSnapshotOwned(context.Context, string, string, []CookieSnapshot) error
	// ClearTokens 清理账号旧连接 Token；失败由应用生命周期端口记录，且不回滚已成功写入的 Cookie。
	ClearTokens(context.Context, string) error
}

// QRLoginLifecycle 定义凭证写入完成后执行的审计、资料刷新和运行时同步边界。
type QRLoginLifecycle interface {
	// AfterSuccessfulQRLogin 在凭证锁释放后执行登录后续编排，禁止在实现中重新暴露凭证明文。
	AfterSuccessfulQRLogin(context.Context, int64, string)
	// ReportQRLoginCleanupFailure 记录旧 Token 清理失败；该失败不改变已完成的凭证写入结果。
	ReportQRLoginCleanupFailure(context.Context, string, error)
}

// QRLoginService 编排扫码结果校验、账号归属、凭证写入和登录后续动作。
type QRLoginService struct {
	// repository 提供不泄露凭证字段的账号持久化端口。
	repository QRLoginRepository
	// lifecycle 提供凭证写入后的审计、资料刷新和运行时适配。
	lifecycle QRLoginLifecycle
}

// NewQRLoginService 构造扫码登录应用服务并校验必需端口。
func NewQRLoginService(repository QRLoginRepository, lifecycle QRLoginLifecycle) (*QRLoginService, error) {
	if repository == nil {
		return nil, errors.New("扫码登录凭证 repository 未初始化")
	}
	if lifecycle == nil {
		return nil, errors.New("扫码登录生命周期端口未初始化")
	}
	return &QRLoginService{repository: repository, lifecycle: lifecycle}, nil
}

// PersistSuccess 执行一次扫码登录成功持久化；返回值只包含账号标识和新建标记。
func (s *QRLoginService) PersistSuccess(ctx context.Context, input QRLoginInput) (QRLoginResult, error) {
	if s == nil || s.repository == nil || s.lifecycle == nil {
		return QRLoginResult{}, errors.New("扫码登录服务未初始化")
	}
	// scannedAccountID 是去除空白后的平台账号标识，避免把格式差异带入账号主键。
	scannedAccountID := strings.TrimSpace(input.ScannedAccountID)
	// targetAccountID 是去除空白后的重新授权目标账号标识。
	targetAccountID := strings.TrimSpace(input.TargetAccountID)
	if scannedAccountID == "" || strings.TrimSpace(input.Cookies) == "" {
		return QRLoginResult{}, ErrQRLoginIncomplete
	}
	// accountID 是最终用于持久化的账号标识；默认采用扫码得到的账号。
	accountID := scannedAccountID
	if targetAccountID != "" {
		if targetAccountID != scannedAccountID {
			return QRLoginResult{}, ErrQRLoginAccountMismatch
		}
		accountID = targetAccountID
	}
	// unlock 保护从归属查询到凭证写入的完整串行区间；生命周期动作在释放后执行。
	unlock := s.repository.LockCredentials(accountID)
	// account 保存不含凭证字段的账号归属查询结果。
	account, findErr := s.repository.FindAccount(ctx, accountID)
	// isNew 标记本次是否需要创建账号。
	isNew := false
	switch {
	case errors.Is(findErr, ErrNotFound):
		if targetAccountID != "" {
			unlock()
			return QRLoginResult{}, errors.New("待重新授权账号不存在")
		}
		isNew = true
		// createErr 保存新账号凭证端口写入错误。
		if createErr := s.repository.CreateCookieOwned(ctx, accountID, input.Cookies, input.UserID); createErr != nil {
			unlock()
			return QRLoginResult{}, createErr
		}
	case findErr != nil:
		unlock()
		return QRLoginResult{}, findErr
	case account.UserID != input.UserID:
		unlock()
		return QRLoginResult{}, ErrForbidden
	default:
		// updateErr 保存已有账号 Cookie 更新错误。
		if updateErr := s.updateExisting(ctx, accountID, input); updateErr != nil {
			unlock()
			return QRLoginResult{}, updateErr
		}
	}
	if isNew && len(input.Snapshot) > 0 {
		// snapshotErr 保存新账号完整 Cookie 快照写入错误。
		if snapshotErr := s.repository.UpdateCookieSnapshotOwned(ctx, accountID, input.Cookies, input.Snapshot); snapshotErr != nil {
			unlock()
			return QRLoginResult{}, snapshotErr
		}
	}
	// clearErr 保存旧连接 Token 清理结果；凭证已写入成功，因此该尽力而为操作不阻断登录后续动作。
	clearErr := s.repository.ClearTokens(ctx, accountID)
	unlock()
	if clearErr != nil {
		s.lifecycle.ReportQRLoginCleanupFailure(ctx, accountID, clearErr)
	}
	s.lifecycle.AfterSuccessfulQRLogin(ctx, input.UserID, accountID)
	return QRLoginResult{AccountID: accountID, IsNew: isNew}, nil
}

// updateExisting 更新已存在且已通过归属校验的账号凭证。
func (s *QRLoginService) updateExisting(ctx context.Context, accountID string, input QRLoginInput) error {
	if len(input.Snapshot) > 0 {
		return s.repository.UpdateCookieSnapshotOwned(ctx, accountID, input.Cookies, input.Snapshot)
	}
	return s.repository.UpdateFlatCookieOwned(ctx, accountID, input.Cookies)
}
