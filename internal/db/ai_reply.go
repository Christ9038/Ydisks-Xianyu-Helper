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
	DB    *sql.DB
	codec *secretCodec
}

// Get 取某账号 AI 回复配置。
func (a *AIReply) Get(ctx context.Context, cookieID string) (*AIReplySettings, error) {
	var s AIReplySettings
	var enabled int
	var apiKey, customPrompts sql.NullString
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

// ConversationHistory 返回最近的会话消息，结果按时间正序排列。
func (a *AIReply) ConversationHistory(ctx context.Context, cookieID, chatID, itemID string, limit int) ([]AIConversationMessage, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rows, err := a.DB.QueryContext(ctx, `
		SELECT role, content, COALESCE(intent,''), COALESCE(bargain_count,0)
		  FROM ai_conversations
		 WHERE cookie_id=? AND chat_id=? AND item_id=?
		 ORDER BY id DESC LIMIT ?`, cookieID, chatID, itemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reversed []AIConversationMessage
	for rows.Next() {
		var message AIConversationMessage
		if err := rows.Scan(&message.Role, &message.Content, &message.Intent, &message.BargainCount); err != nil {
			return nil, err
		}
		reversed = append(reversed, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]AIConversationMessage, len(reversed))
	for i := range reversed {
		result[len(reversed)-1-i] = reversed[i]
	}
	return result, nil
}

// CurrentBargainCount 返回会话目前的砍价轮次。
func (a *AIReply) CurrentBargainCount(ctx context.Context, cookieID, chatID, itemID string) (int, error) {
	var count int
	err := a.DB.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(bargain_count),0) FROM ai_conversations
		 WHERE cookie_id=? AND chat_id=? AND item_id=?`, cookieID, chatID, itemID).Scan(&count)
	return count, err
}

// AddConversation 追加一条会话消息。
func (a *AIReply) AddConversation(ctx context.Context, cookieID, chatID, userID, itemID string, message AIConversationMessage) error {
	_, err := a.DB.ExecContext(ctx, `
		INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content,intent,bargain_count)
		VALUES (?,?,?,?,?,?,?,?)`, cookieID, chatID, userID, itemID, message.Role, message.Content, message.Intent, message.BargainCount)
	return err
}

// AddConversationExchange 原子保存一轮用户消息与 AI 回复，避免上游调用失败时
// 留下半轮历史并错误消耗砍价轮次。
func (a *AIReply) AddConversationExchange(ctx context.Context, cookieID, chatID, userID, itemID string, userMessage, assistantMessage AIConversationMessage) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `INSERT INTO ai_conversations
		(cookie_id,chat_id,user_id,item_id,role,content,intent,bargain_count)
		VALUES (?,?,?,?,?,?,?,?)`
	for _, message := range []AIConversationMessage{userMessage, assistantMessage} {
		if _, err := tx.ExecContext(ctx, query, cookieID, chatID, userID, itemID, message.Role, message.Content, message.Intent, message.BargainCount); err != nil {
			return err
		}
	}
	return tx.Commit()
}
