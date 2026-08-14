package server

import (
	"net/http"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

func (s *Server) requireCookieOwner(w http.ResponseWriter, r *http.Request, cookieID string) (*db.CookieDetail, bool) {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return nil, false
	}
	d, err := s.loadCookieSummaryDetail(r.Context(), sess.UserID, cookieID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return nil, false
	}
	if d.UserID != sess.UserID {
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return nil, false
	}
	return d, true
}

// requireCookieSecretOwner 校验账号归属后读取登录设置所需的完整详情，仅供需要密码的管理流程使用。
func (s *Server) requireCookieSecretOwner(w http.ResponseWriter, r *http.Request, cookieID string) (*db.CookieDetail, bool) {
	// ownerOK 表示当前会话是否通过账号所有权校验。
	_, ownerOK := s.requireCookieOwner(w, r, cookieID)
	if !ownerOK {
		return nil, false
	}
	// detail 是登录设置流程需要的用户名、密码和浏览器显示设置。
	detail, err := s.Store.Cookies.GetDetails(r.Context(), cookieID)
	if err != nil || detail == nil {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return nil, false
	}
	return detail, true
}

// requireCookieOwnership 校验当前会话是否拥有账号，只读取账号所有权元数据，不解密凭证。
func (s *Server) requireCookieOwnership(w http.ResponseWriter, r *http.Request, cookieID string) bool {
	// sess 是当前请求经过认证中间件注入的会话。
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return false
	}
	// owned 表示当前用户是否直接拥有该账号。
	owned, ownedErr := s.Store.Cookies.ExistsOwned(r.Context(), sess.UserID, cookieID)
	if ownedErr != nil {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return false
	}
	if owned {
		return true
	}
	// ownerID 用于区分账号不存在和账号属于其他用户，保持原有 404/403 响应语义。
	ownerID, ownerErr := s.Store.Cookies.GetOwnerID(r.Context(), cookieID)
	if ownerErr != nil {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return false
	}
	if ownerID != sess.UserID {
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return false
	}
	return true
}

// requireCardOwner 校验当前会话是否拥有卡券组。
func (s *Server) requireCardOwner(w http.ResponseWriter, r *http.Request, cardID int64) (*db.CardFull, bool) {
	// sess 是当前请求经过认证中间件注入的会话。
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return nil, false
	}
	// card 和 err 分别表示卡券组记录及其查询错误。
	card, err := s.Store.Cards.Get(r.Context(), cardID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "卡券不存在")
		return nil, false
	}
	if card.UserID != sess.UserID {
		writeErr(w, http.StatusForbidden, "无权操作该卡密组")
		return nil, false
	}
	return card, true
}

// requireChannelOwner 校验当前会话是否拥有通知渠道。
func (s *Server) requireChannelOwner(w http.ResponseWriter, r *http.Request, channelID int64) bool {
	// sess 是当前请求经过认证中间件注入的会话。
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return false
	}
	// exists 和 err 表示通知渠道归属查询结果及错误。
	exists, err := s.Store.Notifications.OwnsChannel(r.Context(), channelID, sess.UserID)
	if err != nil || !exists {
		writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
		return false
	}
	return true
}
