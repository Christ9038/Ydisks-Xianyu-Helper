package server

import (
	"context"
	"errors"
	"strings"
	"time"

	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
)

// accountLoginService 是账号登录相关应用服务，负责凭证写入、登录审计、资料刷新和运行时重启编排。
type accountLoginService struct {
	// cookieWriterFactory 按请求创建新增 Cookie 写入端口；明文只进入返回的短生命周期实例。
	cookieWriterFactory func(string) accountapp.CookieWriter
	// cookieUpdaterFactory 按请求创建既有 Cookie 更新端口；明文只进入返回的短生命周期实例。
	cookieUpdaterFactory func(string) accountapp.CookieUpdater
	// sessionPort 提供平台会话写回所需的最小凭证能力，不暴露完整账号仓储。
	sessionPort accountapp.CredentialSessionPort
	// createApplication 提供已迁移的手动 Cookie 登录应用用例。
	createApplication *accountapp.LoginService
	// qrApplication 提供扫码成功凭证持久化应用用例。
	qrApplication *accountapp.QRLoginService
	// qrSessions 持有扫码会话所有权和幂等结果，Server 只负责 HTTP 适配。
	qrSessions *accountapp.QRLoginSessionRegistry
}

// newAccountLoginCreateApplication 构造手动 Cookie 登录应用服务及其生命周期适配器。
func newAccountLoginCreateApplication(lifecycle accountapp.LoginLifecyclePort) (*accountapp.LoginService, error) {
	return accountapp.NewLoginService(lifecycle)
}

// accountLoginApplication 返回当前 Server 绑定的账号登录应用服务。
func (s *Server) accountLoginApplication() *accountLoginService {
	return s.applicationServiceSet().accountLogin
}

// platformCredentialSessionPort 返回平台 Cookie 会话写回所需的最小应用端口。
// Server 不通过该端口读取账号或登录秘密；凭证锁仍由调用方在慢速平台 I/O 之外管理。
func (s *Server) platformCredentialSessionPort() accountapp.CredentialSessionPort {
	if s == nil || s.accountLoginApplication() == nil {
		return nil
	}
	return s.accountLoginApplication().sessionPort
}

// accountLoginInput 是新增账号登录凭证用例的业务输入。
type accountLoginInput struct {
	// AccountID 是待创建的账号标识。
	AccountID string
	// Cookies 是平台登录 Cookie 字符串。
	Cookies string
	// UserID 是当前用户标识。
	UserID int64
	// LoginMethod 是账号登录方式。
	LoginMethod string
}

// accountCookieUpdateInput 是更新账号登录凭证用例的业务输入。
type accountCookieUpdateInput struct {
	// AccountID 是待更新的账号标识。
	AccountID string
	// Cookies 是新的平台登录 Cookie 字符串。
	Cookies string
	// UserID 是当前用户标识。
	UserID int64
	// LoginMethod 是可选的登录方式。
	LoginMethod string
	// ExpectedRevision 是客户端读取到的最近 Cookie 刷新时间，用于检测并发覆盖。
	ExpectedRevision int64
}

// CreateCookie 创建账号凭证并完成登录审计、资料刷新和运行时重启。
func (svc *accountLoginService) CreateCookie(ctx context.Context, input accountLoginInput) error {
	if svc == nil || svc.createApplication == nil {
		return errors.New("账号登录应用服务未初始化")
	}
	if svc.cookieWriterFactory == nil {
		return errors.New("账号 Cookie 写入端口未初始化")
	}
	return svc.createApplication.CreateCookie(ctx, accountapp.CreateCookieInput{
		AccountID: input.AccountID, UserID: input.UserID, LoginMethod: input.LoginMethod,
	}, svc.cookieWriterFactory(input.Cookies))
}

// UpdateCookie 更新账号凭证并完成登录审计、资料刷新和运行时重启。
func (svc *accountLoginService) UpdateCookie(ctx context.Context, input accountCookieUpdateInput) error {
	if svc == nil || svc.createApplication == nil {
		return errors.New("账号登录应用服务未初始化")
	}
	if svc.cookieUpdaterFactory == nil {
		return errors.New("账号 Cookie 更新端口未初始化")
	}
	return svc.createApplication.UpdateCookie(ctx, accountapp.UpdateCookieInput{
		AccountID: input.AccountID, UserID: input.UserID, LoginMethod: input.LoginMethod, ExpectedRevision: input.ExpectedRevision,
	}, svc.cookieUpdaterFactory(input.Cookies))
}

