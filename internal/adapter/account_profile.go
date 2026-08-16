package adapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/mtop"
)

// AccountProfilePort 将平台资料查询和账号资料持久化适配到账号应用层。
type AccountProfilePort struct {
	// repository 提供凭证快照读取、Cookie 写回和资料保存能力。
	repository *AccountLoginRepository
	// client 提供平台资料查询能力。
	client func() mtop.Client
	// updateRunningCookie 在凭证锁释放后同步运行时 Cookie。
	updateRunningCookie func(context.Context, string, string)
	// recoverSession 在平台返回会话失效时触发账号恢复。
	recoverSession func(context.Context, string, error) bool
	// logger 记录不包含凭证内容的资料刷新错误。
	logger *slog.Logger
}

// NewAccountProfilePort 构造账号资料平台适配器。
func NewAccountProfilePort(repository *AccountLoginRepository, client func() mtop.Client, updateRunningCookie func(context.Context, string, string), recoverSession func(context.Context, string, error) bool, logger *slog.Logger) *AccountProfilePort {
	return &AccountProfilePort{repository: repository, client: client, updateRunningCookie: updateRunningCookie, recoverSession: recoverSession, logger: logger}
}

// RefreshProfile 调用平台资料接口、持久化响应 Cookie 和展示资料。
func (p *AccountProfilePort) RefreshProfile(ctx context.Context, input accountapp.ProfileInput) (accountapp.ProfileResult, error) {
	if p == nil || p.repository == nil || p.client == nil || p.client() == nil {
		return accountapp.ProfileResult{}, errors.New("账号资料适配器未初始化")
	}
	// fallbackNickname、fallbackAvatar 保存平台失败时的非敏感展示兜底。
	fallbackNickname, fallbackAvatar := cachedProfile(input.Summary)
	// unlock 保护平台凭证快照读取和响应写回；网络请求发生在解锁后。
	unlock := p.repository.LockCredentials(input.AccountID)
	// detail、loadErr 保存锁外读取的平台凭证窄视图及其错误。
	detail, loadErr := p.repository.LoadPlatformDetail(ctx, input.AccountID)
	unlock()
	if loadErr != nil || detail == nil || detail.UserID != input.UserID {
		if loadErr == nil {
			loadErr = accountapp.ErrNotFound
		}
		return accountapp.ProfileResult{AccountID: input.AccountID, Nickname: fallbackNickname, AvatarURL: fallbackAvatar, ErrorMessage: truncateProfileError(loadErr)}, nil
	}
	// requestCtx、session 保存带完整 Jar 或扁平 Cookie 的平台请求上下文。
	requestCtx, session := withProfileCookieSession(ctx, detail)
	// profile、callErr 保存平台资料结果和调用错误；响应 Cookie 仍会继续处理。
	profile, callErr := p.client().FetchUserProfile(requestCtx, detail.Value)
	// value、changed、handled、persistErr 保存响应 Cookie 会话写回结果。
	value, changed, handled, persistErr := p.persistProfileSession(ctx, detail, session)
	if persistErr != nil {
		callErr = errors.Join(callErr, fmt.Errorf("保存账号资料响应凭证: %w", persistErr))
	} else if changed && p.updateRunningCookie != nil {
		p.updateRunningCookie(ctx, input.AccountID, value)
	}
	if !handled && callErr == nil && profile != nil && profile.UpdatedCookies != "" && profile.UpdatedCookies != detail.Value {
		// flatErr 保存历史扁平 Cookie 写回错误；没有完整 Jar 时才走该兼容分支。
		if flatErr := p.repository.UpdateFlatCookieOwned(ctx, detail, profile.UpdatedCookies); flatErr != nil {
			callErr = errors.Join(callErr, fmt.Errorf("保存账号资料响应凭证: %w", flatErr))
		} else if p.updateRunningCookie != nil {
			p.updateRunningCookie(ctx, input.AccountID, profile.UpdatedCookies)
		}
	}
	if callErr != nil {
		if p.recoverSession != nil {
			p.recoverSession(ctx, input.AccountID, callErr)
		}
		if p.logger != nil {
			p.logger.Warn("刷新账号资料失败", "account", input.AccountID, "err", callErr)
		}
		return accountapp.ProfileResult{AccountID: input.AccountID, Nickname: fallbackNickname, AvatarURL: fallbackAvatar, ErrorMessage: truncateProfileError(callErr)}, nil
	}
	if profile == nil {
		return accountapp.ProfileResult{AccountID: input.AccountID, Nickname: fallbackNickname, AvatarURL: fallbackAvatar, ErrorMessage: "账号资料接口未返回结果"}, nil
	}
	// nickname 保存归一化后的平台昵称；avatarURL 保存归一化后的非敏感头像地址。
	nickname := strings.TrimSpace(profile.Nickname)
	// avatarURL 保存归一化后的非敏感平台头像地址。
	avatarURL := normalizeProfileAvatar(profile.AvatarURL)
	// saveErr 保存资料展示字段写回错误；写回失败不覆盖已获得的平台资料。
	if saveErr := p.repository.UpdateProfile(ctx, input.AccountID, nickname, avatarURL); saveErr != nil && p.logger != nil {
		p.logger.Warn("保存账号资料失败", "account", input.AccountID, "err", saveErr)
	}
	if nickname == "" {
		nickname = fallbackNickname
	}
	if avatarURL == "" {
		avatarURL = fallbackAvatar
	}
	return accountapp.ProfileResult{AccountID: input.AccountID, Nickname: nickname, AvatarURL: avatarURL}, nil
}

