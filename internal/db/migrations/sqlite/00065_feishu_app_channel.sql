-- +goose NO TRANSACTION
-- +goose Up
-- SQLite 不支持修改 CHECK 约束，只能在关闭外键检查后重建表；旧数据与自增主键原样保留。
PRAGMA foreign_keys=OFF;
CREATE TABLE notification_channels_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('qq','ding_talk','dingtalk','feishu','feishu_app','lark','bark','email','webhook','wechat','telegram')),
    config TEXT NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    user_id INTEGER NOT NULL DEFAULT 1,
    event_types TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
INSERT INTO notification_channels_new (id, name, type, config, enabled, user_id, event_types, created_at, updated_at)
SELECT id, name, type, config, enabled, user_id, event_types, created_at, updated_at FROM notification_channels;
DROP TABLE notification_channels;
ALTER TABLE notification_channels_new RENAME TO notification_channels;
PRAGMA foreign_keys=ON;

-- +goose Down
-- 回滚前先移除新类型渠道，避免旧 CHECK 约束导致数据复制失败；仍在重建表并保留其余数据。
PRAGMA foreign_keys=OFF;
CREATE TABLE notification_channels_rollback (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('qq','ding_talk','dingtalk','feishu','lark','bark','email','webhook','wechat','telegram')),
    config TEXT NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    user_id INTEGER NOT NULL DEFAULT 1,
    event_types TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
INSERT INTO notification_channels_rollback (id, name, type, config, enabled, user_id, event_types, created_at, updated_at)
SELECT id, name, type, config, enabled, user_id, event_types, created_at, updated_at FROM notification_channels WHERE type <> 'feishu_app';
DROP TABLE notification_channels;
ALTER TABLE notification_channels_rollback RENAME TO notification_channels;
PRAGMA foreign_keys=ON;
