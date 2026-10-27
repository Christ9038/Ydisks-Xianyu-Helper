package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ChatSession 用于本次流程后续判断的聊天会话
type ChatSession struct {
	CookieID string `json:"account_id"`
	ChatID   string `json:"chat_id"`
	// BuyerID 是历史数据库列承载的会话对端标识；对外契约必须使用 peer_user_id。
	BuyerID string `json:"peer_user_id"`
	// BuyerName 是历史数据库列承载的会话对端昵称；对外契约必须使用 peer_name。
	BuyerName string `json:"peer_name"`
	// BuyerAvatar 是历史数据库列承载的会话对端头像；对外契约必须使用 peer_avatar_url。
	BuyerAvatar string `json:"peer_avatar_url"`
	// AccountRole 是当前账号在 RoleItemID 商品会话中的角色，仅允许 seller、buyer 或 unknown。
	AccountRole string `json:"account_role"`
	// BuyerUserID 是经本地商品或平台发布者证据确认的买家平台标识。
	BuyerUserID string `json:"buyer_user_id"`
	// SellerUserID 是经本地商品或平台发布者证据确认的卖家平台标识。
	SellerUserID string `json:"seller_user_id"`
	// RoleItemID 是角色结论绑定的商品标识，商品变化时旧结论不得复用。
	RoleItemID string `json:"role_item_id"`
	// RoleSource 是角色证据来源，例如 local_item 或 platform_verified。
	RoleSource string `json:"role_source"`
	ItemID     string `json:"item_id"`
	ItemTitle  string `json:"item_title"`
	// ItemImageURL 是会话商品主图的公开地址，仅用于聊天列表展示。
	ItemImageURL  string `json:"item_image_url"`
	LastMessage   string `json:"last_message"`
	LastMessageAt int64  `json:"last_message_at"`
	UnreadCount   int    `json:"unread_count"`
	// UserHiddenAt 是用户删除会话的 Unix 毫秒时间；非零时仅从聊天列表隐藏，自动化仍可使用会话标识。
	UserHiddenAt int64 `json:"-"`
	// MessagesClearedAt 是本地消息永久清空的 Unix 毫秒截止时间，历史同步不得重新写入该时间及之前的消息。
	MessagesClearedAt int64 `json:"-"`
	// LocalMessagesClearedAt 是实时消息的本地接纳截止时间，恢复可见状态时也必须保留。
	LocalMessagesClearedAt int64 `json:"-"`
}

// ChatSessionCursor 是聊天会话稳定键集分页的最后一条排序键。
// LastMessageAt 与 ChatID 必须同时使用，避免相同消息时间的会话翻页时重复或遗漏。
type ChatSessionCursor struct {
	// LastMessageAt 是上一页末尾会话的最后消息毫秒时间戳。
	LastMessageAt int64
	// ChatID 是同一时间戳下用于稳定排序和翻页的会话标识。
	ChatID string
}

// ChatSessionPage 是本地聊天会话键集分页查询结果。
type ChatSessionPage struct {
	// Sessions 是按最后消息时间和会话标识倒序排列的当前页结果。
	Sessions []ChatSession
	// HasMore 表示本地缓存中是否还有下一页会话。
	HasMore bool
	// NextCursor 是继续读取下一页所需的最后排序键；没有下一页时为 nil。
	NextCursor *ChatSessionCursor
}

// ChatMessage 用于本次流程后续判断的聊天消息
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
	// MediaDuration 是平台语音消息提供的秒级时长，非语音或缺失时为零。
	MediaDuration int64  `json:"media_duration"`
	Status        string `json:"status"`
	ReadStatus    int    `json:"read_status"`
	ReadAt        int64  `json:"read_at,omitempty"`
	SentAt        int64  `json:"sent_at"`
	// ObservedAt 是本进程首次接纳实时或人工消息的 Unix 毫秒时间，只参与删除并发判定，不持久化也不返回前端；零值表示平台历史回灌。
	ObservedAt int64 `json:"-"`
}

// ChatStore 用于本次流程后续判断的聊天Store
type ChatStore struct {
	DB      *sql.DB
	Dialect Dialect
}

