package db

import "database/sql"

// Store 聚合各 repository，供上层（HTTP server、account supervisor 等）统一持有。
type Store struct {
	DB             *sql.DB
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
}

// NewStore 基于 *sql.DB 构造聚合 store。
func NewStore(db *sql.DB) *Store {
	return &Store{
		DB:             db,
		Users:          &Users{DB: db},
		Sessions:       &Sessions{DB: db},
		Cookies:        &Cookies{DB: db},
		Items:          &Items{DB: db},
		Cards:          &Cards{DB: db},
		Automation:     &AutomationRules{DB: db},
		Orders:         &Orders{DB: db},
		Keywords:       &Keywords{DB: db},
		DefaultReps:    &DefaultReplies{DB: db},
		ItemReps:       &ItemReplies{DB: db},
		AIReply:        &AIReply{DB: db},
		Notifications:  &Notifications{DB: db},
		Settings:       &SystemSettings{DB: db},
		WSMessages:     &WSMessageStore{DB: db},
		PublishBatches: &ItemPublishBatches{DB: db},
	}
}
