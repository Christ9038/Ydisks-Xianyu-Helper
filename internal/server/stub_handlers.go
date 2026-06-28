package server

import "github.com/go-chi/chi/v5"

// stub_handlers.go 原为各分组的 501 占位；现已全部由 Real 实现替换。
// 保留本文件作为 mount* 方法名到 Real 实现的别名，保持 server.go 调用简洁。

func (s *Server) mountItems(r chi.Router)          { s.mountItemsReal(r) }
func (s *Server) mountKeywords(r chi.Router)       { s.mountKeywordsReal(r) }
func (s *Server) mountDefaultReplies(r chi.Router) { s.mountDefaultRepliesReal(r) }
func (s *Server) mountNotifications(r chi.Router)  { s.mountNotificationsReal(r) }
func (s *Server) mountSettings(r chi.Router)       { s.mountSettingsReal(r) }
func (s *Server) mountAIReply(r chi.Router)        { s.mountAIReplyReal(r) }
func (s *Server) mountUser(r chi.Router)           { s.mountUserReal(r) }
func (s *Server) mountAdmin(r chi.Router)          { s.mountAdminReal(r) }

