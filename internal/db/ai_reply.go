package db

import (
	"context"
	"database/sql"
	"errors"
)

// AIConversationMessage 是一个账号会话中的 AI 对话消息。
type AIConversationMessage struct {
	Role         string
	Content      string
	Intent       string
	BargainCount int
}

// AIReplySettings 对应 ai_reply_settings 表。
type AIReplySettings struct {
	CookieID           string `json:"cookie_id"`
	AIEnabled          bool   `json:"ai_enabled"`
	ModelName          string `json:"model_name"`
	APIKey             string `json:"api_key"`
	BaseURL            string `json:"base_url"`
	MaxDiscountPercent int    `json:"max_discount_percent"`
	MaxDiscountAmount  int    `json:"max_discount_amount"`
	MaxBargainRounds   int    `json:"max_bargain_rounds"`
	CustomPrompts      string `json:"custom_prompts"`
}

// AIReply 操作。
type AIReply struct {
	DB      *sql.DB
	Dialect Dialect
	codec   *secretCodec
}

// Get 取某账号 AI 回复配置。
func (a *AIReply) Get(ctx context.Context, cookieID string) (*AIReplySettings, error) {
	// s 保存s，供当前处理流程使用
	var s AIReplySettings
	// enabled 保存启用状态，供当前处理流程使用
	var enabled int
	// apiKey、customPrompts 保存apiKey、customPrompts，供当前处理流程使用
	var apiKey, customPrompts sql.NullString
	// err 保存err，供当前处理流程使用
	err := a.DB.QueryRowContext(ctx,
		`SELECT cookie_id, ai_enabled, COALESCE(model_name, ''), COALESCE(api_key, ''), COALESCE(base_url, ''),
		        max_discount_percent, max_discount_amount, max_bargain_rounds, custom_prompts
		 FROM ai_reply_settings WHERE cookie_id=?`, cookieID).Scan(
		&s.CookieID, &enabled, &s.ModelName, &apiKey, &s.BaseURL,
		&s.MaxDiscountPercent, &s.MaxDiscountAmount, &s.MaxBargainRounds, &customPrompts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.AIEnabled = enabled != 0
	s.APIKey, err = a.codec.decrypt("ai-api-key", cookieID, apiKey.String)
	if err != nil {
		return nil, err
	}
	s.CustomPrompts = customPrompts.String
	if s.ModelName == "" {
		s.ModelName = "qwen-plus"
	}
	if s.BaseURL == "" {
		s.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	return &s, nil
}

// ListForUser 查询用户账号的 AI 回复配置，不读取或返回 API 密钥。
func (a *AIReply) ListForUser(ctx context.Context, userID int64) ([]AIReplySettings, error) {
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := a.DB.QueryContext(ctx, `
		SELECT a.cookie_id, a.ai_enabled, a.max_discount_percent, a.max_discount_amount,
		       a.max_bargain_rounds, COALESCE(a.custom_prompts, '')
		  FROM ai_reply_settings a JOIN cookies c ON c.id=a.cookie_id WHERE c.user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 保存out，供当前处理流程使用
	var out []AIReplySettings
	for rows.Next() {
		// item 保存商品，供当前处理流程使用
		var item AIReplySettings
		// enabled 保存启用状态，供当前处理流程使用
		var enabled int
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&item.CookieID, &enabled, &item.MaxDiscountPercent, &item.MaxDiscountAmount, &item.MaxBargainRounds, &item.CustomPrompts); err != nil {
			return nil, err
		}
		item.AIEnabled = enabled != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpsertSettings 保存指定账号的 AI 回复开关和砍价约束。
func (a *AIReply) UpsertSettings(ctx context.Context, cookieID string, settings AIReplySettings) error {
	// err 保存err，供当前处理流程使用
	_, err := a.DB.ExecContext(ctx,
		`INSERT INTO ai_reply_settings
		 (cookie_id, ai_enabled, max_discount_percent, max_discount_amount,
		  max_bargain_rounds, custom_prompts, updated_at)
		 VALUES (?,?,?,?,?,?,CURRENT_TIMESTAMP)`+dialectUpsert(a.Dialect, []string{"cookie_id"}, map[string]string{
			"ai_enabled":           "EXCLUDED.ai_enabled",
			"max_discount_percent": "EXCLUDED.max_discount_percent",
			"max_discount_amount":  "EXCLUDED.max_discount_amount",
			"max_bargain_rounds":   "EXCLUDED.max_bargain_rounds",
			"custom_prompts":       "EXCLUDED.custom_prompts",
			"updated_at":           "CURRENT_TIMESTAMP",
		}), cookieID, boolToInt(settings.AIEnabled), settings.MaxDiscountPercent,
		settings.MaxDiscountAmount, settings.MaxBargainRounds, nullableAIString(settings.CustomPrompts))
	return err
}

// nullableAIString 将空的自定义提示词转换为数据库 NULL。
func nullableAIString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// ConversationHistory 返回最近的会话消息，结果按时间正序排列。
func (a *AIReply) ConversationHistory(ctx context.Context, cookieID, chatID, itemID string, limit int) ([]AIConversationMessage, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := a.DB.QueryContext(ctx, `
		SELECT role, content, COALESCE(intent,''), COALESCE(bargain_count,0)
		  FROM ai_conversations
		 WHERE cookie_id=? AND chat_id=? AND item_id=?
		 ORDER BY id DESC LIMIT ?`, cookieID, chatID, itemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// reversed 保存reversed，供当前处理流程使用
	var reversed []AIConversationMessage
	for rows.Next() {
		// message 保存消息，供当前处理流程使用
		var message AIConversationMessage
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&message.Role, &message.Content, &message.Intent, &message.BargainCount); err != nil {
			return nil, err
		}
		reversed = append(reversed, message)
	}
	if // err 保存err，供当前处理流程使用
	err := rows.Err(); err != nil {
		return nil, err
	}
	// result 保存结果，供当前处理流程使用
	result := make([]AIConversationMessage, len(reversed))
	// i 表示当前遍历过程中的i
	for i := range reversed {
		result[len(reversed)-1-i] = reversed[i]
	}
	return result, nil
}

// CurrentBargainCount 返回会话目前的砍价轮次。
func (a *AIReply) CurrentBargainCount(ctx context.Context, cookieID, chatID, itemID string) (int, error) {
	// count 保存数量，供当前处理流程使用
	var count int
	// err 保存err，供当前处理流程使用
	err := a.DB.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(bargain_count),0) FROM ai_conversations
		 WHERE cookie_id=? AND chat_id=? AND item_id=?`, cookieID, chatID, itemID).Scan(&count)
	return count, err
}

// AddConversation 追加一条会话消息。
func (a *AIReply) AddConversation(ctx context.Context, cookieID, chatID, userID, itemID string, message AIConversationMessage) error {
	// err 保存err，供当前处理流程使用
	_, err := a.DB.ExecContext(ctx, `
		INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content,intent,bargain_count)
		VALUES (?,?,?,?,?,?,?,?)`, cookieID, chatID, userID, itemID, message.Role, message.Content, message.Intent, message.BargainCount)
	return err
}

// AddConversationExchange 原子保存一轮用户消息与 AI 回复，避免上游调用失败时
// 留下半轮历史并错误消耗砍价轮次。
// AddConversationExchange 新增ConversationExchange。
func (a *AIReply) AddConversationExchange(ctx context.Context, cookieID, chatID, userID, itemID string, userMessage, assistantMessage AIConversationMessage) error {
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// query 保存查询，供当前处理流程使用
	query := `INSERT INTO ai_conversations
		(cookie_id,chat_id,user_id,item_id,role,content,intent,bargain_count)
		VALUES (?,?,?,?,?,?,?,?)`
	// message 表示当前遍历过程中的消息
	for _, message := range []AIConversationMessage{userMessage, assistantMessage} {
		if // err 保存err，供当前处理流程使用
		_, err := tx.ExecContext(ctx, query, cookieID, chatID, userID, itemID, message.Role, message.Content, message.Intent, message.BargainCount); err != nil {
			return err
		}
	}
	return tx.Commit()
}
