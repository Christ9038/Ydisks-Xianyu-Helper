-- +goose Up
-- 卡密售出台账：记录每条自动发出的卡密内容及其订单，不引用 cards 外键，卡密组删除后历史仍可核对。
CREATE TABLE IF NOT EXISTS card_sale_records (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    card_id BIGINT NOT NULL,
    card_name VARCHAR(255) NOT NULL DEFAULT '',
    card_type VARCHAR(32) NOT NULL DEFAULT '',
    content LONGTEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'reserved',
    run_id BIGINT NOT NULL DEFAULT 0,
    cookie_id VARCHAR(191) NOT NULL DEFAULT '',
    order_id VARCHAR(191) NOT NULL DEFAULT '',
    item_id VARCHAR(191) NOT NULL DEFAULT '',
    buyer_id VARCHAR(191) NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    sold_at BIGINT NOT NULL DEFAULT 0,
    CONSTRAINT chk_card_sale_records_status CHECK (status IN ('reserved', 'sold')),
    INDEX idx_card_sale_records_card (user_id, card_id, status, id),
    INDEX idx_card_sale_records_order (cookie_id, order_id),
    INDEX idx_card_sale_records_run (run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE IF EXISTS card_sale_records;
