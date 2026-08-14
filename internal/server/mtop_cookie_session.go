package server

import (
	"context"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// withMTopCookieSnapshot 为一次业务 MTOP 流程挂载账号 Cookie 会话。有完整
// Jar 时保持所有作用域；历史账号则使用不冒充完整 Jar 的 flat session，仍能
// 在成功或失败路径观察并持久化 Set-Cookie。
// withMTopCookieSnapshot 负责withMTop登录凭证Snapshot相关处理。
func withMTopCookieSnapshot(ctx context.Context, detail *db.CookieDetail) (context.Context, *mtop.CookieSession) {
	if detail == nil {
		return ctx, nil
	}
	// snapshot、ok 保存snapshot、ok，供当前处理流程使用
	snapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !ok {
		return mtop.WithFlatCookieSession(ctx, detail.Value)
	}
	return mtop.WithCookieSnapshot(ctx, snapshot)
}

// hasStoredCookieCredential 负责hasStored登录凭证Credential相关处理。
func hasStoredCookieCredential(detail *db.CookieDetail) bool {
	if detail == nil {
		return false
	}
	if detail.Value != "" {
		return true
	}
	// complete 保存complete，供当前处理流程使用
	_, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	return complete
}

// persistMTopCookieSessionLocked 原子保存业务 MTOP 响应后的完整 Cookie Jar。
// 调用方必须持有账号凭证锁，并且 detail 必须是加锁后重读的最新记录。
// handled 表示本次请求由完整 Cookie Jar 接管，或 session 已产生可持久化更新；
// 此时即使 Jar 未变化，也不得因扁平 Cookie 的顺序/尾分号差异退回
// UpdatedCookies 写回，否则会把刚保存的完整 Jar 清掉。
// persistMTopCookieSessionLocked 负责persistMTop登录凭证会话Locked相关处理。
func (s *Server) persistMTopCookieSessionLocked(
	ctx context.Context,
	detail *db.CookieDetail,
	session *mtop.CookieSession,
) (value string, valueChanged, handled bool, err error) {
	if detail == nil || session == nil {
		return "", false, false, nil
	}
	// value、snapshot、changed 保存value、snapshot、changed，供当前处理流程使用
	value, snapshot, changed := session.State()
	if !changed {
		if snapshot != nil {
			return detail.Value, false, true, nil
		}
		return "", false, false, nil
	}
	// metadata 保存metadata，供当前处理流程使用
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	if snapshot != nil {
		metadata = cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.Cookies.UpdateRenewalCookie(ctx, detail.ID, value, metadata, time.Now().Unix()); err != nil {
		return value, value != detail.Value, true, err
	}
	return value, value != detail.Value, true, nil
}