// withProfileCookieSession 将平台凭证窄视图转换为带快照更新能力的请求会话。
func withProfileCookieSession(ctx context.Context, detail *accountapp.CredentialDetail) (context.Context, *CookieSession) {
	// snapshot、ok 保存 metadata 中的完整 Cookie 快照及其有效标记。
	if snapshot, ok := SnapshotFromMetadata(detail.MetadataJSON); ok {
		return WithCookieSnapshot(ctx, snapshot)
	}
	return WithFlatCookieSession(ctx, detail.Value)
}

// persistProfileSession 在短凭证锁内保存平台响应 Cookie，避免慢速网络位于锁内。
func (p *AccountProfilePort) persistProfileSession(ctx context.Context, detail *accountapp.CredentialDetail, session *CookieSession) (string, bool, bool, error) {
	// unlock 保护响应 Cookie 写回期间的账号凭证一致性。
	unlock := p.repository.LockCredentials(detail.ID)
	defer unlock()
	// latest、loadErr 保存加锁后重读的最新凭证视图及读取错误。
	latest, loadErr := p.repository.LoadPlatformDetail(ctx, detail.ID)
	if loadErr != nil {
		return "", false, true, loadErr
	}
	return PersistCookieSessionLocked(ctx, p.repository, latest, session)
}

// cachedProfile 生成资料失败时使用的本地非敏感展示值。
func cachedProfile(summary accountapp.Summary) (string, string) {
	// nickname 保存资料失败时优先使用的备注或缓存昵称。
	nickname := strings.TrimSpace(summary.Remark)
	if nickname == "" {
		nickname = strings.TrimSpace(summary.Nickname)
	}
	if nickname == "" {
		nickname = "账号 " + shortProfileID(summary.ID)
	}
	return nickname, normalizeProfileAvatar(summary.AvatarURL)
}

// normalizeProfileAvatar 统一平台返回的头像协议，避免把 HTTP 头像写回展示资料。
func normalizeProfileAvatar(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") {
		return "https://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
}

// truncateProfileError 限制资料刷新错误长度，避免平台响应原文过长进入 HTTP。
func truncateProfileError(err error) string {
	if err == nil {
		return ""
	}
	// message 保存待截断的错误文本，避免平台原文无限进入响应。
	message := err.Error()
	if len(message) > 180 {
		return message[:180]
	}
	return message
}

// shortProfileID 限制兜底展示中的账号标识长度，避免错误响应或日志暴露完整标识。
func shortProfileID(accountID string) string {
	if len(accountID) > 6 {
		return accountID[:6]
	}
	return accountID
}
