package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedAccounts 挂载账号摘要、详情和运行状态的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedAccounts(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Get("/api/v1/accounts", s.listCookies)
		r.Get("/api/v1/accounts/details", s.listCookieDetails)
		r.Get("/api/v1/accounts/runtime-status", s.listCookieRuntimeStatus)
		r.Get("/api/v1/accounts/{cid}", s.getCookieDetails)
		r.Put("/api/v1/accounts/{cid}/status", s.setCookieStatus)
		r.Put("/api/v1/accounts/{cid}/settings", s.updateCookieSettings)
		r.Get("/api/v1/accounts/{cid}/long-login", s.getLongLoginSettings)
		r.Put("/api/v1/accounts/{cid}/long-login", s.setLongLoginSettings)
		r.Post("/api/v1/accounts/{cid}/refresh-profile", s.refreshCookieProfile)
		r.Get("/api/v1/accounts/{cid}/auto-confirm", s.getCookieAutoConfirm)
		r.Put("/api/v1/accounts/{cid}/auto-confirm", s.setCookieAutoConfirm)
		r.Put("/api/v1/accounts/{cid}/remark", s.setCookieRemark)
		r.Get("/api/v1/accounts/{cid}/pause-duration", s.getCookiePauseDuration)
		r.Put("/api/v1/accounts/{cid}/pause-duration", s.setCookiePauseDuration)
	})
}
