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

func (s *Server) requireOrderOwner(w http.ResponseWriter, r *http.Request, orderID string) (*db.Order, bool) {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return nil, false
	}
	order, err := s.Store.Orders.Get(r.Context(), orderID)
	if err != nil || order == nil {
		writeErr(w, http.StatusNotFound, "订单不存在")
		return nil, false
	}
	if order.CookieID == "" {
		writeErr(w, http.StatusForbidden, "订单未绑定账号，无法操作")
		return nil, false
	}
	if _, ok := s.cookieForUser(r, sess.UserID, order.CookieID); !ok {
		writeErr(w, http.StatusForbidden, "无权操作此订单")
		return nil, false
	}
	return order, true
}

func (s *Server) requireCardOwner(w http.ResponseWriter, r *http.Request, cardID int64) (*db.CardFull, bool) {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return nil, false
	}
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

func (s *Server) requireChannelOwner(w http.ResponseWriter, r *http.Request, channelID int64) bool {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return false
	}
	var exists int
	err := s.Store.DB.QueryRowContext(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM notification_channels WHERE id=? AND user_id=?)`,
		channelID, sess.UserID).Scan(&exists)
	if err != nil || exists == 0 {
		writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
		return false
	}
	return true
}

func (s *Server) cookieForUser(r *http.Request, userID int64, cookieID string) (string, bool) {
	all, err := s.Store.Cookies.AllForUser(r.Context(), userID)
	if err != nil {
		return "", false
	}
	value, ok := all[cookieID]
	return value, ok
}
