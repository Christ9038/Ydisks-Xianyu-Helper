package adapter

import (
	"context"
	"errors"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// CookieSession 是一次平台请求流程使用的 Cookie 会话实现别名；Server 通过本适配器访问它，避免直接依赖平台包。
type CookieSession = mtop.CookieSession

// BrowserCookie 是浏览器 Cookie 快照的适配器别名，仅用于测试和平台边界转换。
type BrowserCookie = cookierefresh.BrowserCookie

// WithCookieSnapshot 为平台请求上下文安装完整 Cookie 快照，并保留权威 Jar 的更新状态。
func WithCookieSnapshot(ctx context.Context, snapshot []BrowserCookie) (context.Context, *CookieSession) {
	return mtop.WithCookieSnapshot(ctx, snapshot)
}

// WithFlatCookieSession 为历史扁平 Cookie 安装兼容会话，不伪造完整 Jar 属性。
func WithFlatCookieSession(ctx context.Context, cookies string) (context.Context, *CookieSession) {
	return mtop.WithFlatCookieSession(ctx, cookies)
}

// SnapshotFromMetadata 读取完整 Cookie 快照；解析失败或 metadata 不完整时返回 false。
func SnapshotFromMetadata(metadata string) ([]BrowserCookie, bool) {
	return cookierefresh.SnapshotFromMetadataOK(metadata)
}

// HasStoredCookieCredential 判断凭证视图是否包含扁平 Cookie 或完整 Cookie 快照。
func HasStoredCookieCredential(detail *accountapp.CredentialDetail) bool {
	if detail == nil {
		return false
	}
	if detail.Value != "" {
		return true
	}
	// complete 表示 metadata 是否包含可用于平台请求的完整快照。
	_, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	return complete
}

// PersistCookieSessionLocked 持久化平台响应后的 Cookie 会话；调用方必须已持有凭证锁。
func PersistCookieSessionLocked(
	ctx context.Context,
	repository accountapp.CredentialRepository,
	detail *accountapp.CredentialDetail,
	session *CookieSession,
) (value string, valueChanged, handled bool, err error) {
	if detail == nil || session == nil {
		return "", false, false, nil
	}
	// value、snapshot、changed 保存平台会话当前 Cookie、完整快照及变更状态。
	value, snapshot, changed := session.State()
	if !changed {
		if snapshot != nil {
			return detail.Value, false, true, nil
		}
		return "", false, false, nil
	}
	// metadata 保存去除旧快照或合并新快照后的凭证元数据。
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	if snapshot != nil {
		metadata = cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
	}
	if repository == nil {
		return value, value != detail.Value, true, errors.New("cookie 会话持久化 repository 未初始化")
	}
	// persistErr 保存凭证 Port 写回结果；明文 Cookie 不离开当前适配器调用边界。
	persistErr := repository.UpdateRenewalCookie(ctx, detail.ID, value, metadata, time.Now().Unix())
	if persistErr != nil {
		return value, value != detail.Value, true, persistErr
	}
	return value, value != detail.Value, true, nil
}
