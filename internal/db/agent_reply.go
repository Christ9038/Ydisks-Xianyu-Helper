package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// agentTokenScope 是 Agent 访问令牌加密时使用的用途域，owner 固定为账号标识，防止密文被挪用到其他字段或账号。
const agentTokenScope = "agent-reply-token"

// 以下常量是 Agent 访问令牌写入命令，与系统敏感设置的 retain/replace/clear 三态保持一致。
const (
	// AgentTokenRetain 表示保留已保存的令牌，不读取也不覆盖。
	AgentTokenRetain = "retain"
	// AgentTokenReplace 表示用 AgentReplyWrite.Token 替换令牌。
	AgentTokenReplace = "replace"
	// AgentTokenClear 表示清空已保存的令牌。
	AgentTokenClear = "clear"
)

// AgentReplySettings 是账号 Agent 回复配置的非敏感摘要，不包含访问令牌。
type AgentReplySettings struct {
	// CookieID 是配置所属账号。
	CookieID string
	// Enabled 表示该账号是否启用 Agent 回复。
	Enabled bool
	// EndpointURL 是外部 Agent 的 http(s) 地址。
	EndpointURL string
	// TokenConfigured 表示已保存访问令牌；令牌本身不会离开仓储的运行时读取方法。
	TokenConfigured bool
	// RequestTimeoutSeconds 是单次 Agent 请求的超时秒数。
	RequestTimeoutSeconds int
	// MaxSessionSeconds 是单轮对话（含轮询）的总时长上限秒数。
	MaxSessionSeconds int
}

// AgentReplyWrite 是保存账号 Agent 回复配置的一次写入命令。
type AgentReplyWrite struct {
	// Enabled、EndpointURL、RequestTimeoutSeconds 和 MaxSessionSeconds 是调用方已校验的普通配置。
	Enabled               bool
	EndpointURL           string
	RequestTimeoutSeconds int
	MaxSessionSeconds     int
	// TokenAction 是令牌写入命令，取 AgentToken* 常量；空值按 retain 处理。
	TokenAction string
	// Token 是 TokenAction 为 replace 时写入的明文令牌，仓储加密后落库，不得记录日志。
	Token string
}

// AgentReplyRuntime 是仅供引擎和测试连接使用的运行时配置，携带已解密令牌，不得序列化到 HTTP 响应或日志。
type AgentReplyRuntime struct {
	// Enabled 表示是否启用 Agent 回复。
	Enabled bool
	// EndpointURL 是外部 Agent 地址。
	EndpointURL string
	// Token 是已解密的访问令牌，可为空。
	Token string
	// RequestTimeout 是单次请求超时。
	RequestTimeout time.Duration
	// MaxSession 是单轮对话总时长上限。
	MaxSession time.Duration
}

// AgentReply 读写 agent_reply_settings；令牌字段使用 secretCodec 加密落库。
type AgentReply struct {
	// DB 是底层数据库连接。
	DB *sql.DB
	// Dialect 决定 upsert 语法。
	Dialect Dialect
	// codec 加解密访问令牌；未配置数据密钥时按项目约定明文保存。
	codec *secretCodec
}

// Get 读取账号的 Agent 回复配置摘要，不读取令牌内容；没有配置时返回 ErrNotFound。
func (a *AgentReply) Get(ctx context.Context, cookieID string) (AgentReplySettings, error) {
	// settings 保存扫描出的配置摘要，不含令牌。
	settings := AgentReplySettings{CookieID: cookieID}
	// enabled、tokenConfigured 是数据库整数布尔值，非零表示真。
	var enabled, tokenConfigured int
	// err 保存查询与扫描错误；令牌只判断是否为空，不会被读入内存。
	err := a.DB.QueryRowContext(ctx,
		`SELECT enabled, endpoint_url, CASE WHEN auth_token <> '' THEN 1 ELSE 0 END,
		        request_timeout_seconds, max_session_seconds
		   FROM agent_reply_settings WHERE cookie_id = ?`, cookieID).
		Scan(&enabled, &settings.EndpointURL, &tokenConfigured, &settings.RequestTimeoutSeconds, &settings.MaxSessionSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentReplySettings{}, ErrNotFound
	}
	if err != nil {
		return AgentReplySettings{}, err
	}
	settings.Enabled, settings.TokenConfigured = enabled != 0, tokenConfigured != 0
	return settings, nil
}

