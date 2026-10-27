package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// Keyword 对应 keywords 表，并把兼容的首表达式和完整表达式集合一起提供给运行时。
type Keyword struct {
	// Keyword 是第一条表达式，用于兼容历史单关键词字段。
	Keyword string
	// Expressions 是同一条规则的完整匹配表达式集合。
	Expressions []string
	// MatchType 是 contains 或 regexp；历史行缺失时由读取逻辑回退为 contains。
	MatchType string
	// Reply 是文字回复内容。
	Reply string
	// ItemID 是逗号分隔的商品范围字段。
	ItemID string
	// Type 是 text 或 image 回复类型。
	Type string
	// ImageURL 是图片回复地址。
	ImageURL string
}

// decodeKeywordExpressions 从 JSON 文本读取表达式集合；非法或空数据回退兼容首表达式。
func decodeKeywordExpressions(raw, fallback string) []string {
	// decoded 保存 JSON 中反序列化出的原始表达式集合。
	var decoded []string
	if strings.TrimSpace(raw) != "" && json.Unmarshal([]byte(raw), &decoded) == nil {
		// result 保存去空白、去重后的可匹配表达式集合。
		result := make([]string, 0, len(decoded))
		// seen 记录已经加入结果的表达式。
		seen := make(map[string]struct{}, len(decoded))
		// expression 表示当前从数据库读取的表达式。
		for _, expression := range decoded {
			expression = strings.TrimSpace(expression)
			if expression == "" {
				continue
			}
			// exists 表示当前表达式是否已经收录，重复表达式不会改变原有顺序。
			if _, exists := seen[expression]; exists {
				continue
			}
			seen[expression] = struct{}{}
			result = append(result, expression)
		}
		if len(result) > 0 {
			return result
		}
	}
	// normalizedFallback 保存历史单关键词字段的兼容表达式。
	normalizedFallback := strings.TrimSpace(fallback)
	if normalizedFallback == "" {
		return nil
	}
	return []string{normalizedFallback}
}

// encodeKeywordExpressions 将表达式集合编码为数据库文本，并为旧调用方提供首表达式回退。
func encodeKeywordExpressions(expressions []string, fallback string) string {
	// normalized 保存可安全写入的表达式集合。
	normalized := make([]string, 0, len(expressions))
	// seen 记录已经加入编码集合的表达式。
	seen := make(map[string]struct{}, len(expressions))
	// expression 表示当前待编码的表达式。
	for _, expression := range expressions {
		expression = strings.TrimSpace(expression)
		if expression == "" {
			continue
		}
		// exists 表示当前表达式是否已经收录，重复表达式不会写入 JSON 数组。
		if _, exists := seen[expression]; exists {
			continue
		}
		seen[expression] = struct{}{}
		normalized = append(normalized, expression)
	}
	if len(normalized) == 0 {
		normalized = decodeKeywordExpressions("", fallback)
	}
	if normalized == nil {
		// 没有任何可用表达式时编码为空数组 []，避免 nil 切片被编码成字符串 null 写入数据库。
		normalized = []string{}
	}
	// encoded 保存 JSON 编码结果；字符串数组不会产生不可编码值。
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// keywordPersistenceFields 统一兼容首表达式、表达式 JSON 和匹配模式的写入字段。
func keywordPersistenceFields(keyword string, expressions []string, matchType string) (string, string, string) {
	// normalizedExpressions 保存传入表达式的可写副本。
	normalizedExpressions := decodeKeywordExpressions(encodeKeywordExpressions(expressions, keyword), keyword)
	if len(normalizedExpressions) > 0 {
		keyword = normalizedExpressions[0]
	}
	// normalizedMatchType 保存缺省匹配模式的兼容值。
	normalizedMatchType := normalizeKeywordMatchType(matchType)
	return keyword, encodeKeywordExpressions(normalizedExpressions, keyword), normalizedMatchType
}

// normalizeKeywordMatchType 将数据库中的匹配模式规整为大小写统一的值；空值兼容为 contains。
func normalizeKeywordMatchType(raw string) string {
	// normalized 保存去除空白并统一大小写后的匹配模式。
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return "contains"
	}
	return normalized
}

// DefaultReply 对应 default_replies 表。
type DefaultReply struct {
	Enabled       bool
	ReplyContent  string
	ReplyImageURL string
	ReplyOnce     bool
}

// DefaultReplySummary 是按账号查询默认回复列表时使用的带账号标识视图。
type DefaultReplySummary struct {
	// CookieID 是默认回复所属账号标识。
	CookieID string
	// Enabled 表示默认回复是否启用。
	Enabled bool
	// ReplyContent 是默认回复文字。
	ReplyContent string
	// ReplyImageURL 是默认回复图片地址。
	ReplyImageURL string
	// ReplyOnce 表示同一聊天是否只发送一次。
	ReplyOnce bool
}

// DefaultReplyRecord 记录 reply_once 消息各部分的投递状态。
type DefaultReplyRecord struct {
	Status    string
	TextSent  bool
	ImageSent bool
}

