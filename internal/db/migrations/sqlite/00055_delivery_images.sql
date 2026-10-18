-- +goose Up
ALTER TABLE cards ADD COLUMN image_path TEXT NOT NULL DEFAULT '';
ALTER TABLE delivery_template_messages ADD COLUMN type TEXT NOT NULL DEFAULT 'text';
ALTER TABLE delivery_template_messages ADD COLUMN image_url TEXT;
ALTER TABLE delivery_template_messages ADD COLUMN image_path TEXT;

-- +goose Down
ALTER TABLE delivery_template_messages DROP COLUMN image_path;
ALTER TABLE delivery_template_messages DROP COLUMN image_url;
ALTER TABLE delivery_template_messages DROP COLUMN type;
ALTER TABLE cards DROP COLUMN image_path;
