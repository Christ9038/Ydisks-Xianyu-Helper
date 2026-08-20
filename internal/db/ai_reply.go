package db

import (
	"context"
	"database/sql"
	"errors"
)

// ErrAIBargainQuoteAlreadyClaimed 表示同一订单已绑定过 AI 报价，重复事件不得再次执行外部改价。
var ErrAIBargainQuoteAlreadyClaimed = errors.New("AI 议价报价已被该订单领取")

// AIConversationMessage 是一个账号会话中的 AI 对话消息。
type AIConversationMessage struct {
	Role         string
	Content      string
	Intent       string
	BargainCount int
}

// AIReplySettings 对应 ai_reply_settings 表。
type AIReplySettings struct {
	CookieID               string `json:"cookie_id"`
	AIEnabled              bool   `json:"ai_enabled"`
	AutoAdjustPriceEnabled bool   `json:"auto_adjust_price_enabled"`
	ModelName              string `json:"model_name"`
	APIKey                 string `json:"api_key"`
	BaseURL                string `json:"base_url"`
	MaxDiscountPercent     int    `json:"max_discount_percent"`
	MaxDiscountAmount      int    `json:"max_discount_amount"`
	MaxBargainRounds       int    `json:"max_bargain_rounds"`
	CustomPrompts          string `json:"custom_prompts"`
}

// AIBargainQuote 保存一条已经成功发送给买家的、可用于订单改价的 AI 报价。
type AIBargainQuote struct {
	// ID 是报价持久化主键。
	ID int64
	// CookieID 是卖家账号标识。
	CookieID string
	// ChatID 是产生报价的会话标识。
	ChatID string
	// BuyerID 是收到报价的买家标识。
	BuyerID string
	// ItemID 是报价对应的商品标识。
	ItemID string
	// PriceCents 是以分为单位的单件成交报价；订单执行方负责按购买数量折算总价。
	PriceCents int64
	// OrderID 是领取该报价的订单标识；待领取时为空。
	OrderID string
}

// AIReply 操作。
type AIReply struct {
	DB      *sql.DB
	Dialect Dialect
	codec   *secretCodec
}

