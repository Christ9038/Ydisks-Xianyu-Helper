-- +goose Up
-- 卡密售出台账：记录每条自动发出的卡密内容及其订单，不引用 cards 外键，卡密组删除后历史仍可核对。
CREATE TABLE IF NOT EXISTS card_sale_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    card_id INTEGER NOT NULL,
    card_name TEXT NOT NULL DEFAULT '',
    card_type TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'reserved' CHECK (status IN ('reserved', 'sold')),
    run_id INTEGER NOT NULL DEFAULT 0,
    cookie_id TEXT NOT NULL DEFAULT '',
    order_id TEXT NOT NULL DEFAULT '',
    item_id TEXT NOT NULL DEFAULT '',
    buyer_id TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    sold_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_card_sale_records_card ON card_sale_records(user_id, card_id, status, id);
CREATE INDEX IF NOT EXISTS idx_card_sale_records_order ON card_sale_records(cookie_id, order_id);
CREATE INDEX IF NOT EXISTS idx_card_sale_records_run ON card_sale_records(run_id);

-- +goose Down
DROP TABLE IF EXISTS card_sale_records;
