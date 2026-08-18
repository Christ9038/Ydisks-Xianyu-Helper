package composition

import (
	"context"
	"errors"
	"strings"
	"time"

	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
)

// accountLoginService 组合手动 Cookie 与二维码登录的应用用例；它只在组合层持有适配器细节。
type accountLoginService struct {
	// cookieWriterFactory 为单次请求创建明文 Cookie 写入端口，明文不会进入 HTTP Server 状态。
	cookieWriterFactory func(string) accountapp.CookieWriter
	// cookieUpdaterFactory 为单次更新请求创建明文 Cookie 写入端口。
	cookieUpdaterFactory func(string) accountapp.CookieUpdater
	// sessionPort 保存平台响应 Cookie 会话所需的窄凭证能力。
	sessionPort accountapp.CredentialSessionPort
	// createApplication 执行手动 Cookie 登录用例。
	createApplication *accountapp.LoginService
	// qrApplication 执行扫码成功后的凭证持久化用例。
	qrApplication *accountapp.QRLoginService
	// qrSessions 管理二维码会话的所有权、过期与幂等写入。
	qrSessions *accountapp.QRLoginSessionRegistry
}

// CookieLoginResult 是扫码登录持久化后允许返回给 transport 的非敏感结果。
type CookieLoginResult struct {
	// AccountID 是已经创建或更新的账号标识。
	AccountID string
	// IsNew 指示本次是否创建账号。
	IsNew bool
	// UserID 是二维码会话绑定的用户标识。
	UserID int64
	// CreatedAt 是本次幂等结果创建时间。
	CreatedAt time.Time
}

// CreateCookie 创建账号凭证并触发登录后的应用编排。
func (service *accountLoginService) CreateCookie(ctx context.Context, accountID, cookies string, userID int64, loginMethod string) error {
	if service == nil || service.createApplication == nil || service.cookieWriterFactory == nil {
		return errors.New("账号 Cookie 登录服务未初始化")
	}
	return service.createApplication.CreateCookie(ctx, accountapp.CreateCookieInput{AccountID: accountID, UserID: userID, LoginMethod: loginMethod}, service.cookieWriterFactory(cookies))
}

// UpdateCookie 更新账号凭证并触发登录后的应用编排。
func (service *accountLoginService) UpdateCookie(ctx context.Context, accountID, cookies string, userID int64, loginMethod string, expectedRevision int64) error {
	if service == nil || service.createApplication == nil || service.cookieUpdaterFactory == nil {
		return errors.New("账号 Cookie 更新服务未初始化")
	}
	return service.createApplication.UpdateCookie(ctx, accountapp.UpdateCookieInput{AccountID: accountID, UserID: userID, LoginMethod: loginMethod, ExpectedRevision: expectedRevision}, service.cookieUpdaterFactory(cookies))
}

// PersistQRLoginSuccess 将平台结果转换为应用命令，并在会话注册表下执行一次性持久化。
func (service *accountLoginService) PersistQRLoginSuccess(ctx context.Context, userID int64, sessionID string, result map[string]any, targetAccountID string) (CookieLoginResult, error) {
	if service == nil || service.qrApplication == nil || service.qrSessions == nil {
		return CookieLoginResult{}, errors.New("扫码登录应用服务未初始化")
	}
	// persisted、persistErr 分别是会话幂等结果及其持久化错误。
	persisted, persistErr := service.qrSessions.PersistOnce(sessionID, userID, func() (accountapp.QRLoginSessionPersistence, error) {
		// cookies 保存平台返回的明文凭证，仅在当前用例调用期间传递，不进入 Server 状态。
		cookies := resultString(result, "cookies")
		// cookieSnapshot、snapshotComplete 分别是结构化 Cookie 快照及其是否完整的标记。
		cookieSnapshot, snapshotComplete := adapter.CookieSnapshotsFromResult(result)
		// scannedAccountID 是平台扫描出的账号标识，缺失时从 Cookie 非敏感字段推断。
		scannedAccountID := strings.TrimSpace(firstNonEmpty(resultString(result, "unb"), adapter.AccountIDFromCookie(cookies)))
		// input 是扫码登录应用服务使用的凭证持久化命令。
		input := accountapp.QRLoginInput{UserID: userID, ScannedAccountID: scannedAccountID, TargetAccountID: targetAccountID, Cookies: cookies}
		if snapshotComplete {
			input.Snapshot = cookieSnapshot
		}
		// resultValue、writeErr 分别是扫码成功持久化结果及其错误。
		resultValue, writeErr := service.qrApplication.PersistSuccess(ctx, input)
		if writeErr != nil {
			return accountapp.QRLoginSessionPersistence{}, writeErr
		}
		return accountapp.QRLoginSessionPersistence{AccountID: resultValue.AccountID, IsNew: resultValue.IsNew, CreatedAt: time.Now().UTC()}, nil
	})
	if persistErr != nil {
		return CookieLoginResult{}, persistErr
	}
	return CookieLoginResult{AccountID: persisted.AccountID, IsNew: persisted.IsNew, UserID: persisted.UserID, CreatedAt: persisted.CreatedAt}, nil
}

// RegisterQRSession 记录二维码会话的用户所有权。
func (service *accountLoginService) RegisterQRSession(sessionID string, userID int64, createdAt time.Time) {
	if service != nil && service.qrSessions != nil {
		service.qrSessions.Register(sessionID, userID, createdAt)
	}
}

// AuthorizeQRSession 验证二维码会话属于当前用户。
func (service *accountLoginService) AuthorizeQRSession(sessionID string, userID int64) error {
	if service == nil || service.qrSessions == nil {
		return errors.New("扫码登录应用服务未初始化")
	}
	return service.qrSessions.Authorize(sessionID, userID)
}

// CleanupQRSessions 清理过期二维码会话并返回需要删除的平台会话标识。
func (service *accountLoginService) CleanupQRSessions(now time.Time) []string {
	if service == nil || service.qrSessions == nil {
		return nil
	}
	return service.qrSessions.Cleanup(now)
}

// CredentialSessionPort 返回平台 Cookie 会话写入所需的最小凭证端口。
func (service *accountLoginService) CredentialSessionPort() accountapp.CredentialSessionPort {
	if service == nil {
		return nil
	}
	return service.sessionPort
}

// resultString 读取平台结果中的字符串字段；非字符串或不存在时返回空值。
func resultString(result map[string]any, key string) string {
	// value、ok 分别是平台字段的字符串值及其类型判断结果。
	if value, ok := result[key].(string); ok {
		return value
	}
	return ""
}

// firstNonEmpty 返回第一个非空字符串，用于优先使用平台明确字段。
func firstNonEmpty(values ...string) string {
	// value 是当前待判断的候选字符串；首个非空值具有优先级。
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
