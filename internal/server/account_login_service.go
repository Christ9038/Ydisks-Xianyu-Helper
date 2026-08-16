package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
)

// accountLoginService 是账号登录相关应用服务，负责凭证写入、登录审计、资料刷新和运行时重启编排。
type accountLoginService struct {
	// server 提供账号存储、运行时管理器和扫码会话持久化依赖。
	server *Server
	// repository 提供账号登录服务所需的最小凭证持久化能力。
	repository accountapp.CredentialRepository
	// summaryRepository 提供不解密凭证的账号摘要和归属查询能力。
	summaryRepository accountapp.AccountSummaryRepository
	// createApplication 提供已迁移的手动 Cookie 登录应用用例。
	createApplication *accountapp.LoginService
	// qrApplication 提供扫码成功凭证持久化应用用例；会话幂等状态仍由 Server 适配器拥有。
	qrApplication *accountapp.QRLoginService
}

// serverLoginLifecyclePort 将登录成功后的审计、资料刷新和运行时重启适配到应用层端口。
type serverLoginLifecyclePort struct {
	// server 提供现有 Server 运行时与审计适配能力。
	server *Server
}

// AfterSuccessfulLogin 在凭证写入并释放凭证锁后执行 Server 侧登录后续编排。
func (p serverLoginLifecyclePort) AfterSuccessfulLogin(ctx context.Context, userID int64, accountID, method string) {
	if p.server == nil {
		return
	}
	if strings.TrimSpace(method) != "" {
		p.server.markSuccessfulLogin(ctx, accountID, userID, method, "账号登录成功")
	}
	p.server.accountLoginApplication().refreshAndRestartAccount(ctx, userID, accountID)
}

// serverCookieWriter 将本次请求中的明文 Cookie 限制在 Server 基础设施适配器内。
type serverCookieWriter struct {
	// repository 提供凭证锁、写入和旧 Token 清理能力。
	repository accountapp.CredentialRepository
	// cookies 保存当前请求明文 Cookie；仅在 CreateOwnedCookie 调用期间使用，不进入应用层模型。
	cookies string
	// logger 提供旧 Token 清理失败的脱敏日志能力。
	logger *slog.Logger
}

// CreateOwnedCookie 在凭证锁内原子校验归属、写入 Cookie 并清理旧连接 Token。
func (w serverCookieWriter) CreateOwnedCookie(ctx context.Context, accountID string, userID int64) error {
	if w.repository == nil {
		return errors.New("账号登录凭证 repository 未初始化")
	}
	// unlock 保护凭证写入与旧连接 Token 清理，外部网络和运行时操作在锁外执行。
	unlock := w.repository.LockCredentials(accountID)
	defer unlock()
	// writeErr 保存凭证写入错误；归属校验失败时直接返回且不清理 Token。
	writeErr := w.repository.CreateCookieOwned(ctx, accountID, w.cookies, userID)
	if writeErr != nil {
		return writeErr
	}
	// clearErr 保存旧连接 Token 清理错误；清理失败不回滚已经成功写入的 Cookie。
	clearErr := w.repository.ClearTokens(ctx, accountID)
	if clearErr != nil && w.logger != nil {
		w.logger.Warn("新增账号后清理旧连接凭证失败", "cookie_id", accountID, "err", clearErr)
	}
	return nil
}

// UpdateOwnedCookie 在账号凭证短锁内完成归属复核、Cookie 写入和旧 Token 清理；慢速资料刷新由应用服务在解锁后触发。
func (w serverCookieWriter) UpdateOwnedCookie(ctx context.Context, accountID string, userID, expectedRevision int64) error {
	if w.repository == nil {
		return errors.New("账号登录凭证 repository 未初始化")
	}
	// unlock 只保护凭证快照读取、数据库写入和旧 Token 清理，不跨越网络或运行时操作。
	unlock := w.repository.LockCredentials(accountID)
	defer unlock()
	// detail 保存锁内读取的平台凭证窄视图；该视图不会把登录密码传入应用层。
	detail, loadErr := w.repository.LoadPlatformDetail(ctx, accountID)
	if loadErr != nil {
		return loadErr
	}
	if detail == nil || detail.UserID != userID {
		return accountapp.ErrForbidden
	}
	if expectedRevision != 0 && detail.LastRefreshAt != expectedRevision {
		return accountapp.ErrCredentialConflict
	}
	// updateErr 保存归属已确认后的 Cookie 持久化结果；适配器负责清除旧完整快照。
	if updateErr := w.repository.UpdateFlatCookieOwned(ctx, detail, w.cookies); updateErr != nil {
		return updateErr
	}
	// clearErr 保存旧连接 Token 清理错误；凭证已成功写入时清理失败仅记录并继续。
	if clearErr := w.repository.ClearTokens(ctx, accountID); clearErr != nil && w.logger != nil {
		w.logger.Warn("更新账号后清理旧连接凭证失败", "cookie_id", accountID, "err", clearErr)
	}
	return nil
}

