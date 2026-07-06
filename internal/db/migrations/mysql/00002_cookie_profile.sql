-- +goose Up
-- +goose StatementBegin
ALTER TABLE cookies ADD COLUMN nickname VARCHAR(255) DEFAULT '';
ALTER TABLE cookies ADD COLUMN avatar_url TEXT;
ALTER TABLE cookies ADD COLUMN updated_at TIMESTAMP;
UPDATE cookies SET updated_at = COALESCE(updated_at, created_at, CURRENT_TIMESTAMP);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cookies DROP COLUMN updated_at;
ALTER TABLE cookies DROP COLUMN avatar_url;
ALTER TABLE cookies DROP COLUMN nickname;
-- +goose StatementEnd
