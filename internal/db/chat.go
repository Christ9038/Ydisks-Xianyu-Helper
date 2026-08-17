package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

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
	ReadStatus  int    `json:"read_status"`
	ReadAt      int64  `json:"read_at,omitempty"`
	SentAt      int64  `json:"sent_at"`
}

type ChatStore struct {
	DB      *sql.DB
	Dialect Dialect
}

func (s *ChatStore) UpsertSession(ctx context.Context, session ChatSession) error {
	now := time.Now().UTC().Unix()
	prefix := dialectInsertIgnorePrefix(s.Dialect)
	query := prefix + ` INTO chat_sessions
		(cookie_id,chat_id,buyer_id,buyer_name,buyer_avatar_url,item_id,item_title,last_message,last_message_at,unread_count,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(s.Dialect, []string{"cookie_id", "chat_id"})
	if _, err := s.DB.ExecContext(ctx, query, session.CookieID, session.ChatID, session.BuyerID, session.BuyerName,
		session.BuyerAvatar, session.ItemID, session.ItemTitle, session.LastMessage, session.LastMessageAt,
		session.UnreadCount, now, now); err != nil {
		return err
	}
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

func (s *ChatStore) DeleteSession(ctx context.Context, cookieID, chatID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM chat_sessions WHERE cookie_id=? AND chat_id=?`, cookieID, chatID)
	return err
}

// DeleteEmptySessions removes conversation shells returned by IM pagination
// with visible=0 and no lastMessage. Older versions persisted these shells as
// "暂无消息", although the official UI never renders them.
func (s *ChatStore) DeleteEmptySessions(ctx context.Context, cookieID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM chat_sessions
		WHERE cookie_id=? AND (last_message='' OR last_message='暂无消息')
		AND NOT EXISTS (SELECT 1 FROM chat_messages m WHERE m.cookie_id=chat_sessions.cookie_id AND m.chat_id=chat_sessions.chat_id)`, cookieID)
	return err
}

// SyncSessionSummary applies the authoritative last-message timestamp from the
// official conversation response. observedModifyAt guards against overwriting
// a genuinely newer live message that arrived after that response was built.
func (s *ChatStore) SyncSessionSummary(ctx context.Context, cookieID, chatID, summary string, sentAt, observedModifyAt int64, unread int) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET last_message=?,last_message_at=?,unread_count=?,updated_at=?
		WHERE cookie_id=? AND chat_id=? AND last_message_at<=?`, summary, sentAt, unread, time.Now().UTC().Unix(),
		cookieID, chatID, observedModifyAt)
	return err
}

