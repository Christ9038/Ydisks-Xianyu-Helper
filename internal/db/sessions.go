package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crypto/rand"
	"encoding/base64"
)

// SessionTTL 是 24 小时的会话有效期。
const SessionTTL = 24 * time.Hour

// Sessions 会话表操作（HttpOnly Cookie 会话）。
type Sessions struct {
	DB *sql.DB
}

// Create 为已认证用户创建会话，返回 session_id。
func (s *Sessions) Create(ctx context.Context, u *User) (string, error) {
	// sessionID、err 用于本次流程后续判断的会话ID、err
	sessionID, err := randomSessionID()
	if err != nil {
		return "", fmt.Errorf("生成 session id: %w", err)
	}
	// now 用于本次流程后续判断的now
	now := time.Now().Unix()
	// expires 用于本次流程后续判断的expires
	expires := now + int64(SessionTTL.Seconds())
	// isAdmin 用于本次流程后续判断的isAdmin
	isAdmin := 0
	if u.IsAdmin {
		isAdmin = 1
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO sessions (session_id, user_id, username, is_admin, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, u.ID, u.Username, isAdmin, expires, now)
	if err != nil {
		return "", fmt.Errorf("写入 session: %w", err)
	}
	return sessionID, nil
}

// Get 取会话；过期或不存在返回 ErrNotFound，并清理过期记录。
func (s *Sessions) Get(ctx context.Context, sessionID string) (*Session, error) {
	if sessionID == "" {
		return nil, ErrNotFound
	}
	// sess 用于本次流程后续判断的sess
	var sess Session
	// isAdmin 用于本次流程后续判断的isAdmin
	var isAdmin int
	// err 用于本次流程后续判断的err
	err := s.DB.QueryRowContext(ctx,
		`SELECT s.session_id, s.user_id, u.username, u.is_admin, s.expires_at
		   FROM sessions s
		   JOIN users u ON u.id=s.user_id
		  WHERE s.session_id=? AND u.is_active=1`,
		sessionID).Scan(&sess.SessionID, &sess.UserID, &sess.Username, &isAdmin, &sess.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sess.IsAdmin = isAdmin != 0
	if sess.ExpiresAt <= time.Now().Unix() {
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE session_id=?`, sessionID)
		return nil, ErrNotFound
	}
	return &sess, nil
}

// Delete 删除会话（登出）。
func (s *Sessions) Delete(ctx context.Context, sessionID string) error {
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE session_id=?`, sessionID)
	return err
}

// DeleteExpired 清理所有过期会话（可由定时任务调用）。
func (s *Sessions) DeleteExpired(ctx context.Context) (int64, error) {
	// res、err 用于本次流程后续判断的res、err
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	// n 用于本次流程后续判断的n
	n, _ := res.RowsAffected()
	return n, nil
}

// randomSessionID 生成 URL 安全的随机会话 ID。
func randomSessionID() (string, error) {
	// b 用于本次流程后续判断的b
	b := make([]byte, 32)
	if // err 用于本次流程后续判断的err
	_, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
