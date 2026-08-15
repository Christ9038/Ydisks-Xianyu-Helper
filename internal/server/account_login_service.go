package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/protocol"
)

// accountLoginService 是账号登录相关应用服务，负责凭证写入、登录审计、资料刷新和运行时重启编排。
type accountLoginService struct {
	// server 提供账号存储、运行时管理器和扫码会话持久化依赖。
	server *Server
	// repository 提供账号登录服务所需的最小凭证持久化能力。
	repository accountLoginRepository
}

// serverAccountProfilePort 将平台资料刷新适配为账号应用层 Port。
type serverAccountProfilePort struct {
	// server 提供平台会话、凭证锁、资料保存和运行时同步能力。
	server *Server
}

// RefreshProfile 执行平台资料刷新，并把平台结果转换为应用层 DTO。
func (p serverAccountProfilePort) RefreshProfile(ctx context.Context, input accountapp.ProfileInput) (accountapp.ProfileResult, error) {
	// detail 是兼容 Server 资料刷新器所需的非敏感账号摘要模型。
	detail := &db.CookieDetail{
		ID: input.Summary.ID, UserID: input.Summary.UserID,
		Remark: input.Summary.Remark, Nickname: input.Summary.Nickname,
		AvatarURL: input.Summary.AvatarURL,
	}
	// nickname、avatarURL 和 profileErr 保存平台资料刷新后的展示结果。
	nickname, avatarURL, profileErr := p.server.refreshAccountProfile(ctx, detail)
	return accountapp.ProfileResult{
		AccountID: input.AccountID, Nickname: nickname,
		AvatarURL: avatarURL, ErrorMessage: profileErr,
	}, nil
}

// newAccountProfileApplication 从 Server 的数据库和平台能力构造账号资料应用服务。
func newAccountProfileApplication(server *Server) (*accountapp.ProfileService, error) {
	// repository 是只返回非敏感摘要的数据库适配器。
	repository := storeAccountProfileRepository{store: server.Store}
	// profilePort 是负责平台请求和资料持久化的 Server 适配器。
	profilePort := serverAccountProfilePort{server: server}
	return accountapp.NewProfileService(repository, profilePort)
}

// accountLoginApplication 返回当前 Server 绑定的账号登录应用服务。
func (s *Server) accountLoginApplication() *accountLoginService {
	return s.applicationServiceSet().accountLogin
}

// accountLoginRepositoryForServer 返回当前 Server 装配的账号登录持久化边界。
func (s *Server) accountLoginRepositoryForServer() accountLoginRepository {
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
}

// CreateCookie 创建账号凭证并完成登录审计、资料刷新和运行时重启。
func (svc *accountLoginService) CreateCookie(ctx context.Context, input accountLoginInput) error {
	// s 是当前账号登录应用服务依赖的 Server。
	s := svc.server
	// unlock 保护账号凭证的创建与后续登录状态清理。
	unlock := svc.repository.LockCredentials(input.AccountID)
	// err 表示账号凭证创建错误。
	if err := svc.repository.CreateCookieOwned(ctx, input.AccountID, input.Cookies, input.UserID); err != nil {
		unlock()
		return err
	}
	{
		// err 表示清理旧连接凭证的错误，仅记录不阻断登录。
		if err := svc.repository.ClearTokens(ctx, input.AccountID); err != nil && s.Logger != nil {
			s.Logger.Warn("新增账号后清理旧连接凭证失败", "cookie_id", input.AccountID, "err", err)
		}
	}
	// loginMethod 是归一化后的登录方式。
	loginMethod := normalizeLoginMethod(input.LoginMethod)
	if loginMethod == "" {
		loginMethod = loginMethodManual
	}
	s.markSuccessfulLogin(ctx, input.AccountID, input.UserID, loginMethod, "账号登录成功")
	unlock()
	svc.refreshAndRestartAccount(ctx, input.UserID, input.AccountID)
	return nil
}

// UpdateCookie 更新账号凭证并完成登录审计、资料刷新和运行时重启。
func (svc *accountLoginService) UpdateCookie(ctx context.Context, input accountCookieUpdateInput) error {
	// s 是当前账号登录应用服务依赖的 Server。
	s := svc.server
	// unlock 保护账号凭证更新、连接凭证清理和登录审计。
	unlock := svc.repository.LockCredentials(input.AccountID)
	// detail 和 err 保存账号凭证详情查询结果。
	detail, err := s.loadCookiePlatformDetail(ctx, input.AccountID)
	if err != nil || detail == nil || detail.UserID != input.UserID {
		unlock()
		if err == nil {
			return db.ErrNotFound
		}
		return err
	}
	// err 表示扁平 Cookie 更新错误。
	if err := svc.repository.UpdateFlatCookieOwned(ctx, detail, input.Cookies); err != nil {
		unlock()
		return err
	}
	{
		// err 表示清理旧连接凭证的错误，仅记录不阻断登录。
		if err := svc.repository.ClearTokens(ctx, input.AccountID); err != nil && s.Logger != nil {
			s.Logger.Warn("更新账号后清理旧连接凭证失败", "cookie_id", input.AccountID, "err", err)
		}
	}
	// loginMethod 是可选的归一化登录方式。
	if loginMethod := normalizeLoginMethod(input.LoginMethod); loginMethod != "" {
		s.markSuccessfulLogin(ctx, input.AccountID, input.UserID, loginMethod, "账号登录成功")
	}
	unlock()
	svc.refreshAndRestartAccount(ctx, input.UserID, input.AccountID)
	return nil
}