// UpsertSession 封装Upsert会话业务协调。
func (s *ChatStore) UpsertSession(ctx context.Context, session ChatSession) error {
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Unix()
	// prefix 用于本次流程后续判断的prefix
	prefix := dialectInsertIgnorePrefix(s.Dialect)
	// query 用于本次流程后续判断的查询
	query := prefix + ` INTO chat_sessions
		(cookie_id,chat_id,buyer_id,buyer_name,buyer_avatar_url,item_id,item_title,item_image_url,last_message,last_message_at,unread_count,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(s.Dialect, []string{"cookie_id", "chat_id"})
	if // err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, query, session.CookieID, session.ChatID, session.BuyerID, session.BuyerName,
		session.BuyerAvatar, session.ItemID, session.ItemTitle, session.ItemImageURL, session.LastMessage, session.LastMessageAt,
		session.UnreadCount, now, now); err != nil {
		return err
	}
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET
		buyer_id=CASE WHEN ?<>'' THEN ? ELSE buyer_id END,
		buyer_name=CASE WHEN ?<>'' THEN ? ELSE buyer_name END,
		buyer_avatar_url=CASE WHEN ?<>'' THEN ? ELSE buyer_avatar_url END,
		item_id=CASE WHEN ?<>'' THEN ? ELSE item_id END,
		item_title=CASE WHEN ?<>'' THEN ? ELSE item_title END,
		item_image_url=CASE WHEN ?<>'' THEN ? ELSE item_image_url END,
		account_role=CASE WHEN ?<>'' AND role_item_id<>? THEN 'unknown' ELSE account_role END,
		buyer_user_id=CASE WHEN ?<>'' AND role_item_id<>? THEN '' ELSE buyer_user_id END,
		seller_user_id=CASE WHEN ?<>'' AND role_item_id<>? THEN '' ELSE seller_user_id END,
		role_source=CASE WHEN ?<>'' AND role_item_id<>? THEN '' ELSE role_source END,
		role_item_id=CASE WHEN ?<>'' AND role_item_id<>? THEN '' ELSE role_item_id END,
		last_message=CASE WHEN ?>messages_cleared_at AND last_message_at<=? THEN ? ELSE last_message END,
		last_message_at=CASE WHEN ?>messages_cleared_at AND last_message_at<=? THEN ? ELSE last_message_at END,
		unread_count=CASE WHEN ?>messages_cleared_at AND ?>unread_count THEN ? ELSE unread_count END,
		user_hidden_at=CASE WHEN user_hidden_at>0 AND ?>messages_cleared_at THEN 0 ELSE user_hidden_at END,
		is_visible=?,updated_at=?
		WHERE cookie_id=? AND chat_id=?`, session.BuyerID, session.BuyerID, session.BuyerName, session.BuyerName,
		session.BuyerAvatar, session.BuyerAvatar, session.ItemID, session.ItemID, session.ItemTitle, session.ItemTitle, session.ItemImageURL, session.ItemImageURL,
		session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID,
		session.LastMessageAt, session.LastMessageAt, session.LastMessage, session.LastMessageAt, session.LastMessageAt, session.LastMessageAt,
		session.LastMessageAt, session.UnreadCount, session.UnreadCount, session.LastMessageAt, true, now, session.CookieID, session.ChatID)
	return err
}

// FindChatIDsByBuyerAndItem 查询账号下与买家、商品同时匹配的会话标识。
// ctx 控制查询生命周期；cookieID、buyerID、itemID 共同限制匹配范围；隐藏会话仍返回，因为它仍可作为发货目标；结果按不同 chat_id 去重，交由应用层判断唯一性。
func (s *ChatStore) FindChatIDsByBuyerAndItem(ctx context.Context, cookieID, buyerID, itemID string) ([]string, error) {
	// accountID 保存经过空白清理的账号标识，限制候选会话所属账号。
	accountID := strings.TrimSpace(cookieID)
	// normalizedBuyerID 保存去除协议后缀的买家标识，兼容订单与会话的历史格式差异。
	normalizedBuyerID := strings.TrimSuffix(strings.TrimSpace(buyerID), "@goofish")
	// productID 保存经过空白清理的商品标识，限制候选会话所属商品。
	productID := strings.TrimSpace(itemID)
	if accountID == "" || normalizedBuyerID == "" || productID == "" {
		return nil, nil
	}
	// buyerVariants 始终同时查询裸标识和历史协议后缀，不依赖订单输入格式，避免漏掉候选会话或误判唯一性。
	buyerVariants := []string{normalizedBuyerID, normalizedBuyerID + "@goofish"}
	// rows、err 保存当前账号下候选会话的查询结果及数据库错误。
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT chat_id
		FROM chat_sessions
		WHERE cookie_id=? AND item_id=? AND TRIM(chat_id)<>'' AND (
			(account_role='seller' AND buyer_user_id IN (?,?)) OR
			(account_role='unknown' AND buyer_id IN (?,?))
		)`, accountID, productID, buyerVariants[0], buyerVariants[1], buyerVariants[0], buyerVariants[1])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// chatIDs 保存去重后的候选会话标识；调用方只有在恰有一个候选时才会自动回填。
	chatIDs := make([]string, 0, 1)
	for rows.Next() {
		// chatID 保存当前候选会话标识。
		var chatID string
		// scanErr 保存当前候选会话扫描失败的数据库错误。
		if scanErr := rows.Scan(&chatID); scanErr != nil {
			return nil, scanErr
		}
		chatIDs = append(chatIDs, chatID)
	}
	// iterationErr 保存候选会话游标结束时的数据库错误。
	if iterationErr := rows.Err(); iterationErr != nil {
		return nil, iterationErr
	}
	return chatIDs, nil
}

// SetSessionVisible 更新平台会话是否出现在本地列表中。
// ctx 控制数据库更新生命周期；cookieID 和 chatID 定位非敏感会话；visible=false 只软隐藏会话并保留全部消息。
func (s *ChatStore) SetSessionVisible(ctx context.Context, cookieID, chatID string, visible bool) error {
	// visibleValue 使用跨 SQLite、MySQL 和 PostgreSQL 驱动均可绑定的布尔值。
	visibleValue := visible
	// err 保存可见状态更新失败；会话不存在时保持幂等成功。
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET is_visible=?,updated_at=? WHERE cookie_id=? AND chat_id=?`,
		visibleValue, time.Now().UTC().Unix(), cookieID, chatID)
	return err
}