func (s *ChatStore) UpdateSessionIdentity(ctx context.Context, cookieID, chatID, buyerID, buyerName, avatarURL string) error {
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
func (s *ChatStore) LatestUnmaskedPeerName(ctx context.Context, cookieID, chatID string) (string, error) {
	var name string
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
	// read_status is also used for incoming messages: only a newly received
	// real-user message starts unread. Imported history and official system
	// notices must never contribute to the chat badge.
	if message.Direction == "incoming" && (!unread || message.MessageType == "system") {
		message.ReadStatus = 2
		message.ReadAt = time.Now().UTC().UnixMilli()
	}
	message.CookieID, message.ChatID = session.CookieID, session.ChatID
	now := time.Now().UTC().Unix()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	// The composite foreign key on chat_messages requires the session to exist
	// first. Insert an empty shell without touching an existing conversation.
	sessionPrefix := dialectInsertIgnorePrefix(s.Dialect)
	sessionInsert := sessionPrefix + ` INTO chat_sessions
		(cookie_id,chat_id,buyer_id,buyer_name,buyer_avatar_url,item_id,item_title,last_message,last_message_at,unread_count,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(s.Dialect, []string{"cookie_id", "chat_id"})
	if _, err := tx.ExecContext(ctx, sessionInsert, session.CookieID, session.ChatID, session.BuyerID,
		session.BuyerName, session.BuyerAvatar, session.ItemID, session.ItemTitle, "", int64(0), 0, now, now); err != nil {
		return nil, false, fmt.Errorf("建立聊天会话: %w", err)
	}

	prefix := dialectInsertIgnorePrefix(s.Dialect)
	query := prefix + ` INTO chat_messages
		(cookie_id,chat_id,message_key,direction,sender_id,sender_name,message_type,content,status,read_status,read_at,sent_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(s.Dialect, []string{"cookie_id", "message_key"})
	res, err := tx.ExecContext(ctx, query, message.CookieID, message.ChatID, message.MessageKey,
		message.Direction, message.SenderID, message.SenderName, message.MessageType, message.Content,
		message.Status, message.ReadStatus, message.ReadAt, message.SentAt, now)
	if err != nil {
		return nil, false, fmt.Errorf("保存聊天消息: %w", err)
	}
	inserted, _ := res.RowsAffected()
	if inserted > 0 {
		unreadDelta := 0
		if unread {
			unreadDelta = 1
		}
		if _, err := tx.ExecContext(ctx, `UPDATE chat_sessions SET buyer_id=?,buyer_name=?,buyer_avatar_url=?,
			item_id=?,item_title=?,last_message=CASE WHEN last_message_at<=? THEN ? ELSE last_message END,
			last_message_at=CASE WHEN last_message_at<=? THEN ? ELSE last_message_at END,
			unread_count=unread_count+?,updated_at=?
			WHERE cookie_id=? AND chat_id=?`, session.BuyerID, session.BuyerName, session.BuyerAvatar,
			session.ItemID, session.ItemTitle, message.SentAt, message.Content, message.SentAt, message.SentAt, unreadDelta, now,
			session.CookieID, session.ChatID); err != nil {
			return nil, false, fmt.Errorf("更新聊天会话: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	stored, err := s.GetMessageByKey(ctx, message.CookieID, message.MessageKey)
	return stored, inserted > 0, err
}

func (s *ChatStore) GetMessageByKey(ctx context.Context, cookieID, key string) (*ChatMessage, error) {
	var m ChatMessage
	err := s.DB.QueryRowContext(ctx, `SELECT id,cookie_id,chat_id,message_key,direction,sender_id,sender_name,message_type,content,status,read_status,read_at,sent_at
		FROM chat_messages WHERE cookie_id=? AND message_key=?`, cookieID, key).Scan(
		&m.ID, &m.CookieID, &m.ChatID, &m.MessageKey, &m.Direction, &m.SenderID, &m.SenderName,
		&m.MessageType, &m.Content, &m.Status, &m.ReadStatus, &m.ReadAt, &m.SentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &m, err
}

// UpdateMessageType refreshes the classification of an already persisted
// message when a later history response exposes richer protocol metadata.
func (s *ChatStore) UpdateMessageType(ctx context.Context, cookieID, key, messageType string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET message_type=?
		WHERE cookie_id=? AND message_key=?`, messageType, cookieID, key)
	return err
}

func (s *ChatStore) ListSessions(ctx context.Context, userID int64, cookieID string, limit int) ([]ChatSession, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT cs.cookie_id,cs.chat_id,cs.buyer_id,cs.buyer_name,cs.buyer_avatar_url,
		cs.item_id,cs.item_title,cs.last_message,cs.last_message_at,cs.unread_count
		FROM chat_sessions cs JOIN cookies c ON c.id=cs.cookie_id
		WHERE c.user_id=? AND cs.cookie_id=? ORDER BY cs.last_message_at DESC LIMIT ?`, userID, cookieID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ChatSession
	for rows.Next() {
		var row ChatSession
		if err := rows.Scan(&row.CookieID, &row.ChatID, &row.BuyerID, &row.BuyerName, &row.BuyerAvatar,
			&row.ItemID, &row.ItemTitle, &row.LastMessage, &row.LastMessageAt, &row.UnreadCount); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *ChatStore) ListMessages(ctx context.Context, userID int64, cookieID, chatID string, beforeID int64, limit int) ([]ChatMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT m.id,m.cookie_id,m.chat_id,m.message_key,m.direction,m.sender_id,m.sender_name,m.message_type,m.content,m.status,m.read_status,m.read_at,m.sent_at
		FROM chat_messages m JOIN cookies c ON c.id=m.cookie_id
		WHERE c.user_id=? AND m.cookie_id=? AND m.chat_id=?`
	args := []any{userID, cookieID, chatID}
	if beforeID > 0 {
		query += ` AND (m.sent_at < COALESCE((SELECT older.sent_at FROM chat_messages older WHERE older.id=? AND older.cookie_id=?), m.sent_at)
			OR (m.sent_at = COALESCE((SELECT same.sent_at FROM chat_messages same WHERE same.id=? AND same.cookie_id=?), m.sent_at) AND m.id<?))`
		args = append(args, beforeID, cookieID, beforeID, cookieID, beforeID)
	}
	query += ` ORDER BY m.sent_at DESC,m.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.CookieID, &m.ChatID, &m.MessageKey, &m.Direction, &m.SenderID,
			&m.SenderName, &m.MessageType, &m.Content, &m.Status, &m.ReadStatus, &m.ReadAt, &m.SentAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	// API returns chronological order while the query remains index-friendly.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, rows.Err()
}

func (s *ChatStore) MarkRead(ctx context.Context, userID int64, cookieID, chatID string) error {
	now := time.Now().UTC()
	if _, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET read_status=2,read_at=?
		WHERE cookie_id=? AND chat_id=? AND direction='incoming' AND message_type<>'system' AND read_status<>2`,
		now.UnixMilli(), cookieID, chatID); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET unread_count=0,updated_at=?
		WHERE cookie_id=? AND chat_id=? AND EXISTS(SELECT 1 FROM cookies c WHERE c.id=chat_sessions.cookie_id AND c.user_id=?)`,
		now.Unix(), cookieID, chatID, userID)
	return err
}

// CountUnreadUserMessages returns the message-level unread count used by the
// UI badge. It deliberately excludes official/system messages and relies on
// read_status rather than the conversation summary counter.
func (s *ChatStore) CountUnreadUserMessages(ctx context.Context, cookieID, chatID string) (int, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_messages
		WHERE cookie_id=? AND chat_id=? AND direction='incoming' AND message_type<>'system' AND read_status<>2`, cookieID, chatID).Scan(&count)
	return count, err
}

func (s *ChatStore) UpdateMessageStatus(ctx context.Context, cookieID, key, status string) (*ChatMessage, error) {
	if _, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET status=? WHERE cookie_id=? AND message_key=?`, status, cookieID, key); err != nil {
		return nil, err
	}
	return s.GetMessageByKey(ctx, cookieID, key)
}

func (s *ChatStore) MarkMessageRead(ctx context.Context, cookieID, key string, readAt int64) (*ChatMessage, error) {
	if readAt <= 0 {
		readAt = time.Now().UTC().UnixMilli()
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET read_status=2,read_at=? WHERE cookie_id=? AND message_key=? AND direction='outgoing'`, readAt, cookieID, key); err != nil {
		return nil, err
	}
	return s.GetMessageByKey(ctx, cookieID, key)
}

func (s *ChatStore) MarkLatestOutgoingRead(ctx context.Context, cookieID, chatID string, readAt int64) (*ChatMessage, error) {
	if readAt <= 0 {
		readAt = time.Now().UTC().UnixMilli()
	}
	var key string
	err := s.DB.QueryRowContext(ctx, `SELECT message_key FROM chat_messages WHERE cookie_id=? AND chat_id=? AND direction='outgoing' AND status='sent' AND read_status<>2 ORDER BY sent_at DESC,id DESC LIMIT 1`, cookieID, chatID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.MarkMessageRead(ctx, cookieID, key, readAt)
}
