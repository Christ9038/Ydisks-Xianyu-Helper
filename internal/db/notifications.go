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

// OwnsChannel 判断通知渠道是否归属于指定用户。
func (n *Notifications) OwnsChannel(ctx context.Context, channelID, userID int64) (bool, error) {
	// exists 保存exists，供当前处理流程使用
	var exists bool
	// err 保存err，供当前处理流程使用
	err := n.DB.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM notification_channels WHERE id=? AND user_id=?)`,
		channelID, userID).Scan(&exists)
	return exists, err
}

// NotificationOutboxInput 保存通知OutboxInput，供当前处理流程使用
type NotificationOutboxInput struct {
	ChannelID int64
	EventType string
	Body      string
}

// NotificationOutboxMessage 保存通知Outbox消息，供当前处理流程使用
type NotificationOutboxMessage struct {
	// ID 是通知 outbox 记录的稳定标识，仅用于运维定位，不代表正文内容。
	ID int64
	// ChannelID 是通知渠道标识，用于按渠道归属执行权限过滤。
	ChannelID int64
	// EventType 是通知事件类型，不包含通知正文或渠道配置。
	EventType string
	// Body 是发送给渠道的正文；仅供内部 worker 使用，禁止进入运维查询响应。
	Body string
	// AttemptCount 是当前记录已经尝试发送的次数。
	AttemptCount int
}

// NotificationUncertainSummary 是外部发送完成但本地确认失败的通知摘要。
// 该模型刻意不携带正文、渠道配置和最后错误文本，避免运维接口泄露敏感信息。
type NotificationUncertainSummary struct {
	// ID 是通知 outbox 记录的稳定标识。
	ID int64
	// ChannelID 是关联通知渠道标识。
	ChannelID int64
	// OwnerUserID 是通知渠道所属用户；普通用户查询只返回自己的记录。
	OwnerUserID int64
	// EventType 是通知事件分类。
	EventType string
	// AttemptCount 是进入不确定状态前的发送尝试次数。
	AttemptCount int
	// UncertainAt 是进入不确定状态的 Unix 秒时间戳。
	UncertainAt int64
	// HasError 表示数据库是否记录了本地确认错误，但不暴露错误原文。
	HasError bool
}

// ListUncertainOutboxForUser 查询指定用户拥有渠道的不确定通知摘要。
// 查询只返回元数据，不读取正文、加密渠道配置或凭证；limit 会被限制在 1 到 100 之间。
func (n *Notifications) ListUncertainOutboxForUser(ctx context.Context, userID int64, limit int) ([]NotificationUncertainSummary, error) {
	return n.listUncertainOutbox(ctx, &userID, limit)
}

// ListUncertainOutboxForAdmin 查询所有用户的不确定通知摘要，供管理员运维核对使用。
// 管理员结果包含渠道所属用户 ID，但仍不包含正文、错误原文或任何凭证。
func (n *Notifications) ListUncertainOutboxForAdmin(ctx context.Context, limit int) ([]NotificationUncertainSummary, error) {
	return n.listUncertainOutbox(ctx, nil, limit)
}

// listUncertainOutbox 按可选用户归属读取不确定通知元数据。
// ownerUserID 非空时强制限制到该用户；为空时仅供管理员调用并返回全局结果。
func (n *Notifications) listUncertainOutbox(ctx context.Context, ownerUserID *int64, limit int) ([]NotificationUncertainSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// query 保存按用户隔离的不确定通知元数据查询语句，不选择正文或错误原文。
	query := `SELECT no.id, no.channel_id, COALESCE(nc.user_id,0), no.event_type,
			no.attempt_count, no.uncertain_at, CASE WHEN no.last_error<>'' THEN 1 ELSE 0 END
		FROM notification_outbox no
		LEFT JOIN notification_channels nc ON nc.id=no.channel_id
		WHERE no.status='uncertain'`
	// args 保存 query 中用户归属与分页参数的绑定值。
	args := make([]any, 0, 2)
	if ownerUserID != nil {
		query += ` AND nc.user_id=?`
		args = append(args, *ownerUserID)
	}
	query += ` ORDER BY no.uncertain_at DESC, no.id DESC LIMIT ?`
	args = append(args, limit)
	// rows 保存按权限过滤后读取到的通知摘要行；err 保存查询失败原因。
	rows, err := n.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// summaries 保存当前用户或管理员可见的不确定通知摘要。
	summaries := make([]NotificationUncertainSummary, 0)
	for rows.Next() {
		// summary 保存当前遍历到的通知不确定状态摘要；hasError 是内部错误存在标记。
		var summary NotificationUncertainSummary
		// hasError 保存数据库中是否存在本地确认错误的布尔整型值。
		var hasError int
		// scanErr 保存当前摘要行读取失败的数据库错误。
		if scanErr := rows.Scan(&summary.ID, &summary.ChannelID, &summary.OwnerUserID, &summary.EventType, &summary.AttemptCount, &summary.UncertainAt, &hasError); scanErr != nil {
			return nil, scanErr
		}
		summary.HasError = hasError != 0
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

// CountUncertainOutboxForUser 统计指定用户拥有渠道的不确定通知数量，不读取正文或错误原文。
func (n *Notifications) CountUncertainOutboxForUser(ctx context.Context, userID int64) (int, error) {
	return n.countUncertainOutbox(ctx, &userID)
}

// CountUncertainOutboxForAdmin 统计全局不确定通知数量，供管理员运维看板使用。
func (n *Notifications) CountUncertainOutboxForAdmin(ctx context.Context) (int, error) {
	return n.countUncertainOutbox(ctx, nil)
}

// countUncertainOutbox 按可选渠道所属用户统计不确定通知数量。
func (n *Notifications) countUncertainOutbox(ctx context.Context, ownerUserID *int64) (int, error) {
	// query 保存不确定通知数量统计语句，不读取正文或错误原文。
	query := `SELECT COUNT(*) FROM notification_outbox no LEFT JOIN notification_channels nc ON nc.id=no.channel_id WHERE no.status='uncertain'`
	// args 保存可选用户归属的绑定参数。
	args := make([]any, 0, 1)
	if ownerUserID != nil {
		query += ` AND nc.user_id=?`
		args = append(args, *ownerUserID)
	}
	// count 保存满足状态与归属过滤条件的通知数量。
	var count int
	// scanErr 保存读取统计结果时的数据库错误。
	if scanErr := n.DB.QueryRowContext(ctx, query, args...).Scan(&count); scanErr != nil {
		return 0, scanErr
	}
	return count, nil
}

// NotificationBindingRow 是用户账号与通知渠道的绑定摘要。
type NotificationBindingRow struct {
	// ID 是绑定记录标识。
	ID int64
	// CookieID 是账号标识。
	CookieID string
	// ChannelID 是通知渠道标识。
	ChannelID int64
	// ChannelName 是通知渠道名称。
	ChannelName string
	// Enabled 表示绑定是否启用。
	Enabled bool
}

// ListBindingsForUser 查询用户所有账号的通知渠道绑定。
func (n *Notifications) ListBindingsForUser(ctx context.Context, userID int64) ([]NotificationBindingRow, error) {
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := n.DB.QueryContext(ctx, `
		SELECT mn.id, mn.cookie_id, mn.channel_id, COALESCE(nc.name, ''), mn.enabled
		  FROM message_notifications mn
		  JOIN cookies c ON c.id=mn.cookie_id
		  JOIN notification_channels nc ON nc.id=mn.channel_id AND nc.user_id=c.user_id
		 WHERE c.user_id=? ORDER BY mn.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 保存out，供当前处理流程使用
	var out []NotificationBindingRow
	for rows.Next() {
		// item 保存商品，供当前处理流程使用
		var item NotificationBindingRow
		// enabled 保存启用状态，供当前处理流程使用
		var enabled int
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&item.ID, &item.CookieID, &item.ChannelID, &item.ChannelName, &enabled); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

