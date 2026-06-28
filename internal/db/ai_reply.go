package db

import (
	"context"
	"database/sql"
	"errors"
)

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
type AIReply struct{ DB *sql.DB }

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
	s.APIKey = apiKey.String
	s.CustomPrompts = customPrompts.String
	if s.ModelName == "" {
		s.ModelName = "qwen-plus"
	}
	if s.BaseURL == "" {
		s.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	return &s, nil
}