// IsEnabled 只读取账号 AI 议价开关，不解密模型密钥或其他敏感字段。
func (a *AIReply) IsEnabled(ctx context.Context, cookieID string) (bool, error) {
	// enabled 是数据库保存的 AI 议价开关整数值。
	var enabled int
	// err 是开关查询错误；没有配置时按关闭处理。
	err := a.DB.QueryRowContext(ctx, `SELECT ai_enabled FROM ai_reply_settings WHERE cookie_id=?`, cookieID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return enabled != 0, err
}

// PricingMode 只读取 AI 议价及其真实改价开关，供订单事件选择互斥执行路径。
func (a *AIReply) PricingMode(ctx context.Context, cookieID string) (bool, bool, error) {
	// aiEnabled、autoAdjustEnabled 分别是 AI 议价和真实自动改价开关的数据库整数值。
	var aiEnabled, autoAdjustEnabled int
	// err 是开关查询错误；没有账号级 AI 配置时两个开关均按关闭处理。
	err := a.DB.QueryRowContext(ctx, `SELECT ai_enabled,auto_adjust_price_enabled FROM ai_reply_settings WHERE cookie_id=?`, cookieID).Scan(&aiEnabled, &autoAdjustEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return aiEnabled != 0, autoAdjustEnabled != 0, err
}

// Get 取某账号 AI 回复配置。
func (a *AIReply) Get(ctx context.Context, cookieID string) (*AIReplySettings, error) {
	// s 用于本次流程后续判断的s
	var s AIReplySettings
	// enabled 用于本次流程后续判断的启用状态
	var enabled int
	// autoAdjustEnabled 表示 AI 报价是否允许在订单创建后触发真实改价。
	var autoAdjustEnabled int
	// apiKey、customPrompts 用于本次流程后续判断的apiKey、customPrompts
	var apiKey, customPrompts sql.NullString
	// err 用于本次流程后续判断的err
	err := a.DB.QueryRowContext(ctx,
		`SELECT cookie_id, ai_enabled, auto_adjust_price_enabled, COALESCE(model_name, ''), COALESCE(api_key, ''), COALESCE(base_url, ''),
		        max_discount_percent, max_discount_amount, max_bargain_rounds, custom_prompts
		 FROM ai_reply_settings WHERE cookie_id=?`, cookieID).Scan(
		&s.CookieID, &enabled, &autoAdjustEnabled, &s.ModelName, &apiKey, &s.BaseURL,
		&s.MaxDiscountPercent, &s.MaxDiscountAmount, &s.MaxBargainRounds, &customPrompts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.AIEnabled = enabled != 0
	s.AutoAdjustPriceEnabled = autoAdjustEnabled != 0
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
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := a.DB.QueryContext(ctx, `
		SELECT a.cookie_id, a.ai_enabled, a.auto_adjust_price_enabled, a.max_discount_percent, a.max_discount_amount,
		       a.max_bargain_rounds, COALESCE(a.custom_prompts, '')
		  FROM ai_reply_settings a JOIN cookies c ON c.id=a.cookie_id WHERE c.user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 用于本次流程后续判断的out
	var out []AIReplySettings
	for rows.Next() {
		// item 用于本次流程后续判断的商品
		var item AIReplySettings
		// enabled 用于本次流程后续判断的启用状态
		var enabled, autoAdjustEnabled int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&item.CookieID, &enabled, &autoAdjustEnabled, &item.MaxDiscountPercent, &item.MaxDiscountAmount, &item.MaxBargainRounds, &item.CustomPrompts); err != nil {
			return nil, err
		}
		item.AIEnabled = enabled != 0
		item.AutoAdjustPriceEnabled = autoAdjustEnabled != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpsertSettings 保存指定账号的 AI 回复开关和砍价约束。
func (a *AIReply) UpsertSettings(ctx context.Context, cookieID string, settings AIReplySettings) error {
	// err 用于本次流程后续判断的err
	_, err := a.DB.ExecContext(ctx,
		`INSERT INTO ai_reply_settings
		 (cookie_id, ai_enabled, auto_adjust_price_enabled, max_discount_percent, max_discount_amount,
		  max_bargain_rounds, custom_prompts, updated_at)
		 VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`+dialectUpsert(a.Dialect, []string{"cookie_id"}, map[string]string{
			"ai_enabled":                "EXCLUDED.ai_enabled",
			"auto_adjust_price_enabled": "EXCLUDED.auto_adjust_price_enabled",
			"max_discount_percent":      "EXCLUDED.max_discount_percent",
			"max_discount_amount":       "EXCLUDED.max_discount_amount",
			"max_bargain_rounds":        "EXCLUDED.max_bargain_rounds",
			"custom_prompts":            "EXCLUDED.custom_prompts",
			"updated_at":                "CURRENT_TIMESTAMP",
		}), cookieID, boolToInt(settings.AIEnabled), boolToInt(settings.AutoAdjustPriceEnabled), settings.MaxDiscountPercent,
		settings.MaxDiscountAmount, settings.MaxBargainRounds, nullableAIString(settings.CustomPrompts))
	return err
}

// ReplacePendingQuote 使同一买家和商品的旧报价失效，并保存最新的可执行报价。
func (a *AIReply) ReplacePendingQuote(ctx context.Context, quote AIBargainQuote, expiresAt int64) error {
	// tx 保证旧报价失效与新报价写入不会留下并行的有效报价；err 是事务创建错误。
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE ai_bargain_quotes
		SET status='superseded',updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND buyer_id=? AND item_id=? AND status='pending'`, quote.CookieID, quote.BuyerID, quote.ItemID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ai_bargain_quotes
		(cookie_id,chat_id,buyer_id,item_id,price_cents,status,expires_at,error_message)
		VALUES (?,?,?,?,?,'pending',?,'')`, quote.CookieID, quote.ChatID, quote.BuyerID, quote.ItemID, quote.PriceCents, expiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

// ClaimPendingQuote 按订单事实原子领取最新有效报价；重复订单事件不会领取第二条报价。
func (a *AIReply) ClaimPendingQuote(ctx context.Context, cookieID, chatID, buyerID, itemID, orderID string, now int64) (*AIBargainQuote, error) {
	// tx 串联订单防重检查、报价选择和领取状态更新；err 是事务创建错误。
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// existingCount 是已经绑定到当前订单的报价数量，非零表示重复事件。
	var existingCount int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_bargain_quotes WHERE cookie_id=? AND order_id=?`, cookieID, orderID).Scan(&existingCount); err != nil {
		return nil, err
	}
	if existingCount > 0 {
		return nil, ErrAIBargainQuoteAlreadyClaimed
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_bargain_quotes SET status='expired',updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND status='pending' AND expires_at<?`, cookieID, now); err != nil {
		return nil, err
	}
	// quote 保存与账号、买家、商品和会话完全匹配的最新报价。
	quote := AIBargainQuote{CookieID: cookieID, ChatID: chatID, BuyerID: buyerID, ItemID: itemID, OrderID: orderID}
	if err = tx.QueryRowContext(ctx, `SELECT id,price_cents FROM ai_bargain_quotes
		WHERE cookie_id=? AND chat_id=? AND buyer_id=? AND item_id=? AND status='pending' AND expires_at>=?
		ORDER BY id DESC LIMIT 1`, cookieID, chatID, buyerID, itemID, now).Scan(&quote.ID, &quote.PriceCents); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// result 是带状态前置条件的领取结果，避免同一报价被并发订单重复消费。
	result, err := tx.ExecContext(ctx, `UPDATE ai_bargain_quotes
		SET status='processing',order_id=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='pending'`, orderID, quote.ID)
	if err != nil {
		return nil, err
	}
	// affected 是成功领取的报价行数；零表示另一个执行者已经抢先领取。
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, nil
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &quote, nil
}

// FinishQuote 保存 AI 报价改价的确定终态或人工核对状态。
func (a *AIReply) FinishQuote(ctx context.Context, quoteID int64, status, errorMessage string) error {
	// result 是仅允许 processing 状态收口的更新结果；err 是数据库更新错误。
	result, err := a.DB.ExecContext(ctx, `UPDATE ai_bargain_quotes
		SET status=?,error_message=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='processing'`, status, errorMessage, quoteID)
	if err != nil {
		return err
	}
	// affected 是成功收口的报价行数，非一表示执行状态已被意外修改。
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrNotFound
	}
	return nil
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
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := a.DB.QueryContext(ctx, `
		SELECT role, content, COALESCE(intent,''), COALESCE(bargain_count,0)
		  FROM ai_conversations
		 WHERE cookie_id=? AND chat_id=? AND item_id=?
		 ORDER BY id DESC LIMIT ?`, cookieID, chatID, itemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// reversed 用于本次流程后续判断的reversed
	var reversed []AIConversationMessage
	for rows.Next() {
		// message 用于本次流程后续判断的消息
		var message AIConversationMessage
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&message.Role, &message.Content, &message.Intent, &message.BargainCount); err != nil {
			return nil, err
		}
		reversed = append(reversed, message)
	}
	if // err 用于本次流程后续判断的err
	err := rows.Err(); err != nil {
		return nil, err
	}
	// result 用于本次流程后续判断的结果
	result := make([]AIConversationMessage, len(reversed))
	// i 表示当前遍历过程中的i
	for i := range reversed {
		result[len(reversed)-1-i] = reversed[i]
	}
	return result, nil
}

// CurrentBargainCount 返回会话目前的砍价轮次。
func (a *AIReply) CurrentBargainCount(ctx context.Context, cookieID, chatID, itemID string) (int, error) {
	// count 用于本次流程后续判断的数量
	var count int
	// err 用于本次流程后续判断的err
	err := a.DB.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(bargain_count),0) FROM ai_conversations
		 WHERE cookie_id=? AND chat_id=? AND item_id=?`, cookieID, chatID, itemID).Scan(&count)
	return count, err
}

// AddConversation 追加一条会话消息。
func (a *AIReply) AddConversation(ctx context.Context, cookieID, chatID, userID, itemID string, message AIConversationMessage) error {
	// err 用于本次流程后续判断的err
	_, err := a.DB.ExecContext(ctx, `
		INSERT INTO ai_conversations (cookie_id,chat_id,user_id,item_id,role,content,intent,bargain_count)
		VALUES (?,?,?,?,?,?,?,?)`, cookieID, chatID, userID, itemID, message.Role, message.Content, message.Intent, message.BargainCount)
	return err
}

// AddConversationExchange 原子保存一轮用户消息与 AI 回复，避免上游调用失败时
// 留下半轮历史并错误消耗砍价轮次。
// AddConversationExchange 新增ConversationExchange。
func (a *AIReply) AddConversationExchange(ctx context.Context, cookieID, chatID, userID, itemID string, userMessage, assistantMessage AIConversationMessage) error {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// query 用于本次流程后续判断的查询
	query := `INSERT INTO ai_conversations
		(cookie_id,chat_id,user_id,item_id,role,content,intent,bargain_count)
		VALUES (?,?,?,?,?,?,?,?)`
	// message 表示当前遍历过程中的消息
	for _, message := range []AIConversationMessage{userMessage, assistantMessage} {
		if // err 用于本次流程后续判断的err
		_, err := tx.ExecContext(ctx, query, cookieID, chatID, userID, itemID, message.Role, message.Content, message.Intent, message.BargainCount); err != nil {
			return err
		}
	}
	return tx.Commit()
}
