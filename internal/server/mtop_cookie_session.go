package server

import (
	"context"
	"errors"

	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
)

// persistMTopCookieSessionLocked 原子保存业务 MTOP 响应后的完整 Cookie Jar。
// 调用方必须持有账号凭证锁，并且 detail 必须是加锁后重读的最新记录。
// handled 表示本次请求由完整 Cookie Jar 接管，或 session 已产生可持久化更新；
// 此时即使 Jar 未变化，也不得因扁平 Cookie 的顺序/尾分号差异退回
// UpdatedCookies 写回，否则会把刚保存的完整 Jar 清掉。
// persistMTopCookieSessionLocked 封装persistMTop登录凭证会话Locked业务协调。
func (s *Server) persistMTopCookieSessionLocked(
	ctx context.Context,
	detail *accountapp.CredentialDetail,
	session *adapter.CookieSession,
) (value string, valueChanged, handled bool, err error) {
	if s == nil {
		return "", false, true, errors.New("cookie 会话持久化服务未初始化")
	}
	// sessionPort 只负责在凭证适配器内保存 Cookie 与加密 metadata；Server 不直接访问 Store。
	sessionPort := s.platformCredentialSessionPort()
	return adapter.PersistCookieSessionLocked(ctx, sessionPort, detail, session)
}
