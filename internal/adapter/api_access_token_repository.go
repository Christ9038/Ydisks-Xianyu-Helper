package adapter

import (
	"context"

	apikeyapp "xianyu-go/internal/application/apikey"
	"xianyu-go/internal/db"
)

// APIAccessTokenRepository 将数据库令牌仓储投影为访问令牌应用服务所需的最小 Repository。
type APIAccessTokenRepository struct {
	// repository 是组合根创建的数据库访问令牌仓储。
	repository *db.APIAccessTokens
}

// NewAPIAccessTokenRepository 创建访问令牌仓储适配器；数据库仓储为空时返回 nil。
func NewAPIAccessTokenRepository(repository *db.APIAccessTokens) *APIAccessTokenRepository {
	if repository == nil {
		return nil
	}
	return &APIAccessTokenRepository{repository: repository}
}

// Create 将应用层令牌记录转换为数据库记录并持久化摘要。
func (adapter *APIAccessTokenRepository) Create(ctx context.Context, record apikeyapp.Record) error {
	return adapter.repository.Create(ctx, db.APIAccessToken{
		ID: record.ID, UserID: record.UserID, Name: record.Name, TokenHash: record.TokenHash,
		Prefix: record.Prefix, Scopes: append([]string(nil), record.Scopes...),
		CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt,
	})
}

// List 将指定用户的数据库令牌记录投影为应用层记录。
func (adapter *APIAccessTokenRepository) List(ctx context.Context, userID int64) ([]apikeyapp.Record, error) {
	// records 保存数据库返回的非敏感令牌元数据。
	records, err := adapter.repository.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	// result 是应用层可消费的令牌记录副本。
	result := make([]apikeyapp.Record, 0, len(records))
	// record 是当前用户的一条持久化令牌摘要，不含可用于认证的哈希字段。
	for _, record := range records {
		result = append(result, apikeyapp.Record{
			ID: record.ID, UserID: record.UserID, Name: record.Name, Prefix: record.Prefix,
			Scopes: append([]string(nil), record.Scopes...), CreatedAt: record.CreatedAt,
			ExpiresAt: record.ExpiresAt, LastUsedAt: record.LastUsedAt, RevokedAt: record.RevokedAt,
		})
	}
	return result, nil
}

// Revoke 将应用层撤销请求转发给数据库仓储。
func (adapter *APIAccessTokenRepository) Revoke(ctx context.Context, userID int64, keyID string, revokedAt int64) error {
	return adapter.repository.Revoke(ctx, userID, keyID, revokedAt)
}
