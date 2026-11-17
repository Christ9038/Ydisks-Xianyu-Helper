package server

import (
	"context"

	apikeyapp "xianyu-go/internal/application/apikey"
	itemapp "xianyu-go/internal/application/items"
)

// ItemPlatformLifecyclePort 定义当前用户对闲鱼平台商品执行下架的最小用例能力。
type ItemPlatformLifecyclePort interface {
	// DownShelf 在平台下架商品并报告平台结果与本地会话写回状态，不删除商品。
	DownShelf(context.Context, int64, string, string) (itemapp.DownShelfOutcome, error)
}

// APIAccessTokensPort 定义当前用户管理外部 API 访问令牌的最小用例能力。
type APIAccessTokensPort interface {
	// Create 签发令牌并在创建结果中返回一次明文；后续读取只返回非敏感摘要。
	Create(context.Context, int64, apikeyapp.CreateInput) (apikeyapp.Created, error)
	// List 返回当前用户拥有的令牌摘要，不包含可用于认证的秘密。
	List(context.Context, int64) ([]apikeyapp.Summary, error)
	// Revoke 撤销当前用户拥有的指定令牌。
	Revoke(context.Context, int64, string) error
}

// apiAccessTokensApplication 返回当前用户管理外部 API 访问令牌的用例 Port。
func (server *Server) apiAccessTokensApplication() APIAccessTokensPort {
	return server.applicationServiceSet().apiAccessTokens
}

// itemPlatformLifecycleApplication 返回闲鱼平台商品下架用例。
func (server *Server) itemPlatformLifecycleApplication() ItemPlatformLifecyclePort {
	return server.applicationServiceSet().itemPlatformLifecycle
}