// SetSingleBinding 更新单个账号通知渠道的启用状态。
func (n *Notifications) SetSingleBinding(ctx context.Context, cookieID string, channelID int64, enabled bool) error {
	if !enabled {
		// err 保存err，供当前处理流程使用
		_, err := n.DB.ExecContext(ctx, `DELETE FROM message_notifications WHERE cookie_id=? AND channel_id=?`, cookieID, channelID)
		return err
	}
	// err 保存err，供当前处理流程使用
	_, err := n.DB.ExecContext(ctx,
		`INSERT INTO message_notifications (cookie_id, channel_id, enabled) VALUES (?, ?, ?)`+
			dialectUpsert(n.Dialect, []string{"cookie_id", "channel_id"}, map[string]string{"enabled": "EXCLUDED.enabled", "updated_at": "CURRENT_TIMESTAMP"}),
		cookieID, channelID, 1)
	return err
}

// DeleteBinding 删除用户账号下的一条通知绑定。
func (n *Notifications) DeleteBinding(ctx context.Context, userID, bindingID int64) error {
	// err 保存err，供当前处理流程使用
	_, err := n.DB.ExecContext(ctx, `
		DELETE FROM message_notifications WHERE id=? AND cookie_id IN (SELECT id FROM cookies WHERE user_id=?)`, bindingID, userID)
	return err
}

// DeleteAccountBindings 删除用户账号下的全部通知绑定。
func (n *Notifications) DeleteAccountBindings(ctx context.Context, userID int64, cookieID string) error {
	// err 保存err，供当前处理流程使用
	_, err := n.DB.ExecContext(ctx, `
		DELETE FROM message_notifications WHERE cookie_id=? AND cookie_id IN (SELECT id FROM cookies WHERE user_id=?)`, cookieID, userID)
	return err
}