// Upsert 保存账号 Agent 回复配置；令牌按 TokenAction 保留、替换或清空，retain 时不触碰已有密文。
func (a *AgentReply) Upsert(ctx context.Context, cookieID string, write AgentReplyWrite) error {
	// token 保存要落库的令牌密文；只有 replace 和 clear 会写入该列。
	token := ""
	// updates 是冲突时更新的列；令牌列仅在需要修改时加入。
	updates := map[string]string{
		"enabled":                 "EXCLUDED.enabled",
		"endpoint_url":            "EXCLUDED.endpoint_url",
		"request_timeout_seconds": "EXCLUDED.request_timeout_seconds",
		"max_session_seconds":     "EXCLUDED.max_session_seconds",
		"updated_at":              "CURRENT_TIMESTAMP",
	}
	switch strings.TrimSpace(write.TokenAction) {
	case "", AgentTokenRetain:
	case AgentTokenReplace:
		// encrypted、err 保存令牌密文及加密错误。
		encrypted, err := a.codec.encrypt(agentTokenScope, cookieID, strings.TrimSpace(write.Token))
		if err != nil {
			return fmt.Errorf("加密 Agent 令牌: %w", err)
		}
		token = encrypted
		updates["auth_token"] = "EXCLUDED.auth_token"
	case AgentTokenClear:
		updates["auth_token"] = "EXCLUDED.auth_token"
	default:
		return fmt.Errorf("未知的 Agent 令牌命令")
	}
	// err 保存 upsert 执行错误；新建行在 retain 时令牌列取空串。
	_, err := a.DB.ExecContext(ctx,
		`INSERT INTO agent_reply_settings
		 (cookie_id, enabled, endpoint_url, auth_token, request_timeout_seconds, max_session_seconds, updated_at)
		 VALUES (?,?,?,?,?,?,CURRENT_TIMESTAMP)`+dialectUpsert(a.Dialect, []string{"cookie_id"}, updates),
		cookieID, boolToInt(write.Enabled), strings.TrimSpace(write.EndpointURL), token,
		write.RequestTimeoutSeconds, write.MaxSessionSeconds)
	return err
}

// RuntimeConfig 读取并解密账号的运行时配置，仅供引擎和测试连接使用；没有配置时返回 ErrNotFound。
func (a *AgentReply) RuntimeConfig(ctx context.Context, cookieID string) (AgentReplyRuntime, error) {
	// enabled、timeout、session 是数据库原始整数值：开关布尔与两个超时秒数。
	var enabled, timeout, session int
	// endpoint 是配置的 Agent 地址；rawToken 是库中可能已加密的令牌，解密前不得使用。
	var endpoint, rawToken string
	// err 保存查询与扫描错误。
	err := a.DB.QueryRowContext(ctx,
		`SELECT enabled, endpoint_url, auth_token, request_timeout_seconds, max_session_seconds
		   FROM agent_reply_settings WHERE cookie_id = ?`, cookieID).
		Scan(&enabled, &endpoint, &rawToken, &timeout, &session)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentReplyRuntime{}, ErrNotFound
	}
	if err != nil {
		return AgentReplyRuntime{}, err
	}
	// token、err 保存解密后的令牌及解密错误；错误不包含密文。
	token, err := a.codec.decrypt(agentTokenScope, cookieID, rawToken)
	if err != nil {
		return AgentReplyRuntime{}, fmt.Errorf("读取 Agent 令牌: %w", err)
	}
	return AgentReplyRuntime{
		Enabled: enabled != 0, EndpointURL: endpoint, Token: token,
		RequestTimeout: time.Duration(timeout) * time.Second, MaxSession: time.Duration(session) * time.Second,
	}, nil
}

// migrateLegacyAgentReplyTokens 把启用数据密钥前写入的明文 Agent 令牌升级为密文，并校验已加密行的密钥。
func migrateLegacyAgentReplyTokens(ctx context.Context, tx *sql.Tx, codec *secretCodec) error {
	// rows、err 保存带令牌的配置行游标及查询错误。
	rows, err := tx.QueryContext(ctx, `SELECT cookie_id, auth_token FROM agent_reply_settings WHERE auth_token <> ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	// legacyToken 是待迁移的账号令牌行：账号标识与库中当前的令牌文本。
	type legacyToken struct{ cookieID, token string }
	// pending 保存需要加密或校验的账号标识与令牌。
	var pending []legacyToken
	for rows.Next() {
		// row 保存当前扫描出的账号令牌行。
		var row legacyToken
		// err 表示当前令牌行的扫描错误。
		if err := rows.Scan(&row.cookieID, &row.token); err != nil {
			return err
		}
		pending = append(pending, row)
	}
	// err 表示游标遍历过程中的错误。
	if err := rows.Err(); err != nil {
		return err
	}
	// row 表示当前待迁移的令牌行。
	for _, row := range pending {
		// encrypted、err 保存密文及加密或密钥校验错误。
		encrypted, err := codec.encrypt(agentTokenScope, row.cookieID, row.token)
		if err != nil {
			return fmt.Errorf("校验账号 %s 的 Agent 令牌: %w", row.cookieID, err)
		}
		if encrypted != row.token {
			// err 表示回写令牌密文时的数据库错误。
			if _, err := tx.ExecContext(ctx, `UPDATE agent_reply_settings SET auth_token=? WHERE cookie_id=?`, encrypted, row.cookieID); err != nil {
				return err
			}
		}
	}
	return nil
}
