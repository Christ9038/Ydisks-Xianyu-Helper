package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// chatQuickReplyLimit 是数据库事务执行上限判断时使用的账号级快捷回复最大数量。
const chatQuickReplyLimit = 50

// ErrChatQuickReplyLimitReached 表示账号已达到可保存快捷回复的数量上限。
var ErrChatQuickReplyLimitReached = errors.New("聊天快捷回复数量已达上限")

// ChatQuickReply 是聊天快捷回复的持久化读取模型，不含账号凭证或用户秘密。
type ChatQuickReply struct {
	// ID 是数据库生成的稳定快捷回复标识。
	ID int64
	// CookieID 是快捷回复所属账号标识。
	CookieID string
	// Content 是人工发送时使用的文本模板。
	Content string
	// CreatedAt 是记录创建的 Unix 秒时间戳。
	CreatedAt int64
}

// ChatBuyerNote 是按账号与平台买家 ID 归属的备注持久化模型。
type ChatBuyerNote struct {
	// CookieID 是备注所属账号标识。
	CookieID string
	// BuyerID 是买家稳定平台标识，不使用聊天会话 ID。
	BuyerID string
	// Content 是完整备注正文。
	Content string
	// UpdatedAt 是最近一次保存的 Unix 秒时间戳。
	UpdatedAt int64
}

// ListQuickReplies 查询账号下按最新创建时间排序的人工快捷回复。
func (s *ChatStore) ListQuickReplies(ctx context.Context, cookieID string) ([]ChatQuickReply, error) {
	// rows 和 queryErr 保存快捷回复查询游标及数据库错误。
	rows, queryErr := s.DB.QueryContext(ctx, `SELECT id,cookie_id,content,created_at
		FROM chat_quick_replies WHERE cookie_id=? ORDER BY id DESC`, cookieID)
	if queryErr != nil {
		return nil, queryErr
	}
	defer rows.Close()
	// replies 保存按数据库顺序扫描出的快捷回复集合。
	replies := make([]ChatQuickReply, 0)
	// row 表示当前正在扫描的一条账号快捷回复。
	for rows.Next() {
		// reply 保存当前行映射得到的非敏感快捷回复。
		var reply ChatQuickReply
		// scanErr 保存当前快捷回复行字段映射失败原因。
		if scanErr := rows.Scan(&reply.ID, &reply.CookieID, &reply.Content, &reply.CreatedAt); scanErr != nil {
			return nil, scanErr
		}
		replies = append(replies, reply)
	}
	return replies, rows.Err()
}

