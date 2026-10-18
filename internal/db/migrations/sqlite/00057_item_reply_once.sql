-- +goose Up
-- 旧商品默认关闭；空商品作用域保留已有账号会话去重事实。
ALTER TABLE item_replay ADD COLUMN reply_once INTEGER NOT NULL DEFAULT 0;
CREATE TABLE default_reply_records_scoped (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 cookie_id TEXT NOT NULL,
 chat_id TEXT NOT NULL,
 item_id TEXT NOT NULL DEFAULT '',
 replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
 status TEXT NOT NULL DEFAULT 'sent',
 text_sent INTEGER NOT NULL DEFAULT 1,
 image_sent INTEGER NOT NULL DEFAULT 1,
 last_error TEXT NOT NULL DEFAULT '',
 updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
 lease_expires_at INTEGER NOT NULL DEFAULT 0,
 UNIQUE(cookie_id,chat_id,item_id),
 FOREIGN KEY(cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
INSERT INTO default_reply_records_scoped (id,cookie_id,chat_id,replied_at,status,text_sent,image_sent,last_error,updated_at,lease_expires_at) SELECT id,cookie_id,chat_id,replied_at,status,text_sent,image_sent,last_error,updated_at,lease_expires_at FROM default_reply_records;
DROP TABLE default_reply_records;
ALTER TABLE default_reply_records_scoped RENAME TO default_reply_records;

-- +goose Down
CREATE TABLE default_reply_records_legacy (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 cookie_id TEXT NOT NULL,
 chat_id TEXT NOT NULL,

 replied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
 status TEXT NOT NULL DEFAULT 'sent',
 text_sent INTEGER NOT NULL DEFAULT 1,
 image_sent INTEGER NOT NULL DEFAULT 1,
 last_error TEXT NOT NULL DEFAULT '',
 updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
 lease_expires_at INTEGER NOT NULL DEFAULT 0,
 UNIQUE(cookie_id,chat_id),
 FOREIGN KEY(cookie_id) REFERENCES cookies(id) ON DELETE CASCADE
);
INSERT INTO default_reply_records_legacy (id,cookie_id,chat_id,replied_at,status,text_sent,image_sent,last_error,updated_at,lease_expires_at) SELECT id,cookie_id,chat_id,replied_at,status,text_sent,image_sent,last_error,updated_at,lease_expires_at FROM default_reply_records WHERE item_id='';
DROP TABLE default_reply_records;
ALTER TABLE default_reply_records_legacy RENAME TO default_reply_records;
ALTER TABLE item_replay DROP COLUMN reply_once;
