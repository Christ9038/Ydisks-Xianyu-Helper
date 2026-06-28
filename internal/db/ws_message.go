package db

import (
	"context"
	"database/sql"
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
	if m.Direction == "" {
		m.Direction = "in"
	}
	if m.ParseStatus == "" {
		m.ParseStatus = "raw"
	}
	_, err := w.DB.ExecContext(ctx,
		"INSERT INTO ws_messages (cookie_id, direction, raw_text, parsed_json, message_kind, parse_status, error, created_at) VALUES (?,?,?,?,?,?,?,CURRENT_TIMESTAMP)",
		m.CookieID, m.Direction, m.RawText, m.ParsedJSON, m.MessageKind, m.ParseStatus, m.Error)
	return err
}
