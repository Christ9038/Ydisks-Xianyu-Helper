package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/application/apikey"
	"xianyu-go/internal/auth"
)

// apiAccessTokenCreateRequest 是签发外部 API 访问令牌的具名请求 DTO。
type apiAccessTokenCreateRequest struct {
	// Name 是令牌用途名称，便于用户在撤销列表中识别。
	Name string `json:"name"`
	// Scopes 是本次令牌需要授予的权限集合。
	Scopes []string `json:"scopes"`
	// ExpiresInDays 是令牌有效期天数；0 使用应用层默认的 90 天。
	ExpiresInDays int `json:"expires_in_days"`
}

// apiAccessTokenListResponse 是令牌列表响应；列表只包含不可用于认证的元数据。
type apiAccessTokenListResponse struct {
	// Success 表示请求成功。
	Success bool `json:"success"`
	// Data 是当前登录用户拥有的令牌摘要。
	Data []apikey.Summary `json:"data"`
}

// apiAccessTokenCreatedResponse 是令牌创建响应；Token 只在创建成功时返回一次。
type apiAccessTokenCreatedResponse struct {
	// Success 表示令牌已经成功保存。
	Success bool `json:"success"`
	// Token 是新生成的明文令牌；客户端必须立即保存，服务端不会再次展示。
	Token string `json:"token"`
	// Data 是不含明文和摘要的令牌管理信息。
	Data apikey.Summary `json:"data"`
}

// apiAccessTokenOperationResponse 是撤销令牌成功后的稳定响应。
type apiAccessTokenOperationResponse struct {
	// Success 表示撤销操作成功。
	Success bool `json:"success"`
}

// listAPIAccessTokens 列出当前浏览器登录用户签发的令牌，不返回任何可用于认证的秘密。
func (s *Server) listAPIAccessTokens(w http.ResponseWriter, r *http.Request) {
	// session 保存已由真实 Cookie 会话认证的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	if session == nil || auth.APIKeyFromContext(r.Context()) != nil {
		writeErr(w, http.StatusUnauthorized, "需要浏览器登录会话")
		return
	}
	// summaries、err 保存当前用户的令牌摘要列表及查询错误。
	summaries, err := s.apiAccessTokensApplication().List(r.Context(), session.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取 API 访问令牌失败")
		return
	}
	setNoStore(w)
	writeJSON(w, http.StatusOK, apiAccessTokenListResponse{Success: true, Data: summaries})
}

// createAPIAccessToken 为当前浏览器登录用户签发令牌，并仅在本次响应中展示明文。
func (s *Server) createAPIAccessToken(w http.ResponseWriter, r *http.Request) {
	// session 保存已由真实 Cookie 会话认证的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	if session == nil || auth.APIKeyFromContext(r.Context()) != nil {
		writeErr(w, http.StatusUnauthorized, "需要浏览器登录会话")
		return
	}
	// request 保存用户提交的用途名称、权限范围和有效期。
	var request apiAccessTokenCreateRequest
	// err 是解析用户提交的令牌名称、权限和有效期请求时的格式错误。
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, "API 访问令牌请求格式无效")
		return
	}
	// created、err 保存一次性令牌签发结果及业务错误。
	created, err := s.apiAccessTokensApplication().Create(r.Context(), session.UserID, apikey.CreateInput{
		Name: request.Name, Scopes: request.Scopes, ExpiresInDays: request.ExpiresInDays,
	})
	if err != nil {
		writeAPIAccessTokenError(w, err)
		return
	}
	setNoStore(w)
	writeJSON(w, http.StatusCreated, apiAccessTokenCreatedResponse{Success: true, Token: created.Token, Data: created.Summary})
}

// revokeAPIAccessToken 撤销当前浏览器登录用户拥有的指定令牌。
func (s *Server) revokeAPIAccessToken(w http.ResponseWriter, r *http.Request) {
	// session 保存已由真实 Cookie 会话认证的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	if session == nil || auth.APIKeyFromContext(r.Context()) != nil {
		writeErr(w, http.StatusUnauthorized, "需要浏览器登录会话")
		return
	}
	// keyID 是路由参数中的访问令牌记录标识。
	keyID := strings.TrimSpace(chi.URLParam(r, "token_id"))
	// err 是按当前用户归属撤销指定令牌时返回的应用错误。
	if err := s.apiAccessTokensApplication().Revoke(r.Context(), session.UserID, keyID); err != nil {
		writeAPIAccessTokenError(w, err)
		return
	}
	setNoStore(w)
	writeJSON(w, http.StatusOK, apiAccessTokenOperationResponse{Success: true})
}

// writeAPIAccessTokenError 将令牌应用错误映射为统一 HTTP 状态，不回显令牌明文或摘要。
func writeAPIAccessTokenError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apikey.ErrInvalidInput):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, apikey.ErrNotFound):
		writeErr(w, http.StatusNotFound, "API 访问令牌不存在")
	case errors.Is(err, apikey.ErrInvalidUser):
		writeErr(w, http.StatusUnauthorized, "需要浏览器登录会话")
	default:
		writeErr(w, http.StatusInternalServerError, "API 访问令牌操作失败")
	}
}
