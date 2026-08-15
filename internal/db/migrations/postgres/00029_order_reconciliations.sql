-- +goose Up
CREATE TABLE IF NOT EXISTS order_reconciliations (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL,
    cookie_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_order_reconciliations_pending
    ON order_reconciliations(status, created_at, id);

-- +goose Down
DROP TABLE IF EXISTS order_reconciliations;
