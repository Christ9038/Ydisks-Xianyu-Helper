-- +goose Up
-- ws_messages 表在 Postgres 版 00001 已创建，此迁移为空操作，保留版本号与 SQLite 对齐。

-- +goose Down
-- 空操作：ws_messages 在 00001 创建，回滚由 00001 Down 处理。
