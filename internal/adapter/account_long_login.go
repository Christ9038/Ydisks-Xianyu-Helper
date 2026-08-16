package adapter

import (
	"context"
	"errors"
	"log/slog"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// LongLoginClient 定义长登录平台客户端的最小协议，隔离 Server 与 renew 实现。
type LongLoginClient interface {
	// QueryLongLoginSettings 查询平台当前长登录状态，并返回响应 Cookie。
	QueryLongLoginSettings(context.Context, string, ...[]cookierefresh.BrowserCookie) (*xrenew.LongLoginSettings, error)
	// SetLongLoginSettings 更新平台长登录状态，并返回响应 Cookie。
	SetLongLoginSettings(context.Context, string, bool, ...[]cookierefresh.BrowserCookie) (*xrenew.LongLoginSettings, error)
}

// LongLoginAdapter 将长登录平台协议和 Cookie 持久化适配到账号应用端口。
type LongLoginAdapter struct {
	// repository 提供凭证锁、平台窄视图和 Cookie 写回能力。
	repository accountapp.CredentialRepository
	// service 返回当前 Server 配置的平台长登录客户端；函数形式允许测试在构造后替换客户端。
	service func() LongLoginClient
	// updateRunningCookie 在凭证锁释放后同步已变化的运行时 Cookie。
	updateRunningCookie func(context.Context, string, string)
	// logger 记录不包含明文 Cookie 的持久化错误。
	logger *slog.Logger
}

// NewLongLoginAdapter 构造长登录平台适配器。
func NewLongLoginAdapter(repository accountapp.CredentialRepository, service func() LongLoginClient, updateRunningCookie func(context.Context, string, string), logger *slog.Logger) *LongLoginAdapter {
	return &LongLoginAdapter{repository: repository, service: service, updateRunningCookie: updateRunningCookie, logger: logger}
}

// QueryLongLogin 查询长登录状态并持久化平台返回的 Cookie。
func (a *LongLoginAdapter) QueryLongLogin(ctx context.Context, accountID string) (accountapp.LongLoginResult, error) {
	return a.execute(ctx, accountID, nil)
}

// SetLongLogin 更新长登录开关并持久化平台返回的 Cookie。
func (a *LongLoginAdapter) SetLongLogin(ctx context.Context, accountID string, enabled bool) (accountapp.LongLoginResult, error) {
	return a.execute(ctx, accountID, &enabled)
}

// execute 在凭证锁内读取最新平台视图并完成平台调用，锁只覆盖凭证一致性和写回。
func (a *LongLoginAdapter) execute(ctx context.Context, accountID string, enabled *bool) (accountapp.LongLoginResult, error) {
	if a == nil || a.repository == nil || a.service == nil {
		return accountapp.LongLoginResult{}, errors.New("长登录适配器未初始化")
	}
	// unlock 保护本次平台 Cookie 读取和写回；平台网络请求必须在锁外执行。
	unlock := a.repository.LockCredentials(accountID)
	// detail 保存平台调用所需的 Cookie 与加密 metadata 窄视图。
	detail, loadErr := a.repository.LoadPlatformDetail(ctx, accountID)
	unlock()
	if loadErr != nil {
		return accountapp.LongLoginResult{}, loadErr
	}
	if detail == nil {
		return accountapp.LongLoginResult{}, accountapp.ErrNotFound
	}
	// requestCookies 保存按请求域过滤后的 Cookie；snapshot 保存完整 Cookie Jar（如存在）。
	requestCookies, snapshot := longLoginCookies(detail, queryURL(enabled))
	// platformResult 保存平台返回的非敏感长登录状态；requestErr 保留平台业务或网络错误。
	var platformResult *xrenew.LongLoginSettings
	// requestErr 保存平台请求失败；即使失败，响应 Set-Cookie 也可能已完成持久化。
	var requestErr error
	// client 是本次请求使用的长登录平台客户端。
	client := a.service()
	if enabled == nil {
		if snapshot != nil {
			platformResult, requestErr = client.QueryLongLoginSettings(ctx, requestCookies, snapshot)
		} else {
			platformResult, requestErr = client.QueryLongLoginSettings(ctx, requestCookies)
		}
	} else if snapshot != nil {
		platformResult, requestErr = client.SetLongLoginSettings(ctx, requestCookies, *enabled, snapshot)
	} else {
		platformResult, requestErr = client.SetLongLoginSettings(ctx, requestCookies, *enabled)
	}
	// credentialChanged 表示平台响应使本地凭证或完整 Cookie Jar 发生变化。
	credentialChanged, persistErr := a.persist(ctx, detail, platformResult, queryURL(enabled))
	if persistErr != nil {
		return accountapp.LongLoginResult{}, persistErr
	}
	if credentialChanged && a.updateRunningCookie != nil {
		a.updateRunningCookie(ctx, accountID, platformResult.NewCookies)
	}
	if requestErr != nil {
		return toLongLoginResult(platformResult), errors.Join(accountapp.ErrLongLoginPlatform, requestErr)
	}
	return toLongLoginResult(platformResult), nil
}

// queryURL 返回当前长登录操作对应的官方请求地址。
func queryURL(enabled *bool) string {
	if enabled == nil {
		return xrenew.QueryLoginSettingsURL
	}
	return xrenew.SetLoginSettingsURL
}

// longLoginCookies 从平台窄视图提取请求 Cookie 和完整快照，历史扁平账号不伪造快照。
func longLoginCookies(detail *accountapp.CredentialDetail, requestURL string) (string, []cookierefresh.BrowserCookie) {
	if detail == nil {
		return "", nil
	}
	// snapshot、complete 保存 metadata 中的完整 Cookie 快照及其权威标记。
	if snapshot, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); complete {
		// header、authoritative 保存按平台请求 URL 过滤后的 Cookie 头及完整性标记。
		header, authoritative := cookierefresh.ScopedCookieHeaderForRequest(snapshot, requestURL, "https://goofish.com", time.Now())
		_ = authoritative
		return header, snapshot
	}
	return detail.Value, nil
}

