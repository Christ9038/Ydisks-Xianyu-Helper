package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// APIAccessToken 是访问令牌的持久化记录；不包含可逆的明文令牌。
type APIAccessToken struct {
	// ID 是本地访问令牌记录标识。
	ID string
	// UserID 是签发令牌的本地用户标识。
	UserID int64
	// Name 是令牌用途名称。
	Name string
	// TokenHash 是原始令牌的 SHA-256 十六进制摘要。
	TokenHash string
	// Prefix 是用于识别令牌的非秘密前缀。
	Prefix string
	// Scopes 是令牌允许调用的权限集合。
	Scopes []string
	// CreatedAt 是签发时间的 Unix 秒数。
	CreatedAt int64
	// ExpiresAt 是失效时间的 Unix 秒数。
	ExpiresAt int64
	// LastUsedAt 是最近一次认证成功时间；0 表示尚未使用。
	LastUsedAt int64
	// RevokedAt 是撤销时间；0 表示尚未撤销。
	RevokedAt int64
}

// APIAccessPrincipal 是令牌认证成功后供 HTTP 中间件使用的非秘密身份。
type APIAccessPrincipal struct {
	// ID 是通过认证的令牌记录标识。
	ID string
	// UserID 是令牌所属的本地用户标识。
	UserID int64
	// Username 是当前有效用户的登录名称。
	Username string
	// Prefix 是令牌的非秘密识别前缀。
	Prefix string
	// Scopes 是该令牌被授予的权限集合。
	Scopes []string
}

// APIAccessTokens 负责访问令牌摘要的数据库读写与认证查询。
type APIAccessTokens struct {
	// DB 是底层数据库连接池。
	DB *sql.DB
}

// Create 保存新的访问令牌摘要与权限元数据；明文令牌不会进入仓储。
func (repository *APIAccessTokens) Create(ctx context.Context, record APIAccessToken) error {
	// scopesJSON 是已验证权限集合的持久化 JSON 表示。
	scopesJSON, err := json.Marshal(record.Scopes)
	if err != nil {
		return fmt.Errorf("编码 API 访问权限失败")
	}
	_, err = repository.DB.ExecContext(ctx,
		`INSERT INTO api_access_tokens (id, user_id, name, token_hash, token_prefix, scopes_json, created_at, expires_at, last_used_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		record.ID, record.UserID, record.Name, record.TokenHash, record.Prefix, string(scopesJSON), record.CreatedAt, record.ExpiresAt)
	return err
}

// List 仅列出指定用户拥有的令牌摘要和权限，不查询 token_hash。
func (repository *APIAccessTokens) List(ctx context.Context, userID int64) ([]APIAccessToken, error) {
	// rows、err 保存当前用户令牌摘要查询结果及数据库错误。
	rows, err := repository.DB.QueryContext(ctx,
		`SELECT id, user_id, name, token_prefix, scopes_json, created_at, expires_at,
		        COALESCE(last_used_at, 0), COALESCE(revoked_at, 0)
		   FROM api_access_tokens WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// records 保存当前用户可见的令牌管理元数据。
	records := make([]APIAccessToken, 0)
	for rows.Next() {
		// record 是当前扫描的令牌摘要行；扫描字段不含 token_hash。
		var record APIAccessToken
		// scopesJSON 是数据库保存的权限 JSON 文本。
		var scopesJSON string
		// err 是将当前令牌摘要行映射到持久化模型时的扫描错误。
		if err := rows.Scan(&record.ID, &record.UserID, &record.Name, &record.Prefix, &scopesJSON,
			&record.CreatedAt, &record.ExpiresAt, &record.LastUsedAt, &record.RevokedAt); err != nil {
			return nil, err
		}
		// err 是把持久化权限 JSON 解析为权限集合时的格式错误。
		if err := json.Unmarshal([]byte(scopesJSON), &record.Scopes); err != nil {
			return nil, fmt.Errorf("解析 API 访问权限失败")
		}
		records = append(records, record)
	}
	// err 是遍历用户令牌结果集时由驱动报告的延迟读取错误。
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// Revoke 只撤销指定用户拥有且尚未撤销的令牌，重复撤销不会影响其他记录。
func (repository *APIAccessTokens) Revoke(ctx context.Context, userID int64, keyID string, revokedAt int64) error {
	// result、err 保存撤销语句的执行结果和数据库错误。
	result, err := repository.DB.ExecContext(ctx,
		`UPDATE api_access_tokens SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		revokedAt, keyID, userID)
	if err != nil {
		return err
	}
	// affected 是本次撤销实际修改的记录数。
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Authenticate 按令牌摘要查询有效身份，并同时检查用户状态、撤销标记和过期时间。
func (repository *APIAccessTokens) Authenticate(ctx context.Context, tokenHash string, now int64) (APIAccessPrincipal, error) {
	// principal 保存认证成功后允许进入请求上下文的非敏感身份字段。
	var principal APIAccessPrincipal
	// scopesJSON 是数据库中的权限集合；不会返回或查询原始令牌。
	var scopesJSON string
	// err 是按摘要、有效期、撤销状态和用户状态读取令牌身份时的查询错误。
	err := repository.DB.QueryRowContext(ctx,
		`SELECT k.id, k.user_id, u.username, k.token_prefix, k.scopes_json
		   FROM api_access_tokens k
		   JOIN users u ON u.id = k.user_id
		  WHERE k.token_hash = ? AND k.revoked_at IS NULL AND k.expires_at > ? AND u.is_active = 1`,
		tokenHash, now).Scan(&principal.ID, &principal.UserID, &principal.Username, &principal.Prefix, &scopesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return APIAccessPrincipal{}, ErrNotFound
	}
	if err != nil {
		return APIAccessPrincipal{}, err
	}
	// err 是把认证查询返回的权限 JSON 解析为身份权限集合时的格式错误。
	if err := json.Unmarshal([]byte(scopesJSON), &principal.Scopes); err != nil {
		return APIAccessPrincipal{}, fmt.Errorf("解析 API 访问权限失败")
	}
	return principal, nil
}

// TouchLastUsed 更新最近使用时间，每分钟最多写入一次以降低高频 API 调用的数据库写放大。
func (repository *APIAccessTokens) TouchLastUsed(ctx context.Context, keyID string, now int64) error {
	// err 是更新令牌最近使用时间时的数据库错误。
	_, err := repository.DB.ExecContext(ctx,
		`UPDATE api_access_tokens SET last_used_at = ?
		  WHERE id = ? AND revoked_at IS NULL AND (last_used_at IS NULL OR last_used_at <= ?)`,
		now, keyID, now-60)
	return err
}
