package server

// accountSummaryApplication 返回账号摘要用例。
func (server *Server) accountSummaryApplication() AccountSummaryPort {
	return server.applicationServiceSet().accountSummaries
}

// accountDeleteApplication 返回账号删除用例。
func (server *Server) accountDeleteApplication() AccountDeletePort {
	return server.applicationServiceSet().accountDelete
}

// loginAuditApplication 返回登录审计用例。
func (server *Server) loginAuditApplication() LoginAuditPort {
	return server.applicationServiceSet().loginAudit
}
