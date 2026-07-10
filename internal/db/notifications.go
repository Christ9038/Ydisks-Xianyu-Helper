package db

import (
	"context"
	"database/sql"
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
}

// AccountChannels 取某账号已启用的通知渠道（message_notifications JOIN notification_channels）。
// 移植自 get_account_notifications。
func (n *Notifications) AccountChannels(ctx context.Context, cookieID string) ([]NotificationChannel, error) {
	rows, err := n.DB.QueryContext(ctx,
		`SELECT nc.id, nc.name, nc.type, nc.config,
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
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &c.EventTypes); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
