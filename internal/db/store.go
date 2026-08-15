package db

import (
	"database/sql"
	"sync"
)

// Store 聚合各 repository，供上层（HTTP server、account supervisor 等）统一持有。
type Store struct {
	DB              *sql.DB
	Dialect         Dialect
	Users           *Users
	Sessions        *Sessions
	Cookies         *Cookies
	Items           *Items
	Cards           *Cards
	Automation      *AutomationRules
	Orders          *Orders
	Reconciliations *OrderReconciliations
	Keywords        *Keywords
	DefaultReps     *DefaultReplies
	ItemReps        *ItemReplies
	AIReply         *AIReply
	Notifications   *Notifications
	Settings        *SystemSettings
	UserSettings    *UserSettings
	WSMessages      *WSMessageStore
	PublishBatches  *ItemPublishBatches
	Tokens          *AccountTokens
	Renewal         *RenewalStore
	LoginLogs       *AccountLoginLogs
	RiskLogs        *RiskControlLogs
	Chats           *ChatStore
	AccountTasks    *AccountTaskStore
	Admin           *AdminQueries
	Analytics       *AnalyticsQueries

	credentialMu    sync.Mutex
	credentialLocks map[string]*credentialLockEntry
}

// credentialLockEntry 保存单个账号凭证锁及当前排队/持有者数量。
type credentialLockEntry struct {
	// mu 串行化该账号的 Cookie、token 和 metadata 状态变更。
	mu sync.Mutex
	// refs 记录仍可能使用该 entry 的调用方数量，用于安全回收空闲锁。
	refs int
}

// NewStore 基于 *sql.DB 构造聚合 store。dialect 用于业务 SQL 方言分支。
func NewStore(db *sql.DB, dialect Dialect) *Store {
	// codec 保存codec，供当前处理流程使用
	codec := secretCodecFromEnvironment()
	return &Store{
		DB:              db,
		Dialect:         dialect,
		Users:           &Users{DB: db},
		Sessions:        &Sessions{DB: db},
		Cookies:         &Cookies{DB: db, Dialect: dialect, codec: codec},
		Items:           &Items{DB: db, Dialect: dialect},
		Cards:           &Cards{DB: db, Dialect: dialect},
		Automation:      &AutomationRules{DB: db, Dialect: dialect},
		Orders:          &Orders{DB: db, Dialect: dialect},
		Reconciliations: &OrderReconciliations{DB: db},
		Keywords:        &Keywords{DB: db, Dialect: dialect},
		DefaultReps:     &DefaultReplies{DB: db, Dialect: dialect},
		ItemReps:        &ItemReplies{DB: db, Dialect: dialect},
		AIReply:         &AIReply{DB: db, Dialect: dialect, codec: codec},
		Notifications:   &Notifications{DB: db, Dialect: dialect, codec: codec},
		Settings:        &SystemSettings{DB: db, Dialect: dialect, codec: codec},
		UserSettings:    &UserSettings{DB: db, Dialect: dialect},
		WSMessages:      &WSMessageStore{DB: db},
		PublishBatches:  &ItemPublishBatches{DB: db},
		Tokens:          &AccountTokens{DB: db, Dialect: dialect, codec: codec},
		Renewal:         &RenewalStore{DB: db, Dialect: dialect},
		LoginLogs:       &AccountLoginLogs{DB: db},
		RiskLogs:        &RiskControlLogs{DB: db, Dialect: dialect},
		Chats:           &ChatStore{DB: db, Dialect: dialect},
		AccountTasks:    &AccountTaskStore{DB: db, Dialect: dialect},
		Admin:           &AdminQueries{DB: db},
		Analytics:       &AnalyticsQueries{DB: db},
		credentialLocks: make(map[string]*credentialLockEntry),
	}
}

// LockAccountCredentials serializes Cookie/token state transitions for one
// account across the IM runtime and renewal scheduler. The returned function
// must be called exactly once.
// LockAccountCredentials 负责锁账号Credentials相关处理。
func (s *Store) LockAccountCredentials(cookieID string) func() {
	if s == nil {
		return func() {}
	}
	s.credentialMu.Lock()
	if s.credentialLocks == nil {
		s.credentialLocks = make(map[string]*credentialLockEntry)
	}
	// entry 保存账号锁及其引用计数。
	entry := s.credentialLocks[cookieID]
	if entry == nil {
		entry = &credentialLockEntry{}
		s.credentialLocks[cookieID] = entry
	}
	entry.refs++
	s.credentialMu.Unlock()
	entry.mu.Lock()
	// unlocked 防止异常重复调用释放函数破坏锁和引用计数。
	unlocked := false
	// unlockMu 保护释放函数的幂等状态。
	var unlockMu sync.Mutex
	return func() {
		unlockMu.Lock()
		if unlocked {
			unlockMu.Unlock()
			return
		}
		unlocked = true
		unlockMu.Unlock()
		entry.mu.Unlock()
		s.credentialMu.Lock()
		entry.refs--
		if entry.refs == 0 && s.credentialLocks[cookieID] == entry {
			delete(s.credentialLocks, cookieID)
		}
		s.credentialMu.Unlock()
	}
}