// AccountChannels 取某账号已启用的通知渠道（message_notifications JOIN notification_channels）。
// 移植自 get_account_notifications。
// AccountChannels 负责账号渠道列表相关处理。
func (n *Notifications) AccountChannels(ctx context.Context, cookieID string) ([]NotificationChannel, error) {
	// rows、err 保存rows、err，供当前处理流程使用
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
	// out 保存out，供当前处理流程使用
	var out []NotificationChannel
	for rows.Next() {
		// c 保存c，供当前处理流程使用
		var c NotificationChannel
		// userID 保存用户ID，供当前处理流程使用
		var userID int64
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.Config, &userID, &c.EventTypes); err != nil {
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
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// message 表示当前遍历过程中的消息
	for _, message := range messages {
		if // err 保存err，供当前处理流程使用
		_, err := tx.ExecContext(ctx, `INSERT INTO notification_outbox
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
// ClaimOutbox 负责ClaimOutbox相关处理。
func (n *Notifications) ClaimOutbox(ctx context.Context, workerToken string, now time.Time, limit int) ([]NotificationOutboxMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// nowUnix 保存nowUnix，供当前处理流程使用
	nowUnix := now.Unix()
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := n.DB.QueryContext(ctx, `SELECT id,channel_id,event_type,body,attempt_count
		FROM notification_outbox
		WHERE (status='pending' AND next_attempt_at<=?) OR (status='running' AND lease_expires_at<?)
		ORDER BY id LIMIT ?`, nowUnix, nowUnix, limit)
	if err != nil {
		return nil, err
	}
	// candidates 保存candidates，供当前处理流程使用
	var candidates []NotificationOutboxMessage
	for rows.Next() {
		// message 保存消息，供当前处理流程使用
		var message NotificationOutboxMessage
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&message.ID, &message.ChannelID, &message.EventType, &message.Body, &message.AttemptCount); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, message)
	}
	if // err 保存err，供当前处理流程使用
	err := rows.Close(); err != nil {
		return nil, err
	}
	// claimed 保存claimed，供当前处理流程使用
	claimed := candidates[:0]
	// leaseExpiresAt 保存leaseExpiresAt，供当前处理流程使用
	leaseExpiresAt := now.Add(30 * time.Second).Unix()
	// message 表示当前遍历过程中的消息
	for _, message := range candidates {
		// res、err 保存res、err，供当前处理流程使用
		res, err := n.DB.ExecContext(ctx, `UPDATE notification_outbox
			SET status='running',worker_token=?,lease_expires_at=?,attempt_count=attempt_count+1,updated_at=CURRENT_TIMESTAMP
			WHERE id=? AND ((status='pending' AND next_attempt_at<=?) OR (status='running' AND lease_expires_at<?))`,
			workerToken, leaseExpiresAt, message.ID, nowUnix, nowUnix)
		if err != nil {
			return nil, err
		}
		// count、err 保存count、err，供当前处理流程使用
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

// CompleteOutbox 负责CompleteOutbox相关处理。
func (n *Notifications) CompleteOutbox(ctx context.Context, id int64, workerToken string) (bool, error) {
	// res、err 保存res、err，供当前处理流程使用
	res, err := n.DB.ExecContext(ctx, `DELETE FROM notification_outbox WHERE id=? AND status='running' AND worker_token=?`, id, workerToken)
	if err != nil {
		return false, err
	}
	// count、err 保存count、err，供当前处理流程使用
	count, err := res.RowsAffected()
	return err == nil && count == 1, err
}

// MarkOutboxUncertain 将外部发送成功但本地确认失败的消息隔离，阻止租约过期后自动重复发送。
// 只有仍持有当前租约的 worker 才能完成状态转移；返回 false 表示租约已被其他 worker 接管。
func (n *Notifications) MarkOutboxUncertain(ctx context.Context, id int64, workerToken, message string) (bool, error) {
	// uncertainAt 保存消息进入不确定隔离态的 Unix 时间戳，便于运维定位确认失败窗口。
	uncertainAt := time.Now().Unix()
	// result、err 保存状态更新结果和数据库错误。
	result, err := n.DB.ExecContext(ctx, `UPDATE notification_outbox
		SET status='uncertain',uncertain_at=?,next_attempt_at=0,lease_expires_at=0,worker_token='',last_error=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, uncertainAt, message, id, workerToken)
	if err != nil {
		return false, err
	}
	// affected、err 保存本次状态更新影响的行数和读取错误。
	affected, err := result.RowsAffected()
	return err == nil && affected == 1, err
}

// RetryOutbox 重试Outbox。
func (n *Notifications) RetryOutbox(ctx context.Context, id int64, workerToken, message string, nextAttemptAt int64, permanent bool) (bool, error) {
	// status 保存状态，供当前处理流程使用
	status := "pending"
	if permanent {
		status = "dead"
	}
	// res、err 保存res、err，供当前处理流程使用
	res, err := n.DB.ExecContext(ctx, `UPDATE notification_outbox
		SET status=?,next_attempt_at=?,lease_expires_at=0,worker_token='',last_error=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND worker_token=?`, status, nextAttemptAt, message, id, workerToken)
	if err != nil {
		return false, err
	}
	// count、err 保存count、err，供当前处理流程使用
	count, err := res.RowsAffected()
	return err == nil && count == 1, err
}
