package server

import (
	"context"
	"errors"

	accountapp "xianyu-go/internal/application/account"
)

// loadCookiePlatformDetail 读取平台请求所需的最小 Cookie 状态，并转换为 Server 内部已有的会话适配模型。
func (s *Server) loadCookiePlatformDetail(ctx context.Context, cookieID string) (*accountapp.CredentialDetail, error) {
	if s == nil {
		return nil, errors.New("平台凭证读取服务未初始化")
	}
	// repository 提供不读取登录密码的平台凭证窄视图；SQL 和解密逻辑由 adapter 负责。
	repository := s.accountLoginRepositoryForServer()
	if repository == nil {
		return nil, errors.New("平台凭证 repository 未初始化")
	}
	// platformData 是应用 Port 返回的不含登录密码的平台运行视图。
	platformData, err := repository.LoadPlatformDetail(ctx, cookieID)
	if err != nil {
		return nil, err
	}
	return platformData, nil
}

// loadCookieSummaryDetail 读取账号非敏感摘要并转换为 Server 内部的兼容模型。
func (s *Server) loadCookieSummaryDetail(ctx context.Context, userID int64, cookieID string) (accountapp.AccountSummary, error) {
	if s == nil {
		return accountapp.AccountSummary{}, errors.New("账号摘要读取服务未初始化")
	}
	// service 提供按用户和账号联合过滤的非敏感摘要；Server 不直接访问账号表。
	service := s.accountSummaryApplication()
	if service == nil {
		return accountapp.AccountSummary{}, errors.New("账号摘要服务未初始化")
	}
	// summary、queryErr 保存应用服务返回的非敏感摘要及查询错误。
	summary, queryErr := service.GetOwnedSummary(ctx, userID, cookieID)
	if queryErr != nil {
		return accountapp.AccountSummary{}, queryErr
	}
	return summary, nil
}