// persist 合并平台响应 Cookie 并在凭证锁内保存最新扁平 Cookie 与完整快照。
func (a *LongLoginAdapter) persist(ctx context.Context, detail *accountapp.CredentialDetail, result *xrenew.LongLoginSettings, requestURL string) (bool, error) {
	if result == nil || detail == nil {
		return false, nil
	}
	// unlock 保护加锁后重读和最终写回，避免平台请求期间持有凭证锁。
	unlock := a.repository.LockCredentials(detail.ID)
	defer unlock()
	// latest 保存平台调用期间可能被其他流程更新后的最新凭证视图。
	latest, loadErr := a.repository.LoadPlatformDetail(ctx, detail.ID)
	if loadErr != nil || latest == nil {
		if a.logger != nil {
			a.logger.Warn("读取长登录最新凭证失败", "cookie_id", detail.ID, "err", loadErr)
		}
		return false, loadErr
	}
	// metadata 保存合并响应 Cookie 后的加密元数据。
	metadata := latest.MetadataJSON
	// snapshot、hasSnapshot 保存现有完整 Cookie Jar及其权威标志。
	snapshot, hasSnapshot := cookierefresh.SnapshotFromMetadataOK(latest.MetadataJSON)
	if result.CookieSnapshotComplete {
		snapshot = cookierefresh.NormalizeSnapshot(result.CookieSnapshot)
		if snapshot == nil {
			snapshot = []cookierefresh.BrowserCookie{}
		}
		result.NewCookies, _ = cookierefresh.ScopedCookieHeaderForRequest(snapshot, "https://www.goofish.com/im", "https://goofish.com", time.Now())
		metadata = cookierefresh.MetadataWithSnapshot(metadata, snapshot)
	} else if hasSnapshot {
		snapshot = cookierefresh.ApplySetCookies(snapshot, requestURL, result.SetCookies, time.Now(), "https://goofish.com")
		if snapshot == nil {
			snapshot = []cookierefresh.BrowserCookie{}
		}
		result.NewCookies, _ = cookierefresh.ScopedCookieHeaderForRequest(snapshot, "https://www.goofish.com/im", "https://goofish.com", time.Now())
		metadata = cookierefresh.MetadataWithSnapshot(metadata, snapshot)
	} else {
		result.NewCookies = xrenew.MergeSetCookies(latest.Value, result.SetCookies)
		metadata = cookierefresh.MetadataWithoutSnapshot(metadata)
	}
	// changed 表示写回后 Cookie 或 metadata 是否发生变化。
	changed := result.NewCookies != latest.Value || metadata != latest.MetadataJSON
	if !changed && len(result.SetCookies) == 0 {
		return false, nil
	}
	// persistErr 保存长登录 Cookie 与 metadata 的持久化错误。
	if persistErr := a.repository.UpdateRenewalCookie(ctx, detail.ID, result.NewCookies, metadata, time.Now().Unix()); persistErr != nil {
		if a.logger != nil {
			a.logger.Warn("保存长登录 Cookie 失败", "cookie_id", detail.ID, "err", persistErr)
		}
		return false, persistErr
	}
	if changed {
		// clearErr 保存凭证变化后清理旧连接 Token 的错误；不回滚已成功的 Cookie 写入。
		if clearErr := a.repository.ClearTokens(ctx, detail.ID); clearErr != nil && a.logger != nil {
			a.logger.Warn("长登录 Cookie 保存后清理旧连接凭证失败", "cookie_id", detail.ID, "err", clearErr)
		}
	}
	return changed, nil
}

// toLongLoginResult 将平台结果限制为不含 Cookie 的应用层结果。
func toLongLoginResult(result *xrenew.LongLoginSettings) accountapp.LongLoginResult {
	if result == nil {
		return accountapp.LongLoginResult{}
	}
	return accountapp.LongLoginResult{CanOpenLongLogin: result.CanOpenLongLogin, Enabled: result.Enabled}
}
