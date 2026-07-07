-- +goose Up
ALTER TABLE keywords ADD COLUMN id BIGSERIAL PRIMARY KEY;

-- +goose Down
ALTER TABLE keywords DROP COLUMN id;
