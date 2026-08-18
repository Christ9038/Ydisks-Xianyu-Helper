package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	"xianyu-go/internal/auth"
)

// btoi bool→int（SQLite 无原生 bool）。
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullIfEmpty 空串存为 NULL。
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// mountDefaultRepliesReal 默认回复端点。
func (s *Server) mountDefaultRepliesReal(r chi.Router) {
	r.Get("/default-replies/{cid}", s.getDefaultReply)
	r.Put("/default-replies/{cid}", s.setDefaultReply)
	r.Get("/default-replies", s.listDefaultReplies)
	r.Delete("/default-replies/{cid}", s.deleteDefaultReply)
	r.Get("/api/default-replies", s.listDefaultRepliesMap)
	r.Get("/api/default-reply/{cid}", s.getDefaultReply)
	r.Put("/api/default-reply/{cid}", s.setDefaultReply)
	r.Delete("/api/default-reply/{cid}", s.deleteDefaultReply)
	r.Post("/api/default-reply/{cid}/clear-records", s.clearDefaultReplyRecords)
}

// getDefaultReply 封装getDefault回复业务协调。
func (s *Server) getDefaultReply(w http.ResponseWriter, r *http.Request) {
	// cid 是路径中的默认回复所属账号标识。
	cid := chi.URLParam(r, "cid")
	// userID 表示认证会话中的当前用户，缺失会话时请求不具备业务身份。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// reply、err 保存应用服务返回的默认回复模型及读取错误。
	reply, err := s.defaultReplyApplication().Get(r.Context(), userID, cid)
	switch {
	case errors.Is(err, defaultreplyapp.ErrConfigNotFound):
		writeJSON(w, http.StatusOK, defaultReplyResponse{Enabled: false, ReplyContent: "", ReplyOnce: false})
		return
	case errors.Is(err, defaultreplyapp.ErrAccountNotFound):
		writeErr(w, http.StatusNotFound, "账号不存在")
		return
	case errors.Is(err, defaultreplyapp.ErrForbidden):
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// 已保存默认回复通过具名 DTO 返回，单账号查询不填充 cookie_id。
	writeJSON(w, http.StatusOK, newDefaultReplyResponse("", reply))
}

// setDefaultReply 保存指定账号的默认回复配置。
func (s *Server) setDefaultReply(w http.ResponseWriter, r *http.Request) {
	// cid 是当前操作的账号标识。
	cid := chi.URLParam(r, "cid")
	// userID 表示认证会话中的当前用户，缺失会话时请求不具备业务身份。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// req 是默认回复配置请求体。
	var req defaultReplyMutationRequest
	// decodeErr 表示请求体不是有效的默认回复 JSON。
	if decodeErr := decodeJSON(r, &req); decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// err 表示默认回复应用服务写入失败的原因。
	err := s.defaultReplyApplication().Upsert(r.Context(), userID, cid, defaultreplyapp.Reply{
		Enabled: req.Enabled, ReplyContent: req.ReplyContent,
		ReplyImageURL: req.ReplyImageURL, ReplyOnce: req.ReplyOnce,
	})
	if err != nil {
		writeDefaultReplyMutationError(w, err, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// defaultReplyMutationRequest 是默认回复配置写入接口的具名请求 DTO。
type defaultReplyMutationRequest struct {
	// Enabled 表示是否启用默认回复。
	Enabled bool `json:"enabled"`
	// ReplyContent 是默认回复文字内容。
	ReplyContent string `json:"reply_content"`
	// ReplyImageURL 是默认回复图片地址。
	ReplyImageURL string `json:"reply_image_url"`
	// ReplyOnce 表示同一聊天是否只发送一次默认回复。
	ReplyOnce bool `json:"reply_once"`
}

// listDefaultReplies 查询当前用户的默认回复列表。
func (s *Server) listDefaultReplies(w http.ResponseWriter, r *http.Request) {
	// sess 是当前登录用户会话。
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// rows 和 err 是当前用户的应用层默认回复列表及查询错误。
	rows, err := s.defaultReplyApplication().List(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 是默认回复列表响应。
	var out []defaultReplyResponse
	// row 表示当前遍历过程中的应用层默认回复摘要。
	for _, row := range rows {
		out = append(out, defaultReplyResponse{
			CookieID: row.CookieID, Enabled: row.Reply.Enabled, ReplyContent: row.Reply.ReplyContent,
			ReplyOnce: row.Reply.ReplyOnce, ReplyImageURL: row.Reply.ReplyImageURL,
			// 列表 DTO 保留账号标识，前端可直接建立按账号索引。
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// listDefaultRepliesMap 查询按账号索引的默认回复映射。
func (s *Server) listDefaultRepliesMap(w http.ResponseWriter, r *http.Request) {
	// sess 是当前登录用户会话。
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// rows 和 err 是当前用户的应用层默认回复列表及查询错误。
	rows, err := s.defaultReplyApplication().List(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 是按账号标识索引的默认回复映射。
	out := make(map[string]defaultReplyResponse)
	// row 表示当前遍历过程中的应用层默认回复摘要。
	for _, row := range rows {
		out[row.CookieID] = defaultReplyResponse{
			CookieID:      row.CookieID,
			Enabled:       row.Reply.Enabled,
			ReplyContent:  row.Reply.ReplyContent,
			ReplyOnce:     row.Reply.ReplyOnce,
			ReplyImageURL: row.Reply.ReplyImageURL,
			// map 键与 cookie_id 同时保留，兼容旧前端索引方式。
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// deleteDefaultReply 删除指定账号的默认回复配置。
func (s *Server) deleteDefaultReply(w http.ResponseWriter, r *http.Request) {
	// cid 是当前操作的账号标识。
	cid := chi.URLParam(r, "cid")
	// userID 表示认证会话中的当前用户，缺失会话时请求不具备业务身份。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// err 表示默认回复应用服务删除失败的原因。
	err := s.defaultReplyApplication().Delete(r.Context(), userID, cid)
	if err != nil {
		writeDefaultReplyMutationError(w, err, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// clearDefaultReplyRecords 清空指定账号的默认回复发送记录。
func (s *Server) clearDefaultReplyRecords(w http.ResponseWriter, r *http.Request) {
	// cid 是当前操作的账号标识。
	cid := chi.URLParam(r, "cid")
	// userID 表示认证会话中的当前用户，缺失会话时请求不具备业务身份。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// err 表示默认回复应用服务清理投递记录失败的原因。
	if err := s.defaultReplyApplication().ClearRecords(r.Context(), userID, cid); err != nil {
		writeDefaultReplyMutationError(w, err, "清空失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// defaultReplyUserID 从认证上下文读取当前用户，并统一处理缺失会话。
func defaultReplyUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	// session 是认证中间件写入请求上下文的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return 0, false
	}
	return session.UserID, true
}

// writeDefaultReplyMutationError 将默认回复应用错误映射为既有 HTTP 错误语义。
func writeDefaultReplyMutationError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, defaultreplyapp.ErrAccountNotFound):
		writeErr(w, http.StatusNotFound, "账号不存在")
	case errors.Is(err, defaultreplyapp.ErrForbidden):
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
	default:
		writeErr(w, http.StatusInternalServerError, fallback)
	}
}
