package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// MarkRead 仅对当前用户拥有账号的非系统入站消息标记已读，并同步归零会话红点。
func (s *ChatStore) MarkRead(ctx context.Context, userID int64, cookieID, chatID string) error {
	// now 是同一批消息和会话状态使用的统一 UTC 时间，避免页面显示先后矛盾。
	now := time.Now().UTC()
	// err 保存批量更新非系统入站消息的错误；失败时不得清空会话红点以避免状态不一致。
	if _, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET read_status=2,read_at=?
		WHERE cookie_id=? AND chat_id=? AND direction='incoming' AND message_type<>'system' AND read_status<>2`,
		now.UnixMilli(), cookieID, chatID); err != nil {
		return err
	}
	// err 保存归零会话红点的错误，该更新通过用户归属子查询阻止越权修改。
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET unread_count=0,updated_at=?
		WHERE cookie_id=? AND chat_id=? AND EXISTS(SELECT 1 FROM cookies c WHERE c.id=chat_sessions.cookie_id AND c.user_id=?)`,
		now.Unix(), cookieID, chatID, userID)
	return err
}

// CountUnreadUserMessages 返回界面红点使用的入站真实用户未读数，系统消息永不计入。
func (s *ChatStore) CountUnreadUserMessages(ctx context.Context, cookieID, chatID string) (int, error) {
	// count 保存符合当前账号、会话及未读条件的消息总数。
	var count int
	// err 保存聚合查询错误，调用方可在平台响应缺失时退回官方红点值。
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages
		WHERE cookie_id=? AND chat_id=? AND direction='incoming' AND message_type<>'system' AND read_status<>2`, cookieID, chatID).Scan(&count)
	return count, err
}

// UpdateMessageStatus 更新消息状态。
func (s *ChatStore) UpdateMessageStatus(ctx context.Context, cookieID, key, status string) (*ChatMessage, error) {
	if // err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET status=? WHERE cookie_id=? AND message_key=?`, status, cookieID, key); err != nil {
		return nil, err
	}
	return s.GetMessageByKey(ctx, cookieID, key)
}

// MarkMessageRead 按平台回执把目标出站消息及同会话中更早的出站消息标记为已读，并返回目标消息。
// 回执若对应本地入站消息则原样返回且不写入，由上层识别为无需处理的跨端状态同步。
func (s *ChatStore) MarkMessageRead(ctx context.Context, cookieID, key string, readAt int64) (*ChatMessage, error) {
	// message 保存平台回执对应的本地消息；只有出站方向的会话和发送时间可界定本次批量确认范围。
	message, err := s.GetMessageByKey(ctx, cookieID, key)
	if err != nil {
		return nil, err
	}
	if message.Direction != "outgoing" {
		return message, nil
	}
	// readAt 保存平台已读时间；缺失时使用本机 UTC 时间作为展示回退。
	if readAt <= 0 {
		readAt = time.Now().UTC().UnixMilli()
	}
	// err 保存按回执水位更新同会话出站历史的错误；对方读到目标消息时更早消息也已被阅读。
	if _, err = s.DB.ExecContext(ctx, `UPDATE chat_messages SET read_status=2,
		read_at=CASE WHEN read_status=2 AND read_at>0 THEN read_at ELSE ? END
		WHERE cookie_id=? AND chat_id=? AND direction='outgoing' AND sent_at<=?`, readAt, cookieID, message.ChatID, message.SentAt); err != nil {
		return nil, err
	}
	return s.GetMessageByKey(ctx, cookieID, key)
}

// MarkLatestOutgoingRead 在回执未带消息键时回退标记会话中最近待确认的出站消息。
func (s *ChatStore) MarkLatestOutgoingRead(ctx context.Context, cookieID, chatID string, readAt int64) (*ChatMessage, error) {
	// readAt 保存平台已读时间；缺失时使用本机 UTC 时间作为展示回退。
	if readAt <= 0 {
		readAt = time.Now().UTC().UnixMilli()
	}
	// key 保存最近一条已发送且未标记已读的消息幂等键。
	var key string
	// err 保存查询错误；没有可更新消息时返回统一 ErrNotFound。
	err := s.DB.QueryRowContext(ctx, `SELECT message_key FROM chat_messages WHERE cookie_id=? AND chat_id=? AND direction='outgoing' AND status='sent' AND read_status<>2 ORDER BY sent_at DESC,id DESC LIMIT 1`, cookieID, chatID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.MarkMessageRead(ctx, cookieID, key, readAt)
}
