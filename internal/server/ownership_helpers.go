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
	d, err := s.Store.Cookies.GetDetails(r.Context(), cookieID)
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

// requireOrderOwner 校验当前会话是否拥有订单关联的账号。
func (s *Server) requireOrderOwner(w http.ResponseWriter, r *http.Request, orderID string) (*db.Order, bool) {
	// sess 是当前请求经过认证中间件注入的会话。
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return nil, false
	}
	// order 和 err 分别表示订单记录及其查询错误。
	order, err := s.Store.Orders.Get(r.Context(), orderID)
	if err != nil || order == nil {
		writeErr(w, http.StatusNotFound, "订单不存在")
		return nil, false
	}
	if order.CookieID == "" {
		writeErr(w, http.StatusForbidden, "订单未绑定账号，无法操作")
		return nil, false
	}
	// owned 表示当前会话是否拥有订单绑定的账号。
	if !s.cookieOwnedByUser(r.Context(), sess.UserID, order.CookieID) {
		writeErr(w, http.StatusForbidden, "无权操作此订单")
		return nil, false
	}
	return order, true
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
	// exists 表示通知渠道是否由当前用户拥有。
	var exists bool
	// err 表示通知渠道所有权查询失败的原因。
	err := s.Store.DB.QueryRowContext(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM notification_channels WHERE id=? AND user_id=?)`,
		channelID, sess.UserID).Scan(&exists)
	if err != nil || !exists {
		writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
		return false
	}
	return true
}
