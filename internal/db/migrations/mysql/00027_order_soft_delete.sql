-- +goose Up
ALTER TABLE orders ADD COLUMN deleted_at TIMESTAMP NULL DEFAULT NULL;
CREATE INDEX idx_orders_active ON orders(cookie_id, deleted_at);

-- +goose Down
DROP INDEX idx_orders_active ON orders;
ALTER TABLE orders DROP COLUMN deleted_at;
