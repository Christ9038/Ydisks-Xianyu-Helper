package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AgentRemoteSessionTTL 是平台会话标识的保留时长：超过该时长未使用的映射在下次写入时被清理，买家再来时开启新会话。
const AgentRemoteSessionTTL = 30 * 24 * time.Hour

// AgentRemoteSessions 保存“账号 + 买家会话”到外部 Agent 平台（Dify 的 conversation_id）会话标识的映射。
// 表内只有平台生成的不透明标识，不含 Cookie、令牌或买家消息正文。
type AgentRemoteSessions struct {
	// DB 是底层数据库连接。
	DB *sql.DB
	// Dialect 决定 upsert 语法。
	Dialect Dialect
	// now 返回当前时间；为空时使用 time.Now，测试可注入固定时钟。
	now func() time.Time
}

// currentTime 返回当前时间，供写入时间戳与过期清理使用。
func (s *AgentRemoteSessions) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// Get 读取账号下某个买家会话对应的平台会话标识；没有记录返回空串且不报错。
func (s *AgentRemoteSessions) Get(ctx context.Context, cookieID, chatID string) (string, error) {
	// sessionID、err 保存查询到的标识与查询错误。
	var sessionID string
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	err := s.DB.QueryRowContext(ctx, `SELECT session_id FROM agent_remote_sessions WHERE cookie_id = ? AND chat_id = ?`, cookieID, chatID).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return sessionID, err
}

// Put 保存或更新平台会话标识，并顺带清理已过期的映射，使表大小随活跃买家数而不是历史总量增长。
func (s *AgentRemoteSessions) Put(ctx context.Context, cookieID, chatID, sessionID string) error {
	// now 是本次写入时间的 Unix 秒数。
	now := s.currentTime().Unix()
	// err 保存 upsert 执行错误。
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO agent_remote_sessions (cookie_id, chat_id, session_id, updated_at) VALUES (?,?,?,?)`+
			dialectUpsert(s.Dialect, []string{"cookie_id", "chat_id"}, map[string]string{"session_id": "EXCLUDED.session_id", "updated_at": "EXCLUDED.updated_at"}),
		cookieID, chatID, sessionID, now)
	if err != nil {
		return err
	}
	// 清理失败不影响本次保存：过期记录留到下次写入再清理。
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM agent_remote_sessions WHERE updated_at < ?`, now-int64(AgentRemoteSessionTTL/time.Second))
	return nil
}

// Delete 删除某个买家会话的映射，平台报告会话已失效或账号更换平台后使用。
func (s *AgentRemoteSessions) Delete(ctx context.Context, cookieID, chatID string) error {
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	_, err := s.DB.ExecContext(ctx, `DELETE FROM agent_remote_sessions WHERE cookie_id = ? AND chat_id = ?`, cookieID, chatID)
	return err
}
