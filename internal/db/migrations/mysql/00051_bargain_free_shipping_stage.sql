-- +goose Up
CREATE TABLE bargain_free_shipping_stages (
    order_id VARCHAR(255) NOT NULL,
    cookie_id VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL,
    updated_at VARCHAR(64) NOT NULL,
    PRIMARY KEY(order_id, cookie_id),
    INDEX idx_bargain_free_shipping_stages_status(status, order_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE bargain_free_shipping_stages;
