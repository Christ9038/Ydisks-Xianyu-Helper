package db

import (
	"database/sql"
	"sync"
)

// Store 聚合各 repository，供上层（HTTP server、account supervisor 等）统一持有。
type Store struct {
	DB             *sql.DB
	Dialect        Dialect
	Users          *Users
	Sessions       *Sessions
	Cookies        *Cookies
	Items          *Items
	Cards          *Cards
	Automation     *AutomationRules
	Orders         *Orders
	Keywords       *Keywords
	DefaultReps    *DefaultReplies
	ItemReps       *ItemReplies
	AIReply        *AIReply
	Notifications  *Notifications
	Settings       *SystemSettings
	WSMessages     *WSMessageStore
	PublishBatches *ItemPublishBatches
	Tokens         *AccountTokens
	Renewal        *RenewalStore
	LoginLogs      *AccountLoginLogs
	RiskLogs       *RiskControlLogs

	credentialMu    sync.Mutex
	credentialLocks map[string]*sync.Mutex
}

// NewStore 基于 *sql.DB 构造聚合 store。dialect 用于业务 SQL 方言分支。
func NewStore(db *sql.DB, dialect Dialect) *Store {
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
		Keywords:        &Keywords{DB: db, Dialect: dialect},
		DefaultReps:     &DefaultReplies{DB: db, Dialect: dialect},
		ItemReps:        &ItemReplies{DB: db, Dialect: dialect},
		AIReply:         &AIReply{DB: db, codec: codec},
		Notifications:   &Notifications{DB: db, Dialect: dialect, codec: codec},
		Settings:        &SystemSettings{DB: db, Dialect: dialect, codec: codec},
		WSMessages:      &WSMessageStore{DB: db},
		PublishBatches:  &ItemPublishBatches{DB: db},
		Tokens:          &AccountTokens{DB: db, Dialect: dialect, codec: codec},
		Renewal:         &RenewalStore{DB: db, Dialect: dialect},
		LoginLogs:       &AccountLoginLogs{DB: db},
		RiskLogs:        &RiskControlLogs{DB: db, Dialect: dialect},
		credentialLocks: make(map[string]*sync.Mutex),
	}
}

// LockAccountCredentials serializes Cookie/token state transitions for one
// account across the IM runtime and renewal scheduler. The returned function
// must be called exactly once.
func (s *Store) LockAccountCredentials(cookieID string) func() {
	if s == nil {
		return func() {}
	}
	s.credentialMu.Lock()
	if s.credentialLocks == nil {
		s.credentialLocks = make(map[string]*sync.Mutex)
	}
	lock := s.credentialLocks[cookieID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.credentialLocks[cookieID] = lock
	}
	s.credentialMu.Unlock()
	lock.Lock()
	return lock.Unlock
}
