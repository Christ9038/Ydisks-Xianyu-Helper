-- +goose Up
-- 图片库引用：各图片回复位置可引用本地图片库图片，与原有图片地址互斥；不加跨表外键，引用完整性由删除前检查保证。
ALTER TABLE keywords ADD COLUMN library_image_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE default_replies ADD COLUMN library_image_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE item_greeting_rules ADD COLUMN library_image_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cards ADD COLUMN library_image_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE chat_quick_replies ADD COLUMN library_image_id INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE chat_quick_replies DROP COLUMN library_image_id;
ALTER TABLE cards DROP COLUMN library_image_id;
ALTER TABLE item_greeting_rules DROP COLUMN library_image_id;
ALTER TABLE default_replies DROP COLUMN library_image_id;
ALTER TABLE keywords DROP COLUMN library_image_id;
