package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ChatSession 保存聊天会话，供当前处理流程使用
type ChatSession struct {
	CookieID      string `json:"account_id"`
	ChatID        string `json:"chat_id"`
	BuyerID       string `json:"buyer_id"`
	BuyerName     string `json:"buyer_name"`
	BuyerAvatar   string `json:"buyer_avatar_url"`
	ItemID        string `json:"item_id"`
	ItemTitle     string `json:"item_title"`
	LastMessage   string `json:"last_message"`
	LastMessageAt int64  `json:"last_message_at"`
	UnreadCount   int    `json:"unread_count"`
}

// ChatMessage 保存聊天消息，供当前处理流程使用
type ChatMessage struct {
	ID          int64  `json:"id"`
	CookieID    string `json:"account_id"`
	ChatID      string `json:"chat_id"`
	MessageKey  string `json:"message_key"`
	Direction   string `json:"direction"`
	SenderID    string `json:"sender_id"`
	SenderName  string `json:"sender_name"`
	MessageType string `json:"message_type"`
	Content     string `json:"content"`
	Status      string `json:"status"`
	SentAt      int64  `json:"sent_at"`
}

// ChatStore 保存聊天Store，供当前处理流程使用
type ChatStore struct {
	DB      *sql.DB
	Dialect Dialect
}

// UpsertSession 负责Upsert会话相关处理。
func (s *ChatStore) UpsertSession(ctx context.Context, session ChatSession) error {
	// now 保存now，供当前处理流程使用
	now := time.Now().UTC().Unix()
	// prefix 保存prefix，供当前处理流程使用
	prefix := dialectInsertIgnorePrefix(s.Dialect)
	// query 保存查询，供当前处理流程使用
	query := prefix + ` INTO chat_sessions
		(cookie_id,chat_id,buyer_id,buyer_name,buyer_avatar_url,item_id,item_title,last_message,last_message_at,unread_count,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(s.Dialect, []string{"cookie_id", "chat_id"})
	if // err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, query, session.CookieID, session.ChatID, session.BuyerID, session.BuyerName,
		session.BuyerAvatar, session.ItemID, session.ItemTitle, session.LastMessage, session.LastMessageAt,
		session.UnreadCount, now, now); err != nil {
		return err
	}
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET
		buyer_id=CASE WHEN ?<>'' THEN ? ELSE buyer_id END,
		buyer_name=CASE WHEN ?<>'' THEN ? ELSE buyer_name END,
		buyer_avatar_url=CASE WHEN ?<>'' THEN ? ELSE buyer_avatar_url END,
		item_id=CASE WHEN ?<>'' THEN ? ELSE item_id END,
		item_title=CASE WHEN ?<>'' THEN ? ELSE item_title END,
		last_message=CASE WHEN last_message_at<=? THEN ? ELSE last_message END,
		last_message_at=CASE WHEN last_message_at<=? THEN ? ELSE last_message_at END,
		unread_count=CASE WHEN ?>unread_count THEN ? ELSE unread_count END,updated_at=?
		WHERE cookie_id=? AND chat_id=?`, session.BuyerID, session.BuyerID, session.BuyerName, session.BuyerName,
		session.BuyerAvatar, session.BuyerAvatar, session.ItemID, session.ItemID, session.ItemTitle, session.ItemTitle,
		session.LastMessageAt, session.LastMessage, session.LastMessageAt, session.LastMessageAt,
		session.UnreadCount, session.UnreadCount, now, session.CookieID, session.ChatID)
	return err
}

// DeleteSession 删除会话。
func (s *ChatStore) DeleteSession(ctx context.Context, cookieID, chatID string) error {
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `DELETE FROM chat_sessions WHERE cookie_id=? AND chat_id=?`, cookieID, chatID)
	return err
}