// DeleteSession 删除会话。
func (s *ChatStore) DeleteSession(ctx context.Context, cookieID, chatID string) error {
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `DELETE FROM chat_sessions WHERE cookie_id=? AND chat_id=?`, cookieID, chatID)
	return err
}

// HideAndClearSession 原子隐藏用户拥有的会话并物理清空展示消息，同时保留订单和自动化使用的会话定位字段。
// userID、cookieID 和 chatID 限制删除范围；clearedAt 是 Unix 毫秒截止时间；返回 false 表示账号下没有该会话。
func (s *ChatStore) HideAndClearSession(ctx context.Context, userID int64, cookieID, chatID string, clearedAt int64) (bool, error) {
	if s == nil || s.DB == nil {
		return false, errors.New("聊天存储未初始化")
	}
	// tx 保证隐藏标记、昵称快照和消息清空要么同时成功，要么全部回滚。
	tx, beginErr := s.DB.BeginTx(ctx, nil)
	if beginErr != nil {
		return false, beginErr
	}
	defer tx.Rollback()
	// hiddenAt 保存当前用户删除状态。
	var hiddenAt, localMessagesClearedAt int64
	// buyerName 保存自动化仍会读取的昵称快照。
	var buyerName string
	// lastMessageAt 保存删除前会话摘要的平台时间，用于推进历史消息永久水位。
	var lastMessageAt int64
	// lookupQuery 是带用户归属条件的会话读取语句；行锁数据库用它串行化删除与新消息保存。
	lookupQuery := `SELECT cs.user_hidden_at,cs.local_messages_cleared_at,cs.buyer_name,cs.last_message_at
		FROM chat_sessions cs JOIN cookies c ON c.id=cs.cookie_id
		WHERE c.user_id=? AND cs.cookie_id=? AND cs.chat_id=?`
	if s.Dialect != DialectSQLite {
		lookupQuery += ` FOR UPDATE`
	}
	// lookupErr 是带用户归属条件的会话读取结果，避免跨用户清空消息。
	lookupErr := tx.QueryRowContext(ctx, lookupQuery, userID, cookieID, chatID).Scan(&hiddenAt, &localMessagesClearedAt, &buyerName, &lastMessageAt)
	if errors.Is(lookupErr, sql.ErrNoRows) {
		return false, nil
	}
	if lookupErr != nil {
		return false, lookupErr
	}
	if hiddenAt > 0 {
		return true, nil
	}
	// preservedName 在摘要昵称为空或被平台遮罩时保存历史中最后一个可信昵称，清空消息后自动化模板仍可使用。
	preservedName := strings.TrimSpace(buyerName)
	if preservedName == "" || strings.Contains(preservedName, "***") {
		// candidateName 是删除前从真实入站消息中提取的最后一个未遮罩买家昵称。
		var candidateName string
		// nicknameErr 允许没有候选昵称，但任何真实数据库错误都必须回滚删除事务。
		nicknameErr := tx.QueryRowContext(ctx, `SELECT sender_name FROM chat_messages
			WHERE cookie_id=? AND chat_id=? AND direction='incoming' AND sender_name<>'' AND sender_name NOT LIKE '%***%'
			AND message_type<>'system' AND sender_name<>content
			ORDER BY sent_at DESC,id DESC LIMIT 1`, cookieID, chatID).Scan(&candidateName)
		if nicknameErr == nil {
			preservedName = strings.TrimSpace(candidateName)
		} else if !errors.Is(nicknameErr, sql.ErrNoRows) {
			return false, nicknameErr
		}
	}
	if clearedAt <= 0 {
		clearedAt = time.Now().UTC().UnixMilli()
	}
	if clearedAt < localMessagesClearedAt {
		clearedAt = localMessagesClearedAt
	}
	// maximumMessageAt 保存事务开始前已存在消息的最大平台时间，未来偏移的旧消息也必须纳入历史水位。
	var maximumMessageAt int64
	// maximumErr 读取当前全部展示消息的最大平台时间；聚合查询总会返回一行。
	maximumErr := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sent_at),0) FROM chat_messages WHERE cookie_id=? AND chat_id=?`, cookieID, chatID).Scan(&maximumMessageAt)
	if maximumErr != nil {
		return false, maximumErr
	}
	// historyCutoff 只在平台消息时间域内取删除前摘要和已存消息的最大值；不得混入本机删除时钟，否则平台时钟落后时会吞掉删除后的新历史。
	historyCutoff := int64(0)
	if lastMessageAt > historyCutoff {
		historyCutoff = lastMessageAt
	}
	if maximumMessageAt > historyCutoff {
		historyCutoff = maximumMessageAt
	}
	// previousHistoryCutoff 防止平台时间偏移时第二次删除让历史水位倒退。
	var previousHistoryCutoff int64
	// err 保存读取历史截止线的数据库错误。
	if err := tx.QueryRowContext(ctx, `SELECT messages_cleared_at FROM chat_sessions WHERE cookie_id=? AND chat_id=?`, cookieID, chatID).Scan(&previousHistoryCutoff); err != nil {
		return false, err
	}
	if previousHistoryCutoff > historyCutoff {
		historyCutoff = previousHistoryCutoff
	}
	// updateResult 以 user_hidden_at=0 领取本次删除，重复请求不会推进截止时间或删除后来恢复的消息。
	updateResult, updateErr := tx.ExecContext(ctx, `UPDATE chat_sessions SET
		buyer_name=CASE WHEN ?<>'' THEN ? ELSE buyer_name END,user_hidden_at=?,local_messages_cleared_at=?,messages_cleared_at=?,
		last_message='',last_message_at=0,unread_count=0,updated_at=?
		WHERE cookie_id=? AND chat_id=? AND user_hidden_at=0`, preservedName, preservedName, clearedAt, clearedAt, historyCutoff,
		time.Now().UTC().Unix(), cookieID, chatID)
	if updateErr != nil {
		return false, updateErr
	}
	// updatedRows 判断并发重复请求是否已经由另一个事务完成；零行时保持幂等成功且不再次清空。
	updatedRows, rowsErr := updateResult.RowsAffected()
	if rowsErr != nil {
		return false, rowsErr
	}
	if updatedRows == 0 {
		return true, nil
	}
	// deleteErr 删除取得会话行锁前已经完成落库的全部展示消息；等待该锁的新消息会在提交后按观察时间重新判定。
	if _, deleteErr := tx.ExecContext(ctx, `DELETE FROM chat_messages WHERE cookie_id=? AND chat_id=?`, cookieID, chatID); deleteErr != nil {
		return false, deleteErr
	}
	// commitErr 保存隐藏标记、历史水位和消息物理清空的事务提交结果。
	if commitErr := tx.Commit(); commitErr != nil {
		return false, commitErr
	}
	return true, nil
}

// DeleteEmptySessions removes conversation shells returned by IM pagination
// with visible=0 and no lastMessage. Older versions persisted these shells as
// "暂无消息", although the official UI never renders them.
// DeleteEmptySessions 删除EmptySessions。
func (s *ChatStore) DeleteEmptySessions(ctx context.Context, cookieID string) error {
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `DELETE FROM chat_sessions
		WHERE cookie_id=? AND (last_message='' OR last_message='暂无消息')
		AND user_hidden_at=0
		AND NOT EXISTS (SELECT 1 FROM chat_messages m WHERE m.cookie_id=chat_sessions.cookie_id AND m.chat_id=chat_sessions.chat_id)`, cookieID)
	return err
}

// SyncSessionSummary applies the authoritative last-message timestamp from the
// official conversation response. observedModifyAt guards against overwriting
// a genuinely newer live message that arrived after that response was built.
// SyncSessionSummary 同步会话Summary。
func (s *ChatStore) SyncSessionSummary(ctx context.Context, cookieID, chatID, summary string, sentAt, observedModifyAt int64, unread int) error {
	// err 用于本次流程后续判断的err
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_sessions SET last_message=?,last_message_at=?,unread_count=?,
		user_hidden_at=CASE WHEN user_hidden_at>0 THEN 0 ELSE user_hidden_at END,updated_at=?
		WHERE cookie_id=? AND chat_id=? AND last_message_at<=? AND ?>messages_cleared_at`, summary, sentAt, unread,
		time.Now().UTC().Unix(), cookieID, chatID, observedModifyAt, sentAt)
	return err
}