// CreateQuickReply 在账号行锁保护下创建快捷回复，确保并发请求不能突破数量上限。
func (s *ChatStore) CreateQuickReply(ctx context.Context, cookieID, content string) (ChatQuickReply, error) {
	// transaction 和 beginErr 保存本次账号级写入事务及启动错误。
	transaction, beginErr := s.DB.BeginTx(ctx, nil)
	if beginErr != nil {
		return ChatQuickReply{}, beginErr
	}
	defer transaction.Rollback()
	// lockQuery 保存方言适配的账号行锁 SQL；SQLite 事务的单写入者语义已经提供相同串行化保证。
	lockQuery := `SELECT id FROM cookies WHERE id=?`
	if s.Dialect != DialectSQLite {
		lockQuery += ` FOR UPDATE`
	}
	// lockedAccountID 保存锁定并确认存在的账号标识。
	var lockedAccountID string
	// lockErr 保存账号行锁定或账号不存在导致的查询失败。
	if lockErr := transaction.QueryRowContext(ctx, lockQuery, cookieID).Scan(&lockedAccountID); lockErr != nil {
		return ChatQuickReply{}, lockErr
	}
	// replyCount 保存当前账号已提交快捷回复数量。
	var replyCount int
	// countErr 保存读取账号快捷回复已用数量时的事务查询错误。
	if countErr := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM chat_quick_replies WHERE cookie_id=?`, cookieID).Scan(&replyCount); countErr != nil {
		return ChatQuickReply{}, countErr
	}
	if replyCount >= chatQuickReplyLimit {
		return ChatQuickReply{}, ErrChatQuickReplyLimitReached
	}
	// createdAt 保存新记录的 UTC 创建时间，供列表排序和传输 DTO 展示。
	createdAt := time.Now().UTC().Unix()
	// replyID 和 insertErr 保存数据库生成的主键及写入错误。
	replyID, insertErr := insertReturningID(ctx, transaction, s.Dialect, `INSERT INTO chat_quick_replies(cookie_id,content,created_at) VALUES(?,?,?)`, cookieID, content, createdAt)
	if insertErr != nil {
		return ChatQuickReply{}, insertErr
	}
	// commitErr 保存提交本次受账号锁保护写入事务时的失败原因。
	if commitErr := transaction.Commit(); commitErr != nil {
		return ChatQuickReply{}, commitErr
	}
	return ChatQuickReply{ID: replyID, CookieID: cookieID, Content: content, CreatedAt: createdAt}, nil
}

// DeleteQuickReply 删除账号下指定快捷回复，并报告是否实际删除记录。
func (s *ChatStore) DeleteQuickReply(ctx context.Context, cookieID string, quickReplyID int64) (bool, error) {
	// result 和 deleteErr 保存删除执行结果及数据库错误。
	result, deleteErr := s.DB.ExecContext(ctx, `DELETE FROM chat_quick_replies WHERE id=? AND cookie_id=?`, quickReplyID, cookieID)
	if deleteErr != nil {
		return false, deleteErr
	}
	// affected 和 affectedErr 保存删除影响行数及驱动读取错误。
	affected, affectedErr := result.RowsAffected()
	if affectedErr != nil {
		return false, affectedErr
	}
	return affected > 0, nil
}

// GetBuyerNote 查询账号下买家的备注；没有记录时返回 found=false。
func (s *ChatStore) GetBuyerNote(ctx context.Context, cookieID, buyerID string) (ChatBuyerNote, bool, error) {
	// note 保存查询得到的买家备注持久化模型。
	var note ChatBuyerNote
	// readErr 保存按账号与买家标识读取备注时产生的数据库错误。
	readErr := s.DB.QueryRowContext(ctx, `SELECT cookie_id,buyer_id,content,updated_at
		FROM chat_buyer_notes WHERE cookie_id=? AND buyer_id=?`, cookieID, buyerID).Scan(&note.CookieID, &note.BuyerID, &note.Content, &note.UpdatedAt)
	if errors.Is(readErr, sql.ErrNoRows) {
		return ChatBuyerNote{}, false, nil
	}
	if readErr != nil {
		return ChatBuyerNote{}, false, readErr
	}
	return note, true, nil
}

// SaveBuyerNote 保存完整备注；空正文会删除旧记录并返回逻辑空备注。
func (s *ChatStore) SaveBuyerNote(ctx context.Context, note ChatBuyerNote) (ChatBuyerNote, error) {
	if note.Content == "" {
		// deleteErr 保存清除买家备注时产生的数据库错误；没有旧记录也视为成功。
		_, deleteErr := s.DB.ExecContext(ctx, `DELETE FROM chat_buyer_notes WHERE cookie_id=? AND buyer_id=?`, note.CookieID, note.BuyerID)
		if deleteErr != nil {
			return ChatBuyerNote{}, deleteErr
		}
		return ChatBuyerNote{CookieID: note.CookieID, BuyerID: note.BuyerID}, nil
	}
	// updatedAt 保存本次覆盖写入的 UTC 时间，客户端用它显示最新保存状态。
	updatedAt := time.Now().UTC().Unix()
	// query 保存三方言共用的 upsert 语句；方言工具负责生成冲突更新差异。
	query := `INSERT INTO chat_buyer_notes(cookie_id,buyer_id,content,updated_at) VALUES(?,?,?,?)` + dialectUpsert(s.Dialect, []string{"cookie_id", "buyer_id"}, map[string]string{"content": "EXCLUDED.content", "updated_at": "EXCLUDED.updated_at"})
	// saveErr 保存备注 upsert 执行失败原因，失败时不得返回未持久化模型。
	if _, saveErr := s.DB.ExecContext(ctx, query, note.CookieID, note.BuyerID, note.Content, updatedAt); saveErr != nil {
		return ChatBuyerNote{}, saveErr
	}
	return ChatBuyerNote{CookieID: note.CookieID, BuyerID: note.BuyerID, Content: note.Content, UpdatedAt: updatedAt}, nil
}
