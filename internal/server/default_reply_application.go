package server

import defaultreplyapp "xianyu-go/internal/application/defaultreply"

// defaultReplyApplication 返回默认回复应用服务。
// 该方法在主装配集合接入前提供同一 Port 的兼容构造入口，后续由统一装配复用实例。
func (s *Server) defaultReplyApplication() *defaultreplyapp.Service {
	return s.applicationServiceSet().defaultReplies
}
