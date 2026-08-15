-- +goose Up
CREATE TABLE IF NOT EXISTS order_reconciliations (
    id VARCHAR(36) PRIMARY KEY,
    order_id VARCHAR(255) NOT NULL,
    cookie_id VARCHAR(255) NOT NULL,
    kind VARCHAR(100) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_order_reconciliations_pending (status, created_at, id)
);

-- +goose Down
DROP TABLE IF EXISTS order_reconciliations;
