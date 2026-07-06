-- +goose Up
-- ws_messages 表在 MySQL 版 00001 已创建，此迁移为空操作，保留版本号与 SQLite 对齐。
-- MySQL 不允许重复创建已存在的外键约束，故此处不执行任何 DDL。

-- +goose Down
-- 空操作：ws_messages 在 00001 创建，回滚由 00001 Down 处理。
