package db

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// NotificationChannel 通知渠道（含配置 JSON）。
type NotificationChannel struct {
	ID         int64
	Name       string
	Type       string
	Config     string // JSON
	EventTypes string // JSON array or comma-separated event codes
}

// Notifications 通知绑定操作。
type Notifications struct {
	DB      *sql.DB
	Dialect Dialect
	codec   *secretCodec
}

type NotificationOutboxInput struct {
	ChannelID int64
	EventType string
	Body      string
}

type NotificationOutboxMessage struct {
	ID           int64
	ChannelID    int64
	EventType    string
	Body         string
	AttemptCount int
}

// AccountChannels 取某账号已启用的通知渠道（message_notifications JOIN notification_channels）。
// 移植自 get_account_notifications。
func (n *Notifications) AccountChannels(ctx context.Context, cookieID string) ([]NotificationChannel, error) {
	rows, err := n.DB.QueryContext(ctx,
		`SELECT nc.id, nc.name, nc.type, nc.config, COALESCE(nc.user_id,1),
		        COALESCE(NULLIF(mn.event_types,''), nc.event_types, '')
		 FROM message_notifications mn
		 JOIN cookies c ON c.id=mn.cookie_id
		 JOIN notification_channels nc ON mn.channel_id = nc.id AND nc.user_id=c.user_id
		 WHERE mn.cookie_id=? AND mn.enabled=1 AND nc.enabled=1
		 ORDER BY mn.id`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotificationChannel
	for rows.Next() {
		var c NotificationChannel
		var userID int64
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &userID, &c.EventTypes); err != nil {
			return nil, err
		}
		c.Config, err = n.codec.decrypt("notification-config", strconv.FormatInt(userID, 10), c.Config)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// EnqueueOutbox 在一个事务中持久化同一事件的各渠道投递，避免进程退出造成部分丢失。
func (n *Notifications) EnqueueOutbox(ctx context.Context, messages []NotificationOutboxInput) error {
	if len(messages) == 0 {
		return nil
	}
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, message := range messages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO notification_outbox
			(channel_id,event_type,body,status,attempt_count,next_attempt_at,lease_expires_at,worker_token,last_error)
			VALUES (?,?,?,'pending',0,0,0,'','')`,
			message.ChannelID, message.EventType, message.Body); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClaimOutbox 原子领取到期投递。过期 running 任务可以被重新接管，worker token
// 用于隔离迟到的旧 worker。
func (n *Notifications) ClaimOutbox(ctx context.Context, workerToken string, now time.Time, limit int) ([]NotificationOutboxMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	nowUnix := now.Unix()
	rows, err := n.DB.QueryContext(ctx, `SELECT id,channel_id,event_type,body,attempt_count
		FROM notification_outbox
		WHERE (status='pending' AND next_attempt_at<=?) OR (status='running' AND lease_expires_at<?)
		ORDER BY id LIMIT ?`, nowUnix, nowUnix, limit)
	if err != nil {
		return nil, err
	}
	var candidates []NotificationOutboxMessage
	for rows.Next() {
		var message NotificationOutboxMessage
		if err := rows.Scan(&message.ID, &message.ChannelID, &message.EventType, &message.Body, &message.AttemptCount); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, message)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	claimed := candidates[:0]
	leaseExpiresAt := now.Add(30 * time.Second).Unix()
	for _, message := range candidates {
		res, err := n.DB.ExecContext(ctx, `UPDATE notification_outbox
			SET status='running',worker_token=?,lease_expires_at=?,attempt_count=attempt_count+1,updated_at=CURRENT_TIMESTAMP
			WHERE id=? AND ((status='pending' AND next_attempt_at<=?) OR (status='running' AND lease_expires_at<?))`,
			workerToken, leaseExpiresAt, message.ID, nowUnix, nowUnix)
		if err != nil {
			return nil, err
		}
		count, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		if count == 1 {
			message.AttemptCount++
			claimed = append(claimed, message)
		}
	}
	return claimed, nil
}

func (n *Notifications) CompleteOutbox(ctx context.Context, id int64, workerToken string) (bool, error) {
	res, err := n.DB.ExecContext(ctx, `DELETE FROM notification_outbox WHERE id=? AND status='running' AND worker_token=?`, id, workerToken)
	if err != nil {
		return false, err
	}
	count, err := res.RowsAffected()
	return err == nil && count == 1, err
}

func (n *Notifications) RetryOutbox(ctx context.Context, id int64, workerToken, message string, nextAttemptAt int64, permanent bool) (bool, error) {
	status := "pending"
	if permanent {
		status = "dead"
	}
	res, err := n.DB.ExecContext(ctx, `UPDATE notification_outbox
		SET status=?,next_attempt_at=?,lease_expires_at=0,worker_token='',last_error=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, status, nextAttemptAt, message, id, workerToken)
	if err != nil {
		return false, err
	}
	count, err := res.RowsAffected()
	return err == nil && count == 1, err
}