// DeleteEmptySessions removes conversation shells returned by IM pagination
// with visible=0 and no lastMessage. Older versions persisted these shells as
// "暂无消息", although the official UI never renders them.
// DeleteEmptySessions 删除EmptySessions。
func (s *ChatStore) DeleteEmptySessions(ctx context.Context, cookieID string) error {
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `DELETE FROM chat_sessions
		WHERE cookie_id=? AND (last_message='' OR last_message='暂无消息')
		AND NOT EXISTS (SELECT 1 FROM chat_messages m WHERE m.cookie_id=chat_sessions.cookie_id AND m.chat_id=chat_sessions.chat_id)`, cookieID)
	return err
}

// SyncSessionSummary applies the authoritative last-message timestamp from the
// official conversation response. observedModifyAt guards against overwriting
// a genuinely newer live message that arrived after that response was built.
// SyncSessionSummary 同步会话Summary。
func (s *ChatStore) SyncSessionSummary(ctx context.Context, cookieID, chatID, summary string, sentAt, observedModifyAt int64, unread int) error {
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET last_message=?,last_message_at=?,unread_count=?,updated_at=?
		WHERE cookie_id=? AND chat_id=? AND last_message_at<=?`, summary, sentAt, unread, time.Now().UTC().Unix(),
		cookieID, chatID, observedModifyAt)
	return err
}

// UpdateSessionIdentity 更新会话Identity。
func (s *ChatStore) UpdateSessionIdentity(ctx context.Context, cookieID, chatID, buyerID, buyerName, avatarURL string) error {
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET
		buyer_id=CASE WHEN ?<>'' THEN ? ELSE buyer_id END,
		buyer_name=CASE WHEN ?<>'' THEN ? ELSE buyer_name END,
		buyer_avatar_url=CASE WHEN ?<>'' THEN ? ELSE buyer_avatar_url END,
		updated_at=? WHERE cookie_id=? AND chat_id=?`, buyerID, buyerID, buyerName, buyerName,
		avatarURL, avatarURL, time.Now().UTC().Unix(), cookieID, chatID)
	return err
}

