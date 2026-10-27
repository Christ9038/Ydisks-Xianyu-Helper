-- +goose Up
-- 本地资源库：图片库元数据、商品模板，以及批量铺货明细的多规格快照。
CREATE TABLE IF NOT EXISTS library_images (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    filename TEXT NOT NULL DEFAULT '',
    storage_key TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    sha256 TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, sha256),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_library_images_user_created ON library_images(user_id, created_at);

CREATE TABLE IF NOT EXISTS product_templates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    price_cents INTEGER NOT NULL DEFAULT 0,
    original_price_cents INTEGER NOT NULL DEFAULT 0,
    quantity INTEGER NOT NULL DEFAULT 1,
    postage_mode TEXT NOT NULL DEFAULT 'free',
    postage_cents INTEGER NOT NULL DEFAULT 0,
    category_json TEXT NOT NULL DEFAULT '{}',
    image_ids_json TEXT NOT NULL DEFAULT '[]',
    specs_json TEXT NOT NULL DEFAULT '{}',
    automation_json TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_product_templates_user_updated ON product_templates(user_id, updated_at);

ALTER TABLE item_publish_batch_rows ADD COLUMN specs_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE item_publish_batch_rows DROP COLUMN specs_json;
DROP TABLE IF EXISTS product_templates;
DROP TABLE IF EXISTS library_images;
