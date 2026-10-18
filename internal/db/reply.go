package db

import (
	"context"
	"database/sql"
	"encoding/json"
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
	// Enabled 控制账号默认回复，不影响商品专属配置。
	Enabled bool
	// ReplyContent 是默认回复正文。
	ReplyContent string
	// ReplyImageURL 是兼容网络图片来源。
	ReplyImageURL string
	// ReplyImagePath 是账号专用图片目录内的相对文件引用。
	ReplyImagePath string
	// ReplyOnce 保持账号加会话的一次性投递语义。
	ReplyOnce bool
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
	// ReplyImagePath 是账号专用图片目录内的相对文件引用。
	ReplyImagePath string
	// ReplyOnce 表示同一聊天是否只发送一次。
	ReplyOnce bool
}

// DefaultReplyRecord 记录 reply_once 消息各部分的投递状态。
type DefaultReplyRecord struct {
	// Status 表示领取、失败、未知或已完成状态，控制是否允许自动重放。
	Status string
	// TextSent 表示文字已确认发送，失败恢复时不能再发。
	TextSent bool
	// ImageSent 表示图片已确认发送，失败恢复时不能再发。
	ImageSent bool
}

// uncertainReplyErrorPrefix 标记未知投递结果的降级隔离记录，阻止租约到期后自动重发。
const uncertainReplyErrorPrefix = "uncertain:"

// defaultReplyStatusSending 表示已经领取且即将调用外部发送接口的持久化状态。
// 该状态的租约过期后也不能自动接管，避免数据库故障时重复发送一次性消息。
const defaultReplyStatusSending = "sending"

// ItemReply 对应 item_replay 表（指定商品回复）。
type ItemReply struct {
	// ReplyOnce 仅对同一账号、商品、会话去重，默认关闭且不继承账号兜底开关。
	ReplyOnce bool
	// ItemID 是配置所属商品标识。
	ItemID string
	// CookieID 是配置所属账号标识。
	CookieID string
	// ReplyContent 是商品默认回复正文。
	ReplyContent string
	// ReplyImageURL 是商品默认回复的网络图片来源。
	ReplyImageURL string
	// ReplyImagePath 是账号专用目录内的本地图片相对路径。
	ReplyImagePath string
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

// Get 读取 cookieID 的默认回复配置；未配置时返回 ErrNotFound。
func (d *DefaultReplies) Get(ctx context.Context, cookieID string) (*DefaultReply, error) {
	return readDefaultReply(ctx, d.DB, cookieID)
}

// Upsert 完整覆盖 cookieID 的默认回复配置 reply，与兼容补丁共享账号行锁。
func (d *DefaultReplies) Upsert(ctx context.Context, cookieID string, reply DefaultReply) error {
	return d.UpdateWithCurrent(ctx, cookieID, func(DefaultReply) (DefaultReply, error) {
		return reply, nil
	})
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
		SELECT dr.cookie_id, dr.enabled, COALESCE(dr.reply_content,''), dr.reply_once, COALESCE(dr.reply_image_url,''), COALESCE(dr.reply_image_path,'')
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
		err := rows.Scan(&item.CookieID, &enabled, &item.ReplyContent, &replyOnce, &item.ReplyImageURL, &item.ReplyImagePath); err != nil {
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

// ClearRecords 在 ctx 下只清空 cookieID 的账号兜底记录，商品作用域不受影响；返回数据库错误。
func (d *DefaultReplies) ClearRecords(ctx context.Context, cookieID string) error {
	return d.RecordsForItem("").ClearRecords(ctx, cookieID)
}

// HasRecord 查询 ctx 下 cookieID/chatID 的账号兜底是否完成；保持旧接口读取失败返回 false 的兼容行为。
func (d *DefaultReplies) HasRecord(ctx context.Context, cookieID, chatID string) bool {
	return d.RecordsForItem("").HasRecord(ctx, cookieID, chatID)
}

// ClaimRecord 为 ctx 下 cookieID/chatID 领取账号兜底；needsText/needsImage 定义待发分段，返回快照、领取权和错误。
func (d *DefaultReplies) ClaimRecord(ctx context.Context, cookieID, chatID string, needsText, needsImage bool) (DefaultReplyRecord, bool, error) {
	return d.RecordsForItem("").ClaimRecord(ctx, cookieID, chatID, needsText, needsImage)
}

// Record 返回 ctx 下 cookieID/chatID 的账号兜底分段状态，不存在时保留 sql.ErrNoRows。
func (d *DefaultReplies) Record(ctx context.Context, cookieID, chatID string) (DefaultReplyRecord, error) {
	return d.RecordsForItem("").Record(ctx, cookieID, chatID)
}

// MarkPartSent 在 ctx 下标记 cookieID/chatID 的账号兜底 part 分段，part 只接受 text/image；无效输入返回错误。
func (d *DefaultReplies) MarkPartSent(ctx context.Context, cookieID, chatID, part string) error {
	return d.RecordsForItem("").MarkPartSent(ctx, cookieID, chatID, part)
}

// MarkRecordFailed 用 message 记录 ctx 下 cookieID/chatID 的账号兜底确定失败，不会修改商品记录。
func (d *DefaultReplies) MarkRecordFailed(ctx context.Context, cookieID, chatID, message string) error {
	return d.RecordsForItem("").MarkRecordFailed(ctx, cookieID, chatID, message)
}

// MarkRecordUncertain 用 message 隔离 ctx 下 cookieID/chatID 的账号兜底未知结果，禁止自动重放。
func (d *DefaultReplies) MarkRecordUncertain(ctx context.Context, cookieID, chatID, message string) error {
	return d.RecordsForItem("").MarkRecordUncertain(ctx, cookieID, chatID, message)
}

// MarkRecordSent 将 ctx 下 cookieID/chatID 的账号兜底标记为全部完成，数据库失败原样返回。
func (d *DefaultReplies) MarkRecordSent(ctx context.Context, cookieID, chatID string) error {
	return d.RecordsForItem("").MarkRecordSent(ctx, cookieID, chatID)
}

// AddRecord 在 ctx 下幂等写入 cookieID/chatID 的账号兜底完成记录，保留旧调用方接口。
func (d *DefaultReplies) AddRecord(ctx context.Context, cookieID, chatID string) error {
	return d.RecordsForItem("").AddRecord(ctx, cookieID, chatID)
}

// ItemReplies 指定商品回复操作。
type ItemReplies struct {
	DB      *sql.DB
	Dialect Dialect
}

// Get 读取 cookieID 下 itemID 的商品图文默认回复，未配置返回 ErrNotFound。
func (i *ItemReplies) Get(ctx context.Context, cookieID, itemID string) (*ItemReply, error) {
	return readItemReply(ctx, i.DB, cookieID, itemID)
}
