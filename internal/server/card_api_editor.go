package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	cardsapp "xianyu-go/internal/application/cards"
	"xianyu-go/internal/auth"
)

// cardAPIEditorResponse 是用户主动编辑时的专用 DTO，不包含数据库模型且禁止缓存。
type cardAPIEditorResponse struct {
	// URL 是请求目标。
	URL string `json:"url"`
	// Method 是请求方法。
	Method string `json:"method"`
	// TimeoutSeconds 是超时秒数。
	TimeoutSeconds int `json:"timeout_seconds"`
	// ContentType 是正文编码。
	ContentType string `json:"content_type"`
	// Headers 是包含密钥的请求头 JSON，只交给当前所属用户的编辑器。
	Headers string `json:"headers"`
	// Params 是当前所属用户可编辑的查询模板 JSON。
	Params string `json:"params"`
	// Body 是当前所属用户可编辑的正文模板 JSON。
	Body string `json:"body"`
	// ResponsePath 是结果提取路径。
	ResponsePath string `json:"response_path"`
	// RetryEnabled 表示幂等重试开关。
	RetryEnabled bool `json:"retry_enabled"`
}

// getCardAPIEditor 读取当前会话拥有的卡密配置，不扩大普通查询的敏感字段范围。
func (s *Server) getCardAPIEditor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// cardID、err 是 URL 中的卡密标识及解析错误。
	cardID, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "卡券ID无效")
		return
	}
	// session 是已认证的当前调用者，不接受请求体指定所有者。
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "请先登录")
		return
	}
	// editor、err 保存经所有权验证的专用编辑模型。
	editor, err := s.cardsApplication().GetAPIEditor(r.Context(), session.UserID, cardID)
	if err != nil {
		writeCardAPIError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cardAPIEditorResponse{URL: editor.URL, Method: editor.Method, TimeoutSeconds: editor.TimeoutSeconds,
		ContentType: editor.ContentType, Headers: editor.Headers, Params: editor.Params, Body: editor.Body,
		ResponsePath: editor.ResponsePath, RetryEnabled: editor.RetryEnabled})
}

// writeCardAPIError 将配置校验与资源归属错误映射为统一错误响应，基础设施错误不泄漏内部文本。
func writeCardAPIError(w http.ResponseWriter, r *http.Request, err error) {
	// validation 是允许向用户展示的无秘密校验提示。
	var validation *cardsapp.ValidationError
	if errors.As(err, &validation) {
		writeErr(w, http.StatusBadRequest, validation.Message)
		return
	}
	writeCardReadError(w, r, err)
}
