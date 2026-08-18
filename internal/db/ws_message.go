package db

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// WSMessage 原始 WS 收包记录。
type WSMessage struct {
	CookieID    string
	Direction   string
	RawText     string
	ParsedJSON  string
	MessageKind string
	ParseStatus string
	Error       string
}

// WSMessageStore 保存 WS 消息。
type WSMessageStore struct{ DB *sql.DB }

// Add 记录一条 WS 消息。
func (w *WSMessageStore) Add(ctx context.Context, m WSMessage) error {
	return w.AddBatch(ctx, []WSMessage{m})
}

// AddBatch 在一次数据库操作中记录多条 WS 消息。
func (w *WSMessageStore) AddBatch(ctx context.Context, messages []WSMessage) error {
	if len(messages) == 0 {
		return nil
	}

	// query 拼接批量写入 WS 诊断帧的占位符 SQL，避免逐条提交造成额外事务开销。
	var query strings.Builder
	query.WriteString("INSERT INTO ws_messages (cookie_id, direction, raw_text, parsed_json, message_kind, parse_status, error, created_at) VALUES ")
	// args 按列顺序保存每条诊断帧的参数，最终一次性绑定到批量 INSERT。
	args := make([]any, 0, len(messages)*7)
	// i、message 表示当前遍历过程中的i、message
	for i, message := range messages {
		if i > 0 {
			query.WriteByte(',')
		}
		query.WriteString("(?,?,?,?,?,?,?,CURRENT_TIMESTAMP)")
		if message.Direction == "" {
			message.Direction = "in"
		}
		if message.ParseStatus == "" {
			message.ParseStatus = "raw"
		}
		args = append(args, message.CookieID, message.Direction, message.RawText, message.ParsedJSON,
			message.MessageKind, message.ParseStatus, message.Error)
	}

	// err 表示批量写入 WS 诊断帧的数据库错误。
	_, err := w.DB.ExecContext(ctx, query.String(), args...)
	return err
}

// DeleteBefore 删除指定账号在 cutoff 之前的 WS 诊断消息。
func (w *WSMessageStore) DeleteBefore(ctx context.Context, cookieID string, cutoff time.Time) (int64, error) {
	// result 提供删除行数；err 表示清理历史诊断帧时的数据库错误。
	result, err := w.DB.ExecContext(ctx,
		"DELETE FROM ws_messages WHERE cookie_id=? AND created_at < ?", cookieID, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// FindInboundParsedJSONContaining 返回最近的已解密入站诊断帧，供旧版聊天消息标识迁移使用。
func (w *WSMessageStore) FindInboundParsedJSONContaining(ctx context.Context, cookieID, fragment string, limit int) ([]string, error) {
	// safeLimit 限制诊断查询行数，避免单次已读操作读取过多历史帧。
	safeLimit := limit
	if safeLimit <= 0 || safeLimit > 100 {
		safeLimit = 20
	}
	// rows、err 保存匹配的诊断帧游标及数据库查询错误。
	rows, err := w.DB.QueryContext(ctx, `SELECT parsed_json FROM ws_messages
		WHERE cookie_id=? AND direction='in' AND parse_status='decrypted' AND parsed_json LIKE ?
		ORDER BY id DESC LIMIT ?`, cookieID, "%"+fragment+"%", safeLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// values 保存按时间倒序读取的已解密诊断 JSON。
	values := make([]string, 0, safeLimit)
	for rows.Next() {
		// value 保存当前诊断帧的解析 JSON。
		var value string
		// scanErr 保存当前诊断帧字段扫描错误。
		if scanErr := rows.Scan(&value); scanErr != nil {
			return nil, scanErr
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
