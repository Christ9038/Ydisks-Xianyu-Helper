package adapter

import (
	"context"
	"errors"

	agentreplyapp "xianyu-go/internal/application/agentreply"
	"xianyu-go/internal/db"
)

// AgentReplyRepository 将 Agent 回复数据库仓储适配为应用层消费者定义的 Port。
type AgentReplyRepository struct {
	// store 保存数据库聚合入口；账号所有权只通过 Cookies 的非敏感方法判断。
	store *db.Store
}

// NewAgentReplyRepository 创建 Agent 回复配置数据库适配器。
func NewAgentReplyRepository(store *db.Store) *AgentReplyRepository {
	return &AgentReplyRepository{store: store}
}

// CheckOwnership 查询账号所有者，并把数据库缺失错误转换为应用错误；不读取任何凭证字段。
func (r *AgentReplyRepository) CheckOwnership(ctx context.Context, userID int64, cookieID string) (agentreplyapp.AccountOwnership, error) {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return agentreplyapp.AccountOwnership{}, err
	}
	// owned 表示账号是否直接属于当前用户。
	owned, err := r.store.Cookies.ExistsOwned(ctx, userID, cookieID)
	if err != nil {
		return agentreplyapp.AccountOwnership{}, err
	}
	if owned {
		return agentreplyapp.AccountOwnership{OwnerID: userID}, nil
	}
	// ownerID 保存账号的非敏感所有者标识，用于区分不存在和跨用户访问。
	ownerID, err := r.store.Cookies.GetOwnerID(ctx, cookieID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return agentreplyapp.AccountOwnership{}, agentreplyapp.ErrAccountNotFound
		}
		return agentreplyapp.AccountOwnership{}, err
	}
	return agentreplyapp.AccountOwnership{OwnerID: ownerID}, nil
}

// Get 读取不含令牌的配置摘要，并把 ErrNotFound 转换为应用错误。
func (r *AgentReplyRepository) Get(ctx context.Context, cookieID string) (agentreplyapp.Settings, error) {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return agentreplyapp.Settings{}, err
	}
	// row、err 保存数据库摘要及读取错误。
	row, err := r.store.AgentReply.Get(ctx, cookieID)
	if errors.Is(err, db.ErrNotFound) {
		return agentreplyapp.Settings{}, agentreplyapp.ErrConfigNotFound
	}
	if err != nil {
		return agentreplyapp.Settings{}, err
	}
	return agentreplyapp.Settings{
		CookieID: row.CookieID, Enabled: row.Enabled, EndpointURL: row.EndpointURL, TokenConfigured: row.TokenConfigured,
		RequestTimeoutSeconds: row.RequestTimeoutSeconds, MaxSessionSeconds: row.MaxSessionSeconds,
	}, nil
}

// Save 保存配置并按命令处理令牌；令牌明文只在本调用栈内传给仓储加密。
func (r *AgentReplyRepository) Save(ctx context.Context, cookieID string, draft agentreplyapp.Draft) error {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.AgentReply.Upsert(ctx, cookieID, db.AgentReplyWrite{
		Enabled: draft.Enabled, EndpointURL: draft.EndpointURL, RequestTimeoutSeconds: draft.RequestTimeoutSeconds,
		MaxSessionSeconds: draft.MaxSessionSeconds, TokenAction: draft.TokenAction, Token: draft.Token,
	})
}

// StoredToken 读取已保存令牌明文，仅供连接测试使用；没有配置时返回空串。
func (r *AgentReplyRepository) StoredToken(ctx context.Context, cookieID string) (string, error) {
	// err 表示适配器依赖未装配的错误，非空时直接返回。
	if err := r.validate(); err != nil {
		return "", err
	}
	// runtime、err 保存解密后的运行时配置及读取错误。
	runtime, err := r.store.AgentReply.RuntimeConfig(ctx, cookieID)
	if errors.Is(err, db.ErrNotFound) {
		return "", nil
	}
	return runtime.Token, err
}

// validate 确认适配器依赖已注入。
func (r *AgentReplyRepository) validate() error {
	if r == nil || r.store == nil || r.store.AgentReply == nil || r.store.Cookies == nil {
		return errors.New("外部 Agent 回复数据库依赖未初始化")
	}
	return nil
}

var _ agentreplyapp.Repository = (*AgentReplyRepository)(nil)