// uncertainReplyErrorPrefix 标记未知投递结果的降级隔离记录，阻止租约到期后自动重发。
const uncertainReplyErrorPrefix = "uncertain:"

// defaultReplyStatusSending 表示已经领取且即将调用外部发送接口的持久化状态。
// 该状态的租约过期后也不能自动接管，避免数据库故障时重复发送一次性消息。
const defaultReplyStatusSending = "sending"

// ItemReply 对应 item_replay 表（指定商品回复）。
type ItemReply struct {
	ItemID       string
	CookieID     string
	ReplyContent string
}

// Keywords 关键字操作。
type Keywords struct {
	DB      *sql.DB
	Dialect Dialect
}

// AllWithType 取某账号所有关键词（含类型、图片、表达式和匹配模式）。
func (k *Keywords) AllWithType(ctx context.Context, cookieID string) ([]Keyword, error) {
	// 查询继续按旧 keyword 列长度降序、主键升序，后续表达式不会改变跨规则优先级。
	// rows、err 保存数据库查询结果。
	rows, err := k.DB.QueryContext(ctx,
		`SELECT keyword, reply, COALESCE(item_id,''), COALESCE(type,'text'), COALESCE(image_url,''),
				COALESCE(keyword_expressions,''), COALESCE(match_type,'contains')
			 FROM keywords WHERE cookie_id=? ORDER BY LENGTH(keyword) DESC,id ASC`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 保存当前账号的关键词规则。
	var out []Keyword
	for rows.Next() {
		// kw 表示当前读取的关键词规则。
		var kw Keyword
		// rawExpressions 保存数据库中的表达式 JSON 文本。
		var rawExpressions string
		if // err 表示当前行扫描错误。
		err := rows.Scan(&kw.Keyword, &kw.Reply, &kw.ItemID, &kw.Type, &kw.ImageURL, &rawExpressions, &kw.MatchType); err != nil {
			return nil, err
		}
		kw.Expressions = decodeKeywordExpressions(rawExpressions, kw.Keyword)
		if len(kw.Expressions) > 0 {
			kw.Keyword = kw.Expressions[0]
		}
		kw.MatchType = normalizeKeywordMatchType(kw.MatchType)
		out = append(out, kw)
	}
	return out, rows.Err()
}

// DefaultReplies 默认回复操作。
type DefaultReplies struct {
	DB      *sql.DB
	Dialect Dialect
}

// Get 取某账号默认回复设置。不存在返回 ErrNotFound。
func (d *DefaultReplies) Get(ctx context.Context, cookieID string) (*DefaultReply, error) {
	// dr 用于本次流程后续判断的dr
	var dr DefaultReply
	// enabled、replyOnce 用于本次流程后续判断的enabled、replyOnce
	var enabled, replyOnce int
	// content、imageURL 用于本次流程后续判断的content、imageURL
	var content, imageURL sql.NullString
	// err 用于本次流程后续判断的err
	err := d.DB.QueryRowContext(ctx,
		`SELECT enabled, reply_content, reply_image_url, reply_once FROM default_replies WHERE cookie_id=?`,
		cookieID).Scan(&enabled, &content, &imageURL, &replyOnce)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	dr.Enabled = enabled != 0
	dr.ReplyContent = content.String
	dr.ReplyImageURL = imageURL.String
	dr.ReplyOnce = replyOnce != 0
	return &dr, nil
}

// Upsert 保存或覆盖指定账号的默认回复配置。
func (d *DefaultReplies) Upsert(ctx context.Context, cookieID string, reply DefaultReply) error {
	// err 用于本次流程后续判断的err
	_, err := d.DB.ExecContext(ctx,
		`INSERT INTO default_replies (cookie_id, enabled, reply_content, reply_image_url, reply_once, updated_at)
		 VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)`+dialectUpsert(d.Dialect, []string{"cookie_id"}, map[string]string{
			"enabled":         "EXCLUDED.enabled",
			"reply_content":   "EXCLUDED.reply_content",
			"reply_image_url": "EXCLUDED.reply_image_url",
			"reply_once":      "EXCLUDED.reply_once",
			"updated_at":      "CURRENT_TIMESTAMP",
		}),
		cookieID, boolToInt(reply.Enabled), reply.ReplyContent, defaultReplyNullableString(reply.ReplyImageURL), boolToInt(reply.ReplyOnce))
	return err
}

// defaultReplyNullableString 将空图片地址转换为数据库 NULL，保持历史存储语义。
func defaultReplyNullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// ListForUser 查询用户所有账号的默认回复配置。
func (d *DefaultReplies) ListForUser(ctx context.Context, userID int64) ([]DefaultReplySummary, error) {
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := d.DB.QueryContext(ctx, `
		SELECT dr.cookie_id, dr.enabled, COALESCE(dr.reply_content,''), dr.reply_once, COALESCE(dr.reply_image_url,'')
		  FROM default_replies dr JOIN cookies c ON c.id=dr.cookie_id WHERE c.user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 用于本次流程后续判断的out
	var out []DefaultReplySummary
	for rows.Next() {
		// item 用于本次流程后续判断的商品
		var item DefaultReplySummary
		// enabled、replyOnce 用于本次流程后续判断的enabled、replyOnce
		var enabled, replyOnce int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&item.CookieID, &enabled, &item.ReplyContent, &replyOnce, &item.ReplyImageURL); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		item.ReplyOnce = replyOnce != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

// Delete 删除指定账号的默认回复配置。
func (d *DefaultReplies) Delete(ctx context.Context, cookieID string) error {
	// err 用于本次流程后续判断的err
	_, err := d.DB.ExecContext(ctx, `DELETE FROM default_replies WHERE cookie_id=?`, cookieID)
	return err
}

// ClearRecords 清空指定账号的默认回复投递记录。
func (d *DefaultReplies) ClearRecords(ctx context.Context, cookieID string) error {
	// err 用于本次流程后续判断的err
	_, err := d.DB.ExecContext(ctx, `DELETE FROM default_reply_records WHERE cookie_id=?`, cookieID)
	return err
}

// HasRecord 是否已对该 chat_id 回复过（reply_once 用）。
func (d *DefaultReplies) HasRecord(ctx context.Context, cookieID, chatID string) bool {
	// n 用于本次流程后续判断的n
	var n int
	// err 用于本次流程后续判断的err
	err := d.DB.QueryRowContext(ctx,
		`SELECT 1 FROM default_reply_records WHERE cookie_id=? AND chat_id=? AND status='sent' LIMIT 1`,
		cookieID, chatID).Scan(&n)
	return err == nil
}

// onceRecords 返回账号默认回复记录表对应的一次性投递状态机。
func (d *DefaultReplies) onceRecords() onceReplyRecords {
	return onceReplyRecords{db: d.DB, dialect: d.Dialect, table: "default_reply_records"}
}

// ClaimRecord 原子领取一次默认回复投递；语义见 onceReplyRecords.ClaimRecord。
func (d *DefaultReplies) ClaimRecord(ctx context.Context, cookieID, chatID string, needsText, needsImage bool) (DefaultReplyRecord, bool, error) {
	return d.onceRecords().ClaimRecord(ctx, cookieID, chatID, needsText, needsImage)
}

// Record 查询一次默认回复的投递状态。
func (d *DefaultReplies) Record(ctx context.Context, cookieID, chatID string) (DefaultReplyRecord, error) {
	return d.onceRecords().Record(ctx, cookieID, chatID)
}

// MarkPartSent 标记图片或文字已经成功投递。
func (d *DefaultReplies) MarkPartSent(ctx context.Context, cookieID, chatID, part string) error {
	return d.onceRecords().MarkPartSent(ctx, cookieID, chatID, part)
}

// MarkRecordFailed 标记确定未送达的失败状态，允许后续继续投递。
func (d *DefaultReplies) MarkRecordFailed(ctx context.Context, cookieID, chatID, message string) error {
	return d.onceRecords().MarkRecordFailed(ctx, cookieID, chatID, message)
}

// MarkRecordUncertain 隔离可能已经发送但未得到可靠本地确认的默认回复，避免自动重发。
func (d *DefaultReplies) MarkRecordUncertain(ctx context.Context, cookieID, chatID, message string) error {
	return d.onceRecords().MarkRecordUncertain(ctx, cookieID, chatID, message)
}

// MarkRecordSent 标记完整回复已经投递完成。
func (d *DefaultReplies) MarkRecordSent(ctx context.Context, cookieID, chatID string) error {
	return d.onceRecords().MarkRecordSent(ctx, cookieID, chatID)
}

// AddRecord 记录已回复（reply_once 防重复）。
func (d *DefaultReplies) AddRecord(ctx context.Context, cookieID, chatID string) error {
	// err 用于本次流程后续判断的err
	_, err := d.DB.ExecContext(ctx,
		dialectInsertIgnorePrefix(d.Dialect)+` INTO default_reply_records (cookie_id, chat_id) VALUES (?, ?)`+dialectInsertIgnore(d.Dialect, []string{"cookie_id", "chat_id"}),
		cookieID, chatID)
	return err
}

// ItemReplies 指定商品回复操作。
type ItemReplies struct {
	DB      *sql.DB
	Dialect Dialect
}

// Get 取某账号某商品的指定回复。
func (i *ItemReplies) Get(ctx context.Context, cookieID, itemID string) (*ItemReply, error) {
	// ir 用于本次流程后续判断的ir
	var ir ItemReply
	// content 用于本次流程后续判断的内容
	var content sql.NullString
	// err 用于本次流程后续判断的err
	err := i.DB.QueryRowContext(ctx,
		`SELECT item_id, cookie_id, reply_content FROM item_replay WHERE cookie_id=? AND item_id=?`,
		cookieID, itemID).Scan(&ir.ItemID, &ir.CookieID, &content)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ir.ReplyContent = content.String
	return &ir, nil
}