// LatestUnmaskedPeerName recovers the most recent real nickname observed in
// message history. Conversation summaries and profile APIs may return masked
// names such as x***3, while older message extensions still contain the nick.
// LatestUnmaskedPeerName 负责LatestUnmaskedPeer名称相关处理。
func (s *ChatStore) LatestUnmaskedPeerName(ctx context.Context, cookieID, chatID string) (string, error) {
	// name 保存名称，供当前处理流程使用
	var name string
	// err 保存err，供当前处理流程使用
	err := s.DB.QueryRowContext(ctx, `SELECT sender_name FROM chat_messages
		WHERE cookie_id=? AND chat_id=? AND direction='incoming' AND sender_name<>'' AND sender_name NOT LIKE '%***%'
			AND message_type<>'system'
			AND sender_name<>content AND sender_name NOT IN ('交易消息','系统消息','卡片消息','我完成了评价','对方完成了评价',
			'快给ta一个评价吧～','卖家已发货','买家已付款','买家已确认收货','等待您发货','超时未付款，系统关闭了订单','邀您填写售后问卷')
		ORDER BY sent_at DESC,id DESC LIMIT 1`, cookieID, chatID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return strings.TrimSpace(name), err
}

// SaveMessage inserts a message idempotently and updates its conversation only
// when the message was new. This keeps retries from inflating unread counters.
// SaveMessage 保存消息。
func (s *ChatStore) SaveMessage(ctx context.Context, session ChatSession, message ChatMessage, unread bool) (*ChatMessage, bool, error) {
	if s == nil || s.DB == nil {
		return nil, false, errors.New("聊天存储未初始化")
	}
	session.CookieID = strings.TrimSpace(session.CookieID)
	session.ChatID = strings.TrimSpace(session.ChatID)
	message.MessageKey = strings.TrimSpace(message.MessageKey)
	if session.CookieID == "" || session.ChatID == "" || message.MessageKey == "" {
		return nil, false, errors.New("聊天消息缺少账号、会话或消息键")
	}
	if message.SentAt <= 0 {
		message.SentAt = time.Now().UTC().UnixMilli()
	}
	message.CookieID, message.ChatID = session.CookieID, session.ChatID
	// now 保存now，供当前处理流程使用
	now := time.Now().UTC().Unix()
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	// The composite foreign key on chat_messages requires the session to exist
	// first. Insert an empty shell without touching an existing conversation.
	// sessionPrefix 保存会话Prefix，供当前处理流程使用
	sessionPrefix := dialectInsertIgnorePrefix(s.Dialect)
	// sessionInsert 保存会话Insert，供当前处理流程使用
	sessionInsert := sessionPrefix + ` INTO chat_sessions
		(cookie_id,chat_id,buyer_id,buyer_name,buyer_avatar_url,item_id,item_title,last_message,last_message_at,unread_count,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(s.Dialect, []string{"cookie_id", "chat_id"})
	if // err 保存err，供当前处理流程使用
	_, err := tx.ExecContext(ctx, sessionInsert, session.CookieID, session.ChatID, session.BuyerID,
		session.BuyerName, session.BuyerAvatar, session.ItemID, session.ItemTitle, "", int64(0), 0, now, now); err != nil {
		return nil, false, fmt.Errorf("建立聊天会话: %w", err)
	}

	// prefix 保存prefix，供当前处理流程使用
	prefix := dialectInsertIgnorePrefix(s.Dialect)
	// query 保存查询，供当前处理流程使用
	query := prefix + ` INTO chat_messages
		(cookie_id,chat_id,message_key,direction,sender_id,sender_name,message_type,content,status,sent_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(s.Dialect, []string{"cookie_id", "message_key"})
	// res、err 保存res、err，供当前处理流程使用
	res, err := tx.ExecContext(ctx, query, message.CookieID, message.ChatID, message.MessageKey,
		message.Direction, message.SenderID, message.SenderName, message.MessageType, message.Content,
		message.Status, message.SentAt, now)
	if err != nil {
		return nil, false, fmt.Errorf("保存聊天消息: %w", err)
	}
	// inserted 保存inserted，供当前处理流程使用
	inserted, _ := res.RowsAffected()
	if inserted > 0 {
		// unreadDelta 保存unreadDelta，供当前处理流程使用
		unreadDelta := 0
		if unread {
			unreadDelta = 1
		}
		if // err 保存err，供当前处理流程使用
		_, err := tx.ExecContext(ctx, `UPDATE chat_sessions SET buyer_id=?,buyer_name=?,buyer_avatar_url=?,
			item_id=?,item_title=?,last_message=CASE WHEN last_message_at<=? THEN ? ELSE last_message END,
			last_message_at=CASE WHEN last_message_at<=? THEN ? ELSE last_message_at END,
			unread_count=unread_count+?,updated_at=?
			WHERE cookie_id=? AND chat_id=?`, session.BuyerID, session.BuyerName, session.BuyerAvatar,
			session.ItemID, session.ItemTitle, message.SentAt, message.Content, message.SentAt, message.SentAt, unreadDelta, now,
			session.CookieID, session.ChatID); err != nil {
			return nil, false, fmt.Errorf("更新聊天会话: %w", err)
		}
	}
	if // err 保存err，供当前处理流程使用
	err := tx.Commit(); err != nil {
		return nil, false, err
	}
	// stored、err 保存stored、err，供当前处理流程使用
	stored, err := s.GetMessageByKey(ctx, message.CookieID, message.MessageKey)
	return stored, inserted > 0, err
}

// GetMessageByKey 读取消息ByKey。
func (s *ChatStore) GetMessageByKey(ctx context.Context, cookieID, key string) (*ChatMessage, error) {
	// m 保存m，供当前处理流程使用
	var m ChatMessage
	// err 保存err，供当前处理流程使用
	err := s.DB.QueryRowContext(ctx, `SELECT id,cookie_id,chat_id,message_key,direction,sender_id,sender_name,message_type,content,status,sent_at
		FROM chat_messages WHERE cookie_id=? AND message_key=?`, cookieID, key).Scan(
		&m.ID, &m.CookieID, &m.ChatID, &m.MessageKey, &m.Direction, &m.SenderID, &m.SenderName,
		&m.MessageType, &m.Content, &m.Status, &m.SentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &m, err
}

// UpdateMessageType refreshes the classification of an already persisted
// message when a later history response exposes richer protocol metadata.
// UpdateMessageType 更新消息类型。
func (s *ChatStore) UpdateMessageType(ctx context.Context, cookieID, key, messageType string) error {
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET message_type=?
		WHERE cookie_id=? AND message_key=?`, messageType, cookieID, key)
	return err
}

// ListSessions 读取Sessions。
func (s *ChatStore) ListSessions(ctx context.Context, userID int64, cookieID string, limit int) ([]ChatSession, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.DB.QueryContext(ctx, `SELECT cs.cookie_id,cs.chat_id,cs.buyer_id,cs.buyer_name,cs.buyer_avatar_url,
		cs.item_id,cs.item_title,cs.last_message,cs.last_message_at,cs.unread_count
		FROM chat_sessions cs JOIN cookies c ON c.id=cs.cookie_id
		WHERE c.user_id=? AND cs.cookie_id=? ORDER BY cs.last_message_at DESC LIMIT ?`, userID, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 保存结果，供当前处理流程使用
	var result []ChatSession
	for rows.Next() {
		// row 保存row，供当前处理流程使用
		var row ChatSession
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&row.CookieID, &row.ChatID, &row.BuyerID, &row.BuyerName, &row.BuyerAvatar,
			&row.ItemID, &row.ItemTitle, &row.LastMessage, &row.LastMessageAt, &row.UnreadCount); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// ListMessages 读取消息列表。
func (s *ChatStore) ListMessages(ctx context.Context, userID int64, cookieID, chatID string, beforeID int64, limit int) ([]ChatMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// query 保存查询，供当前处理流程使用
	query := `SELECT m.id,m.cookie_id,m.chat_id,m.message_key,m.direction,m.sender_id,m.sender_name,m.message_type,m.content,m.status,m.sent_at
		FROM chat_messages m JOIN cookies c ON c.id=m.cookie_id
		WHERE c.user_id=? AND m.cookie_id=? AND m.chat_id=?`
	// args 保存args，供当前处理流程使用
	args := []any{userID, cookieID, chatID}
	if beforeID > 0 {
		query += ` AND (m.sent_at < COALESCE((SELECT older.sent_at FROM chat_messages older WHERE older.id=? AND older.cookie_id=?), m.sent_at)
			OR (m.sent_at = COALESCE((SELECT same.sent_at FROM chat_messages same WHERE same.id=? AND same.cookie_id=?), m.sent_at) AND m.id<?))`
		args = append(args, beforeID, cookieID, beforeID, cookieID, beforeID)
	}
	query += ` ORDER BY m.sent_at DESC,m.id DESC LIMIT ?`
	args = append(args, limit)
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 保存结果，供当前处理流程使用
	var result []ChatMessage
	for rows.Next() {
		// m 保存m，供当前处理流程使用
		var m ChatMessage
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&m.ID, &m.CookieID, &m.ChatID, &m.MessageKey, &m.Direction, &m.SenderID,
			&m.SenderName, &m.MessageType, &m.Content, &m.Status, &m.SentAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	// API returns chronological order while the query remains index-friendly.
	for // i、j 保存i、j，供当前处理流程使用
	i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, rows.Err()
}

// MarkRead 负责MarkRead相关处理。
func (s *ChatStore) MarkRead(ctx context.Context, userID int64, cookieID, chatID string) error {
	// err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET unread_count=0,updated_at=?
		WHERE cookie_id=? AND chat_id=? AND EXISTS(SELECT 1 FROM cookies c WHERE c.id=chat_sessions.cookie_id AND c.user_id=?)`,
		time.Now().UTC().Unix(), cookieID, chatID, userID)
	return err
}

// UpdateMessageStatus 更新消息状态。
func (s *ChatStore) UpdateMessageStatus(ctx context.Context, cookieID, key, status string) (*ChatMessage, error) {
	if // err 保存err，供当前处理流程使用
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET status=? WHERE cookie_id=? AND message_key=?`, status, cookieID, key); err != nil {
		return nil, err
	}
	return s.GetMessageByKey(ctx, cookieID, key)
}