// newAccountLoginCreateApplication 构造手动 Cookie 登录应用服务及其 Server 生命周期适配器。
func newAccountLoginCreateApplication(server *Server) (*accountapp.LoginService, error) {
	return accountapp.NewLoginService(serverLoginLifecyclePort{server: server})
}

// accountLoginApplication 返回当前 Server 绑定的账号登录应用服务。
func (s *Server) accountLoginApplication() *accountLoginService {
	return s.applicationServiceSet().accountLogin
}

// accountLoginRepositoryForServer 返回当前 Server 装配的账号登录持久化边界。
func (s *Server) accountLoginRepositoryForServer() accountapp.CredentialRepository {
	return s.accountLoginApplication().repository
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
	if svc == nil || svc.createApplication == nil || svc.server == nil {
		return errors.New("账号登录应用服务未初始化")
	}
	return svc.createApplication.CreateCookie(ctx, accountapp.CreateCookieInput{
		AccountID: input.AccountID, UserID: input.UserID, LoginMethod: input.LoginMethod,
	}, serverCookieWriter{repository: svc.repository, cookies: input.Cookies, logger: svc.server.Logger})
}

// UpdateCookie 更新账号凭证并完成登录审计、资料刷新和运行时重启。
func (svc *accountLoginService) UpdateCookie(ctx context.Context, input accountCookieUpdateInput) error {
	if svc == nil || svc.createApplication == nil || svc.server == nil {
		return errors.New("账号登录应用服务未初始化")
	}
	return svc.createApplication.UpdateCookie(ctx, accountapp.UpdateCookieInput{
		AccountID: input.AccountID, UserID: input.UserID, LoginMethod: input.LoginMethod, ExpectedRevision: input.ExpectedRevision,
	}, serverCookieWriter{repository: svc.repository, cookies: input.Cookies, logger: svc.server.Logger})
}

// serverQRLoginLifecycle 将扫码登录成功后的审计、资料刷新和运行时同步适配到应用端口。
type serverQRLoginLifecycle struct {
	// server 提供审计、资料刷新、自动化唤醒和账号运行时管理能力。
	server *Server
}

// AfterSuccessfulQRLogin 在凭证锁释放后执行扫码登录成功的后续编排。
func (p serverQRLoginLifecycle) AfterSuccessfulQRLogin(ctx context.Context, userID int64, accountID string) {
	if p.server == nil {
		return
	}
	p.server.markSuccessfulLogin(ctx, accountID, userID, loginMethodQRScan, "扫码登录成功")
	p.server.accountLoginApplication().refreshAndRestartAccount(ctx, userID, accountID)
	p.server.wakeCredentialBlockedAutomation(ctx, accountID)
}

// ReportQRLoginCleanupFailure 记录扫码登录后旧 Token 清理失败，不暴露 Cookie 内容且不回滚已写入凭证。
func (p serverQRLoginLifecycle) ReportQRLoginCleanupFailure(_ context.Context, accountID string, err error) {
	if p.server != nil && p.server.Logger != nil {
		p.server.Logger.Warn("扫码登录后清理旧连接凭证失败", "cookie_id", accountID, "err", err)
	}
}

