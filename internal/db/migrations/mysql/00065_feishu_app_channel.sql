-- +goose Up
-- MySQL 初始表从未给 type 建 CHECK 约束；这里保留编号对齐，不修改历史表结构。
SELECT 1;

-- +goose Down
SELECT 1;