// UpdateSessionIdentity 更新会话Identity。
func (s *ChatStore) UpdateSessionIdentity(ctx context.Context, cookieID, chatID, buyerID, buyerName, avatarURL string) error {
	// err 用于本次流程后续判断的err
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
// LatestUnmaskedPeerName 封装LatestUnmaskedPeer名称业务协调。
func (s *ChatStore) LatestUnmaskedPeerName(ctx context.Context, cookieID, chatID string) (string, error) {
	// name 用于本次流程后续判断的名称
	var name string
	// err 用于本次流程后续判断的err
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

// BuyerNicknameForAutomation 返回自动化模板可使用的买家昵称，并优先回退到未遮罩的历史消息昵称。
func (s *ChatStore) BuyerNicknameForAutomation(ctx context.Context, cookieID, chatID string) (string, error) {
	// name 保存会话摘要中的买家昵称。
	var name string
	// err 保存会话摘要查询错误。
	err := s.DB.QueryRowContext(ctx, `SELECT buyer_name FROM chat_sessions WHERE cookie_id=? AND chat_id=?`, cookieID, chatID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return s.LatestUnmaskedPeerName(ctx, cookieID, chatID)
	}
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name != "" && !strings.Contains(name, "***") {
		return name, nil
	}
	// fallback 保存消息历史中可直接展示的昵称。
	fallback, fallbackErr := s.LatestUnmaskedPeerName(ctx, cookieID, chatID)
	if fallbackErr != nil {
		return "", fallbackErr
	}
	if fallback != "" {
		return fallback, nil
	}
	return name, nil
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
	// read_status is also used for incoming messages: only a newly received
	// real-user message starts unread. Imported history and official system
	// notices must never contribute to the chat badge.
	if message.Direction == "incoming" && (!unread || message.MessageType == "system") {
		message.ReadStatus = 2
		message.ReadAt = time.Now().UTC().UnixMilli()
	}
	message.CookieID, message.ChatID = session.CookieID, session.ChatID
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Unix()
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	// The composite foreign key on chat_messages requires the session to exist
	// first. Insert an empty shell without touching an existing conversation.
	// sessionPrefix 用于本次流程后续判断的会话Prefix
	sessionPrefix := dialectInsertIgnorePrefix(s.Dialect)
	// sessionInsert 用于本次流程后续判断的会话Insert
	sessionInsert := sessionPrefix + ` INTO chat_sessions
		(cookie_id,chat_id,buyer_id,buyer_name,buyer_avatar_url,item_id,item_title,item_image_url,last_message,last_message_at,unread_count,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(s.Dialect, []string{"cookie_id", "chat_id"})
	if // err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, sessionInsert, session.CookieID, session.ChatID, session.BuyerID,
		session.BuyerName, session.BuyerAvatar, session.ItemID, session.ItemTitle, session.ItemImageURL, "", int64(0), 0, now, now); err != nil {
		return nil, false, fmt.Errorf("建立聊天会话: %w", err)
	}
	// userHiddenAt 和 messagesClearedAt 分别是本地观察截止线与平台历史永久水位。
	var userHiddenAt, localMessagesClearedAt, messagesClearedAt int64
	// cutoffQuery 读取清空边界；行锁数据库借此与删除事务串行，SQLite 已由前面的写入取得事务写锁。
	cutoffQuery := `SELECT user_hidden_at,local_messages_cleared_at,messages_cleared_at FROM chat_sessions WHERE cookie_id=? AND chat_id=?`
	if s.Dialect != DialectSQLite {
		cutoffQuery += ` FOR UPDATE`
	}
	// cutoffErr 读取当前会话的清空边界；读取失败必须终止事务，避免绕过用户删除语义。
	cutoffErr := tx.QueryRowContext(ctx, cutoffQuery, session.CookieID, session.ChatID).Scan(&userHiddenAt, &localMessagesClearedAt, &messagesClearedAt)
	if cutoffErr != nil {
		return nil, false, fmt.Errorf("读取聊天消息清空边界: %w", cutoffErr)
	}
	// liveAccepted 表示实时或人工消息在用户删除动作之后才被本进程接纳；它不依赖平台的秒级或偏移时间。
	liveAccepted := message.ObservedAt > 0 && (localMessagesClearedAt == 0 || message.ObservedAt > localMessagesClearedAt)
	// historyAccepted 表示平台历史消息严格晚于删除时记录的最大历史水位。
	historyAccepted := message.ObservedAt == 0 && (messagesClearedAt == 0 || message.SentAt > messagesClearedAt)
	if !liveAccepted && !historyAccepted {
		return nil, false, nil
	}

	// prefix 用于本次流程后续判断的prefix
	prefix := dialectInsertIgnorePrefix(s.Dialect)
	// query 保存带已读字段的幂等插入 SQL，三方言冲突时保持同一列顺序。
	query := prefix + ` INTO chat_messages
		(cookie_id,chat_id,message_key,direction,sender_id,sender_name,message_type,content,media_duration,status,read_status,read_at,sent_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(s.Dialect, []string{"cookie_id", "message_key"})
	// res、err 保存插入结果及执行错误，用于判断是否更新会话摘要。
	res, err := tx.ExecContext(ctx, query, message.CookieID, message.ChatID, message.MessageKey,
		message.Direction, message.SenderID, message.SenderName, message.MessageType, message.Content, message.MediaDuration,
		message.Status, message.ReadStatus, message.ReadAt, message.SentAt, now)
	if err != nil {
		return nil, false, fmt.Errorf("保存聊天消息: %w", err)
	}
	// inserted 用于本次流程后续判断的inserted
	inserted, _ := res.RowsAffected()
	if inserted > 0 {
		// inheritErr 保存买家后续消息确认此前出站消息已读时的失败；同一会话中的后续回复是已读历史的确定证据。
		if message.Direction == "incoming" && message.MessageType != "system" {
			// inheritErr 表示把后续买家消息作为已读证据回写到此前出站消息时的数据库错误。
			_, inheritErr := tx.ExecContext(ctx, `UPDATE chat_messages SET read_status=2,
				read_at=CASE WHEN read_status=2 AND read_at>0 THEN read_at ELSE ? END
				WHERE cookie_id=? AND chat_id=? AND direction='outgoing' AND sent_at<=?`, message.SentAt, message.CookieID, message.ChatID, message.SentAt)
			if inheritErr != nil {
				return nil, false, fmt.Errorf("按后续消息确认聊天已读: %w", inheritErr)
			}
		}
		// inheritErr 保存平台消息补入历史时继承本地临时消息已读回执的失败，避免同一消息因键不同长期显示未读。
		if message.Direction == "outgoing" && strings.HasSuffix(message.MessageKey, ".PNM") {
			// localReadAt 保存与该平台消息匹配的本地临时消息已读时间；先单独查询，避免 MySQL 禁止 UPDATE 子查询引用目标表（错误 1093）。
			var localReadAt sql.NullInt64
			// lookupErr 表示查询可继承已读回执的本地临时消息时的数据库错误；无匹配行属于正常情况。
			lookupErr := tx.QueryRowContext(ctx, `SELECT local.read_at FROM chat_messages AS local
				WHERE local.cookie_id=? AND local.chat_id=? AND local.direction='outgoing'
				AND local.message_key NOT LIKE '%.PNM' AND local.content=? AND local.read_status=2
				AND ABS(local.sent_at-?)<=10000 ORDER BY local.read_at DESC LIMIT 1`,
				message.CookieID, message.ChatID, message.Content, message.SentAt).Scan(&localReadAt)
			if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
				return nil, false, fmt.Errorf("继承聊天已读回执: %w", lookupErr)
			}
			if lookupErr == nil && localReadAt.Valid {
				// inheritErr 表示把临时出站消息的已读回执继承到平台补入历史消息时的数据库错误。
				_, inheritErr := tx.ExecContext(ctx, `UPDATE chat_messages SET read_status=2,read_at=?
					WHERE cookie_id=? AND message_key=? AND read_status<>2`, localReadAt.Int64, message.CookieID, message.MessageKey)
				if inheritErr != nil {
					return nil, false, fmt.Errorf("继承聊天已读回执: %w", inheritErr)
				}
			}
		}
		// unreadDelta 用于本次流程后续判断的unreadDelta
		unreadDelta := 0
		if unread {
			unreadDelta = 1
		}
		if // err 用于本次流程后续判断的err
		_, err := tx.ExecContext(ctx, `UPDATE chat_sessions SET buyer_id=CASE WHEN ?<>'' THEN ? ELSE buyer_id END,
			buyer_name=CASE WHEN ?<>'' THEN ? ELSE buyer_name END,buyer_avatar_url=CASE WHEN ?<>'' THEN ? ELSE buyer_avatar_url END,
			item_id=CASE WHEN ?<>'' THEN ? ELSE item_id END,item_title=CASE WHEN ?<>'' THEN ? ELSE item_title END,
		item_image_url=CASE WHEN ?<>'' THEN ? ELSE item_image_url END,
		account_role=CASE WHEN ?<>'' AND role_item_id<>? THEN 'unknown' ELSE account_role END,
		buyer_user_id=CASE WHEN ?<>'' AND role_item_id<>? THEN '' ELSE buyer_user_id END,
		seller_user_id=CASE WHEN ?<>'' AND role_item_id<>? THEN '' ELSE seller_user_id END,
		role_source=CASE WHEN ?<>'' AND role_item_id<>? THEN '' ELSE role_source END,
		role_item_id=CASE WHEN ?<>'' AND role_item_id<>? THEN '' ELSE role_item_id END,
		last_message=CASE WHEN last_message_at<=? THEN ? ELSE last_message END,
		last_message_at=CASE WHEN last_message_at<=? THEN ? ELSE last_message_at END,
		unread_count=unread_count+?,user_hidden_at=CASE WHEN user_hidden_at>0 THEN 0 ELSE user_hidden_at END,
		is_visible=?,updated_at=?
			WHERE cookie_id=? AND chat_id=?`, session.BuyerID, session.BuyerID, session.BuyerName, session.BuyerName, session.BuyerAvatar, session.BuyerAvatar,
			session.ItemID, session.ItemID, session.ItemTitle, session.ItemTitle, session.ItemImageURL, session.ItemImageURL,
			session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID, session.ItemID,
			message.SentAt, message.Content, message.SentAt, message.SentAt, unreadDelta,
			true, now, session.CookieID, session.ChatID); err != nil {
			return nil, false, fmt.Errorf("更新聊天会话: %w", err)
		}
	}
	if // err 用于本次流程后续判断的err
	err := tx.Commit(); err != nil {
		return nil, false, err
	}
	// stored、err 用于本次流程后续判断的stored、err
	stored, err := s.GetMessageByKey(ctx, message.CookieID, message.MessageKey)
	return stored, inserted > 0, err
}

// GetMessageByKey 读取消息ByKey。
func (s *ChatStore) GetMessageByKey(ctx context.Context, cookieID, key string) (*ChatMessage, error) {
	// m 保存按账号和幂等键读取的完整聊天消息。
	var m ChatMessage
	// err 保存查询错误；不存在时转换为仓储统一的 ErrNotFound。
	err := s.DB.QueryRowContext(ctx, `SELECT id,cookie_id,chat_id,message_key,direction,sender_id,sender_name,message_type,content,media_duration,status,read_status,read_at,sent_at
		FROM chat_messages WHERE cookie_id=? AND message_key=?`, cookieID, key).Scan(
		&m.ID, &m.CookieID, &m.ChatID, &m.MessageKey, &m.Direction, &m.SenderID, &m.SenderName,
		&m.MessageType, &m.Content, &m.MediaDuration, &m.Status, &m.ReadStatus, &m.ReadAt, &m.SentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &m, err
}

// UpdateMessageContent 用历史接口暴露的富媒体分类与地址纠正已持久化占位消息。
// ctx 控制数据库更新生命周期；cookieID 和 key 定位消息；messageType 和 content 是新的非敏感展示内容。
func (s *ChatStore) UpdateMessageContent(ctx context.Context, cookieID, key, messageType, content string) error {
	// err 保存更新消息展示分类与内容地址时的数据库错误。
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET message_type=?,content=?
		WHERE cookie_id=? AND message_key=?`, messageType, content, cookieID, key)
	return err
}

// UpdateMessageMediaDuration 用历史载荷中的秒级时长补齐已持久化语音消息。
// ctx 控制数据库更新生命周期；cookieID 和 key 定位消息；duration 以秒为单位，零值代表平台未提供。
func (s *ChatStore) UpdateMessageMediaDuration(ctx context.Context, cookieID, key string, duration int64) error {
	// err 保存更新富媒体时长时的数据库错误，调用方据此决定是否返回历史刷新失败。
	_, err := s.DB.ExecContext(ctx, `UPDATE chat_messages SET media_duration=?
		WHERE cookie_id=? AND message_key=?`, duration, cookieID, key)
	return err
}

// ListSessions 读取Sessions。
func (s *ChatStore) ListSessions(ctx context.Context, userID int64, cookieID string, limit int) ([]ChatSession, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := s.DB.QueryContext(ctx, `SELECT cs.cookie_id,cs.chat_id,cs.buyer_id,cs.buyer_name,cs.buyer_avatar_url,
		cs.item_id,cs.item_title,cs.item_image_url,cs.last_message,cs.last_message_at,cs.unread_count,
		cs.account_role,cs.buyer_user_id,cs.seller_user_id,cs.role_item_id,cs.role_source
		FROM chat_sessions cs JOIN cookies c ON c.id=cs.cookie_id
		WHERE c.user_id=? AND cs.cookie_id=? AND cs.is_visible=? AND cs.user_hidden_at=0 ORDER BY cs.last_message_at DESC,cs.chat_id DESC LIMIT ?`, userID, cookieID, true, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 用于本次流程后续判断的结果
	var result []ChatSession
	for rows.Next() {
		// row 用于本次流程后续判断的row
		var row ChatSession
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&row.CookieID, &row.ChatID, &row.BuyerID, &row.BuyerName, &row.BuyerAvatar,
			&row.ItemID, &row.ItemTitle, &row.ItemImageURL, &row.LastMessage, &row.LastMessageAt, &row.UnreadCount,
			&row.AccountRole, &row.BuyerUserID, &row.SellerUserID, &row.RoleItemID, &row.RoleSource); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// FindSession 按用户归属、账号和会话标识精确读取一条可见会话。
// 返回 ErrNotFound 表示会话不存在、不可见或不属于当前用户。
func (s *ChatStore) FindSession(ctx context.Context, userID int64, cookieID, chatID string) (*ChatSession, error) {
	// session 保存从数据库扫描出的非敏感会话摘要。
	var session ChatSession
	// err 是带账号归属条件的单行查询结果。
	err := s.DB.QueryRowContext(ctx, `SELECT cs.cookie_id,cs.chat_id,cs.buyer_id,cs.buyer_name,cs.buyer_avatar_url,
		cs.item_id,cs.item_title,cs.item_image_url,cs.last_message,cs.last_message_at,cs.unread_count,
		cs.account_role,cs.buyer_user_id,cs.seller_user_id,cs.role_item_id,cs.role_source
		FROM chat_sessions cs JOIN cookies c ON c.id=cs.cookie_id
		WHERE c.user_id=? AND cs.cookie_id=? AND cs.chat_id=? AND cs.is_visible=? AND cs.user_hidden_at=0`, userID, cookieID, chatID, true).
		Scan(&session.CookieID, &session.ChatID, &session.BuyerID, &session.BuyerName, &session.BuyerAvatar,
			&session.ItemID, &session.ItemTitle, &session.ItemImageURL, &session.LastMessage, &session.LastMessageAt, &session.UnreadCount,
			&session.AccountRole, &session.BuyerUserID, &session.SellerUserID, &session.RoleItemID, &session.RoleSource)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// ListSessionPage 按用户归属读取本地聊天会话的稳定键集分页结果。
// cursor 为空时读取首页；limit 的默认值和最大值均为 200，避免单次列表过大。
func (s *ChatStore) ListSessionPage(ctx context.Context, userID int64, cookieID string, cursor *ChatSessionCursor, limit int) (ChatSessionPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	// query 保存归属过滤、可选键集条件和稳定排序共同构成的会话分页 SQL。
	query := `SELECT cs.cookie_id,cs.chat_id,cs.buyer_id,cs.buyer_name,cs.buyer_avatar_url,
		cs.item_id,cs.item_title,cs.item_image_url,cs.last_message,cs.last_message_at,cs.unread_count,
		cs.account_role,cs.buyer_user_id,cs.seller_user_id,cs.role_item_id,cs.role_source
		FROM chat_sessions cs JOIN cookies c ON c.id=cs.cookie_id
		WHERE c.user_id=? AND cs.cookie_id=? AND cs.is_visible=? AND cs.user_hidden_at=0`
	// args 保存与会话分页 SQL 占位符严格对应的非敏感查询参数。
	args := []any{userID, cookieID, true}
	if cursor != nil {
		query += ` AND (cs.last_message_at<? OR (cs.last_message_at=? AND cs.chat_id<?))`
		args = append(args, cursor.LastMessageAt, cursor.LastMessageAt, cursor.ChatID)
	}
	query += ` ORDER BY cs.last_message_at DESC,cs.chat_id DESC LIMIT ?`
	args = append(args, limit+1)
	// rows 和 queryErr 保存数据库游标及执行键集查询时的错误。
	rows, queryErr := s.DB.QueryContext(ctx, query, args...)
	if queryErr != nil {
		return ChatSessionPage{}, queryErr
	}
	defer rows.Close()
	// sessions 保存最多 limit 加一条的扫描结果，用额外记录判断是否还有下一页。
	sessions := make([]ChatSession, 0, limit+1)
	for rows.Next() {
		// session 保存当前扫描出的单个聊天会话非敏感摘要。
		var session ChatSession
		// scanErr 保存当前数据库行映射到会话摘要时的错误。
		if scanErr := rows.Scan(&session.CookieID, &session.ChatID, &session.BuyerID, &session.BuyerName, &session.BuyerAvatar,
			&session.ItemID, &session.ItemTitle, &session.ItemImageURL, &session.LastMessage, &session.LastMessageAt, &session.UnreadCount,
			&session.AccountRole, &session.BuyerUserID, &session.SellerUserID, &session.RoleItemID, &session.RoleSource); scanErr != nil {
			return ChatSessionPage{}, scanErr
		}
		sessions = append(sessions, session)
	}
	// iterationErr 保存数据库驱动在耗尽行流时报告的错误，不能把不完整页面视为成功。
	if iterationErr := rows.Err(); iterationErr != nil {
		return ChatSessionPage{}, iterationErr
	}
	// page 保存裁剪额外探测行后的分页响应。
	page := ChatSessionPage{Sessions: sessions}
	if len(sessions) > limit {
		page.HasMore = true
		page.Sessions = sessions[:limit]
	}
	if len(page.Sessions) > 0 && page.HasMore {
		// last 保存当前页最后一条会话，其排序键作为下一页的不透明游标来源。
		last := page.Sessions[len(page.Sessions)-1]
		page.NextCursor = &ChatSessionCursor{LastMessageAt: last.LastMessageAt, ChatID: last.ChatID}
	}
	return page, nil
}

// ListMessages 读取消息列表。
func (s *ChatStore) ListMessages(ctx context.Context, userID int64, cookieID, chatID string, beforeID int64, limit int) ([]ChatMessage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// query 保存按时间倒序读取再反转为时间正序的分页 SQL。
	query := `SELECT m.id,m.cookie_id,m.chat_id,m.message_key,m.direction,m.sender_id,m.sender_name,m.message_type,m.content,m.media_duration,m.status,m.read_status,m.read_at,m.sent_at
		FROM chat_messages m JOIN cookies c ON c.id=m.cookie_id
		WHERE c.user_id=? AND m.cookie_id=? AND m.chat_id=?`
	// args 用于本次流程后续判断的args
	args := []any{userID, cookieID, chatID}
	if beforeID > 0 {
		query += ` AND (m.sent_at < COALESCE((SELECT older.sent_at FROM chat_messages older WHERE older.id=? AND older.cookie_id=?), m.sent_at)
			OR (m.sent_at = COALESCE((SELECT same.sent_at FROM chat_messages same WHERE same.id=? AND same.cookie_id=?), m.sent_at) AND m.id<?))`
		args = append(args, beforeID, cookieID, beforeID, cookieID, beforeID)
	}
	query += ` ORDER BY m.sent_at DESC,m.id DESC LIMIT ?`
	args = append(args, limit)
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 用于本次流程后续判断的结果
	var result []ChatMessage
	for rows.Next() {
		// m 保存当前扫描出的消息及其本地已读状态。
		var m ChatMessage
		// err 保存当前行字段映射错误，避免返回缺少已读字段的不完整消息。
		if err := rows.Scan(&m.ID, &m.CookieID, &m.ChatID, &m.MessageKey, &m.Direction, &m.SenderID,
			&m.SenderName, &m.MessageType, &m.Content, &m.MediaDuration, &m.Status, &m.ReadStatus, &m.ReadAt, &m.SentAt); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	// API returns chronological order while the query remains index-friendly.
	for // i、j 用于本次流程后续判断的i、j
	i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, rows.Err()
}