// PersistQRLoginSuccess 将 HTTP/平台 map 适配为纯应用 DTO，并复用会话级幂等锁。
func (svc *accountLoginService) PersistQRLoginSuccess(ctx context.Context, userID int64, sessionID string, result map[string]any, targetAccountID string) (qrLoginPersistence, error) {
	if svc == nil || svc.server == nil || svc.qrApplication == nil {
		return qrLoginPersistence{}, errors.New("扫码登录应用服务未初始化")
	}
	// s 是当前账号登录应用服务依赖的 Server。
	s := svc.server
	// lockValue 和 persistMu 保证同一扫码会话只执行一次持久化。
	lockValue, _ := s.qrPersistLocks.LoadOrStore(sessionID, &sync.Mutex{})
	// persistMu 是当前扫码会话的串行化锁。
	persistMu := lockValue.(*sync.Mutex)
	persistMu.Lock()
	defer persistMu.Unlock()

	s.qrMu.Lock()
	if s.qrPersisted == nil {
		s.qrPersisted = make(map[string]qrLoginPersistence)
	}
	// persisted 和 ok 保存已完成的幂等结果及其存在性。
	if persisted, ok := s.qrPersisted[sessionID]; ok {
		s.qrMu.Unlock()
		if persisted.UserID != userID {
			return qrLoginPersistence{}, errors.New("扫码会话不属于当前用户")
		}
		return persisted, nil
	}
	s.qrMu.Unlock()
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
			return qrLoginPersistence{}, errors.New("扫码结果缺少 cookies 或 unb")
		}
		if errors.Is(persistErr, accountapp.ErrQRLoginAccountMismatch) {
			return qrLoginPersistence{}, errors.New("扫码账号与待重新授权账号不一致，已拒绝覆盖")
		}
		if errors.Is(persistErr, accountapp.ErrForbidden) {
			return qrLoginPersistence{}, errors.New("该账号ID已存在且不属于当前用户")
		}
		if errors.Is(persistErr, accountapp.ErrAlreadyExists) {
			return qrLoginPersistence{}, errors.New("该账号ID已被并发创建，请重新获取账号状态")
		}
		return qrLoginPersistence{}, persistErr
	}
	// persisted 是扫码会话幂等返回值。
	persisted := qrLoginPersistence{AccountID: resultValue.AccountID, IsNew: resultValue.IsNew, UserID: userID, CreatedAt: time.Now().UTC()}
	s.qrMu.Lock()
	s.qrPersisted[sessionID] = persisted
	s.qrMu.Unlock()
	s.qrPersistLocks.Delete(sessionID)
	return persisted, nil
}

// refreshAndRestartAccount 在凭证锁释放后刷新资料并按账号状态重启运行时。
func (svc *accountLoginService) refreshAndRestartAccount(ctx context.Context, userID int64, accountID string) {
	// s 是当前账号登录应用服务依赖的 Server。
	s := svc.server
	// detail 和 err 保存资料摘要查询结果。
	_, err := svc.summaryRepository.GetOwnedSummary(ctx, userID, accountID)
	if err == nil {
		// profileResult、profileErr 保存登录成功后的资料刷新结果；失败仅记录，不阻断运行时重启。
		profileResult, profileErr := s.accountProfileApplication().RefreshProfile(ctx, userID, accountID)
		if profileErr != nil && s.Logger != nil {
			s.Logger.Warn("登录后刷新账号资料失败", "cookie_id", accountID, "err", profileErr)
		} else if profileResult.ErrorMessage != "" && s.Logger != nil {
			s.Logger.Warn("登录后刷新账号资料返回业务错误", "cookie_id", accountID, "err", profileResult.ErrorMessage)
		}
	}
	if s.Manager != nil && svc.repository.GetStatus(ctx, accountID) {
		// err 表示账号运行时重启错误。
		if err := s.Manager.Restart(ctx, accountID); err != nil && s.Logger != nil {
			s.Logger.Warn("账号登录后重启账号失败", "cookie_id", accountID, "err", err)
		}
	}
}

// ValidateCookieInput 校验账号标识和 Cookie 输入，供 HTTP 适配层复用一致规则。
func (svc *accountLoginService) ValidateCookieInput(input accountLoginInput) error {
	if strings.TrimSpace(input.AccountID) == "" || strings.TrimSpace(input.Cookies) == "" {
		return errors.New("缺少账号 ID 或 Cookie")
	}
	return nil
}
