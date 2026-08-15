-- +goose Up
CREATE INDEX idx_orders_cursor ON orders(cookie_id, deleted_at, created_at DESC, order_id DESC);

-- +goose Down
DROP INDEX idx_orders_cursor ON orders;
