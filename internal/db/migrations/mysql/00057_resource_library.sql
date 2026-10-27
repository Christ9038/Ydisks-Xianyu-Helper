-- +goose Up
-- 本地资源库：图片库元数据、商品模板，以及批量铺货明细的多规格快照。
CREATE TABLE IF NOT EXISTS library_images (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    filename VARCHAR(255) NOT NULL DEFAULT '',
    storage_key VARCHAR(512) NOT NULL,
    content_type VARCHAR(128) NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    sha256 CHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_library_images_user_sha (user_id, sha256),
    INDEX idx_library_images_user_created (user_id, created_at),
    CONSTRAINT fk_library_images_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS product_templates (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL DEFAULT '',
    title VARCHAR(255) NOT NULL DEFAULT '',
    description LONGTEXT NOT NULL,
    price_cents BIGINT NOT NULL DEFAULT 0,
    original_price_cents BIGINT NOT NULL DEFAULT 0,
    quantity INTEGER NOT NULL DEFAULT 1,
    postage_mode VARCHAR(32) NOT NULL DEFAULT 'free',
    postage_cents BIGINT NOT NULL DEFAULT 0,
    category_json LONGTEXT NOT NULL,
    image_ids_json LONGTEXT NOT NULL,
    specs_json LONGTEXT NOT NULL,
    automation_json LONGTEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_product_templates_user_updated (user_id, updated_at),
    CONSTRAINT fk_product_templates_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE item_publish_batch_rows ADD COLUMN specs_json LONGTEXT NULL;

-- +goose Down
ALTER TABLE item_publish_batch_rows DROP COLUMN specs_json;
DROP TABLE IF EXISTS product_templates;
DROP TABLE IF EXISTS library_images;
