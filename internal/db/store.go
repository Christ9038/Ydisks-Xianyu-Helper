package db

import "database/sql"

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
}

// NewStore 基于 *sql.DB 构造聚合 store。dialect 用于业务 SQL 方言分支。
func NewStore(db *sql.DB, dialect Dialect) *Store {
	return &Store{
		DB:             db,
		Dialect:        dialect,
		Users:          &Users{DB: db},
		Sessions:       &Sessions{DB: db},
		Cookies:        &Cookies{DB: db, Dialect: dialect},
		Items:          &Items{DB: db, Dialect: dialect},
		Cards:          &Cards{DB: db, Dialect: dialect},
		Automation:     &AutomationRules{DB: db, Dialect: dialect},
		Orders:         &Orders{DB: db, Dialect: dialect},
		Keywords:       &Keywords{DB: db, Dialect: dialect},
		DefaultReps:    &DefaultReplies{DB: db, Dialect: dialect},
		ItemReps:       &ItemReplies{DB: db, Dialect: dialect},
		AIReply:        &AIReply{DB: db},
		Notifications:  &Notifications{DB: db, Dialect: dialect},
		Settings:       &SystemSettings{DB: db, Dialect: dialect},
		WSMessages:     &WSMessageStore{DB: db},
		PublishBatches: &ItemPublishBatches{DB: db},
		Tokens:         &AccountTokens{DB: db, Dialect: dialect},
		Renewal:        &RenewalStore{DB: db, Dialect: dialect},
		LoginLogs:      &AccountLoginLogs{DB: db},
	}
}