// PersistQRLoginSuccess 将 HTTP/平台 map 适配为纯应用 DTO，并复用会话级幂等锁。
func (svc *accountLoginService) PersistQRLoginSuccess(ctx context.Context, userID int64, sessionID string, result map[string]any, targetAccountID string) (qrLoginPersistence, error) {
	if svc == nil || svc.qrApplication == nil || svc.qrSessions == nil {
		return qrLoginPersistence{}, errors.New("扫码登录应用服务未初始化")
	}
	// persisted 保存注册表执行凭证持久化后返回的非敏感幂等结果。
	persisted, persistErr := svc.qrSessions.PersistOnce(sessionID, userID, func() (accountapp.QRLoginSessionPersistence, error) {
		// cookies 保存平台返回的登录 Cookie 明文，仅在 Server 到应用端口的调用边界短暂存在。
		cookies := qrString(result, "cookies")
		// cookieSnapshot 和 snapshotComplete 保存平台返回的完整 Cookie 快照及其完整性。
		cookieSnapshot, snapshotComplete := adapter.CookieSnapshotsFromResult(result)
		// scannedAccountID 保存从结果字段或 Cookie 解析出的平台账号标识。
		scannedAccountID := strings.TrimSpace(firstNonEmpty(qrString(result, "unb"), adapter.AccountIDFromCookie(cookies)))
		// input 保存转换后的纯应用扫码登录输入；Cookie 只交给凭证端口消费。
		input := accountapp.QRLoginInput{UserID: userID, ScannedAccountID: scannedAccountID, TargetAccountID: targetAccountID, Cookies: cookies}
		if snapshotComplete {
			input.Snapshot = cookieSnapshot
		}
		// resultValue 保存应用服务返回的非敏感持久化结果。
		resultValue, persistErr := svc.qrApplication.PersistSuccess(ctx, input)
		if persistErr != nil {
			if errors.Is(persistErr, accountapp.ErrQRLoginIncomplete) {
				return accountapp.QRLoginSessionPersistence{}, errors.New("扫码结果缺少 cookies 或 unb")
			}
			if errors.Is(persistErr, accountapp.ErrQRLoginAccountMismatch) {
				return accountapp.QRLoginSessionPersistence{}, errors.New("扫码账号与待重新授权账号不一致，已拒绝覆盖")
			}
			if errors.Is(persistErr, accountapp.ErrForbidden) {
				return accountapp.QRLoginSessionPersistence{}, errors.New("该账号ID已存在且不属于当前用户")
			}
			if errors.Is(persistErr, accountapp.ErrAlreadyExists) {
				return accountapp.QRLoginSessionPersistence{}, errors.New("该账号ID已被并发创建，请重新获取账号状态")
			}
			return accountapp.QRLoginSessionPersistence{}, persistErr
		}
		return accountapp.QRLoginSessionPersistence{AccountID: resultValue.AccountID, IsNew: resultValue.IsNew, CreatedAt: time.Now().UTC()}, nil
	})
	if persistErr != nil {
		if errors.Is(persistErr, accountapp.ErrQRLoginSessionForbidden) {
			return qrLoginPersistence{}, errors.New("扫码会话不属于当前用户")
		}
		return qrLoginPersistence{}, persistErr
	}
	return qrLoginPersistence{AccountID: persisted.AccountID, IsNew: persisted.IsNew, UserID: persisted.UserID, CreatedAt: persisted.CreatedAt}, nil
}

// ValidateCookieInput 校验账号标识和 Cookie 输入，供 HTTP 适配层复用一致规则。
func (svc *accountLoginService) ValidateCookieInput(input accountLoginInput) error {
	if strings.TrimSpace(input.AccountID) == "" || strings.TrimSpace(input.Cookies) == "" {
		return errors.New("缺少账号 ID 或 Cookie")
	}
	return nil
}
