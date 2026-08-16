package server

import (
	"errors"
	"net/http"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/auth"
)

// authSess 从请求上下文读取经过认证中间件注入的会话。
func authSess(r *http.Request) *auth.SessionIdentity {
	return auth.IdentityFromContext(r.Context())
}

// requireCookieOwner 负责require登录凭证所有者相关处理。
func (s *Server) requireCookieOwner(w http.ResponseWriter, r *http.Request, cookieID string) (accountapp.AccountSummary, bool) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return accountapp.AccountSummary{}, false
	}
	// d、err 保存d、err，供当前处理流程使用
	d, err := s.loadCookieSummaryDetail(r.Context(), sess.UserID, cookieID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return accountapp.AccountSummary{}, false
	}
	if d.UserID != sess.UserID {
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return accountapp.AccountSummary{}, false
	}
	return d, true
}

// requireCookieOwnership 校验当前会话是否拥有账号，只读取账号所有权元数据，不解密凭证。
func (s *Server) requireCookieOwnership(w http.ResponseWriter, r *http.Request, cookieID string) bool {
	// sess 是当前请求经过认证中间件注入的会话。
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return false
	}
	// service 提供按用户过滤的非敏感所有权判断，避免 Server 直接访问账号表。
	service := s.accountSummaryApplication()
	if service == nil {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return false
	}
	// ownershipErr 保存应用服务返回的账号归属结果。
	if ownershipErr := service.RequireOwnership(r.Context(), sess.UserID, cookieID); ownershipErr == nil {
		return true
	} else if errors.Is(ownershipErr, accountapp.ErrForbidden) {
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return false
	}
	writeErr(w, http.StatusNotFound, "账号不存在")
	return false
}
