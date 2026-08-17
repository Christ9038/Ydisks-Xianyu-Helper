package adapter

import (
	"context"
	"errors"
	"log/slog"

	accountapp "xianyu-go/internal/application/account"
)

// AccountCookieWriter 将手动登录产生的明文 Cookie 限制在账号基础设施适配器内。
// 该类型同时实现新增和更新两个应用端口；Cookie 只在当前请求创建的实例中短暂存在。
type AccountCookieWriter struct {
	// repository 提供凭证锁、归属校验、加密写入和旧 Token 清理能力。
	repository accountapp.CredentialRepository
	// platformCredentials 提供更新既有账号时所需的平台窄凭证视图。
	platformCredentials accountapp.PlatformCredentialViewPort
	// cookies 保存当前请求的明文 Cookie；禁止写入日志、HTTP 响应或长期状态。
	cookies string
	// logger 记录旧 Token 清理失败；日志中不得包含 Cookie 内容。
	logger *slog.Logger
}

// NewAccountCookieWriter 构造一次请求范围的 Cookie 写入适配器。
// 调用方应在使用后丢弃返回实例，避免明文 Cookie 跨请求存活。
func NewAccountCookieWriter(
	repository accountapp.CredentialRepository,
	platformCredentials accountapp.PlatformCredentialViewPort,
	cookies string,
	logger *slog.Logger,
) *AccountCookieWriter {
	return &AccountCookieWriter{
		repository: repository, platformCredentials: platformCredentials, cookies: cookies, logger: logger,
	}
}

// CreateOwnedCookie 在凭证锁内原子校验归属、写入 Cookie 并清理旧连接 Token。
// 锁只覆盖数据库凭证变更，不跨越外部网络或运行时操作；清理失败不会回滚主写入。
func (w *AccountCookieWriter) CreateOwnedCookie(ctx context.Context, accountID string, userID int64) error {
	if w == nil || w.repository == nil {
		return errors.New("账号登录凭证 repository 未初始化")
	}
	// unlock 保护同一账号的凭证写入与旧连接 Token 清理，外部网络和运行时操作在锁外执行。
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

// UpdateOwnedCookie 在账号凭证短锁内完成归属复核、版本检查、Cookie 写入和旧 Token 清理。
// 资料刷新等慢速后续动作由应用服务在释放锁后触发。
func (w *AccountCookieWriter) UpdateOwnedCookie(ctx context.Context, accountID string, userID, expectedRevision int64) error {
	if w == nil || w.repository == nil || w.platformCredentials == nil {
		return errors.New("账号登录凭证端口未初始化")
	}
	// unlock 只保护凭证快照读取、数据库写入和旧 Token 清理，不跨越网络或运行时 I/O。
	unlock := w.repository.LockCredentials(accountID)
	defer unlock()
	// detail 保存锁内读取的平台凭证窄视图；该视图不会把登录密码传入应用层。
	detail, loadErr := w.platformCredentials.LoadPlatformDetail(ctx, accountID)
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

var _ accountapp.CookieWriter = (*AccountCookieWriter)(nil)
var _ accountapp.CookieUpdater = (*AccountCookieWriter)(nil)