// PersistQRLoginSuccess 持久化扫码登录结果，复用会话级幂等锁和账号重启流程。
func (svc *accountLoginService) PersistQRLoginSuccess(ctx context.Context, userID int64, sessionID string, result map[string]any, targetAccountID string) (qrLoginPersistence, error) {
	return svc.persistQRLoginSuccessCore(ctx, userID, sessionID, result, targetAccountID)
}

// persistQRLoginSuccessCore 执行扫码结果校验、凭证合并、登录审计和账号重启。
func (svc *accountLoginService) persistQRLoginSuccessCore(ctx context.Context, userID int64, sessionID string, result map[string]any, targetAccountID string) (qrLoginPersistence, error) {
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
	// cookies、cookieSnapshot 和 snapshotComplete 保存平台登录凭证及其完整 Cookie Jar。
	cookies := qrString(result, "cookies")
	// cookieSnapshot 和 snapshotComplete 保存平台返回的完整 Cookie Jar。
	cookieSnapshot, snapshotComplete := qrCookieSnapshot(result)
	// scannedAccountID 是扫码结果中的平台账号标识。
	scannedAccountID := strings.TrimSpace(firstNonEmpty(qrString(result, "unb"), protocol.TransCookies(cookies)["unb"]))
	if cookies == "" || scannedAccountID == "" {
		return qrLoginPersistence{}, errors.New("扫码结果缺少 cookies 或 unb")
	}
	// accountID 是最终写入的账号标识。
	accountID := strings.TrimSpace(targetAccountID)
	if accountID == "" {
		accountID = scannedAccountID
	} else if accountID != scannedAccountID {
		return qrLoginPersistence{}, errors.New("扫码账号与待重新授权账号不一致，已拒绝覆盖")
	}

	// isNew 标记本次扫码是否创建了新账号。
	isNew := false
	// credentialUnlock 保护账号凭证写入和登录审计。
	credentialUnlock := svc.repository.LockCredentials(accountID)
	// saveErr 保存账号凭证、Cookie Jar 和登录审计的事务错误。
	saveErr := func() error {
		defer credentialUnlock()
		// detail 和 err 保存待更新账号的凭证详情。
		detail, err := s.loadCookiePlatformDetail(ctx, accountID)
		switch {
		case errors.Is(err, db.ErrNotFound):
			if targetAccountID != "" {
				return errors.New("待重新授权账号不存在")
			}
			isNew = true
			// err 表示创建扫码账号的错误。
			if err := svc.repository.CreateCookieOwned(ctx, accountID, cookies, userID); err != nil {
				return err
			}
			if snapshotComplete {
				// metadata 是新账号的完整 Cookie Jar 元数据。
				metadata := cookierefresh.MetadataWithSnapshot("", cookieSnapshot)
				// err 表示保存新账号 Cookie Jar 的错误。
				if err := svc.repository.UpdateRenewalCookie(ctx, accountID, cookies, metadata, time.Now().Unix()); err != nil {
					return err
				}
			}
		case err != nil:
			return err
		case detail == nil:
			return db.ErrNotFound
		case detail.UserID != userID:
			if targetAccountID != "" {
				return errors.New("待重新授权账号不属于当前用户")
			}
			return db.ErrForbidden
		default:
			if snapshotComplete {
				// metadata 是已有账号合并后的 Cookie Jar 元数据。
				metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, cookieSnapshot)
				// err 表示保存已有账号 Cookie Jar 的错误。
				if err := svc.repository.UpdateRenewalCookie(ctx, detail.ID, cookies, metadata, time.Now().Unix()); err != nil {
					return err
				}
				// err 表示合并扁平 Cookie 的错误。
			} else if err := svc.repository.UpdateFlatCookieOwned(ctx, detail, cookies); err != nil {
				return err
			}
		}
		s.markSuccessfulLogin(ctx, accountID, userID, loginMethodQRScan, "扫码登录成功")
		{
			// err 表示清理旧连接凭证的错误，仅记录不阻断扫码登录。
			if err := svc.repository.ClearTokens(ctx, accountID); err != nil && s.Logger != nil {
				s.Logger.Warn("扫码登录保存后清理旧连接凭证失败", "cookie_id", accountID, "err", err)
			}
		}
		return nil
	}()
	if saveErr != nil {
		if errors.Is(saveErr, db.ErrForbidden) {
			return qrLoginPersistence{}, errors.New("该账号ID已存在且不属于当前用户")
		}
		if errors.Is(saveErr, db.ErrAlreadyExists) {
			return qrLoginPersistence{}, errors.New("该账号ID已被并发创建，请重新获取账号状态")
		}
		return qrLoginPersistence{}, saveErr
	}
	// detail 和 err 保存登录成功后的资料摘要查询结果。
	if detail, err := s.loadCookieSummaryDetail(ctx, userID, accountID); err == nil {
		s.refreshAccountProfile(ctx, detail)
	}
	s.wakeCredentialBlockedAutomation(ctx, accountID)
	if s.Manager != nil && svc.repository.GetStatus(ctx, accountID) {
		// err 表示扫码登录后的账号运行时重启错误。
		if err := s.Manager.Restart(ctx, accountID); err != nil && s.Logger != nil {
			s.Logger.Warn("扫码登录后重启账号失败", "cookie_id", accountID, "err", err)
		}
	}
	// persisted 是扫码会话幂等返回值。
	persisted := qrLoginPersistence{AccountID: accountID, IsNew: isNew, UserID: userID, CreatedAt: time.Now().UTC()}
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
	detail, err := s.loadCookieSummaryDetail(ctx, userID, accountID)
	if err == nil {
		s.refreshAccountProfile(ctx, detail)
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
