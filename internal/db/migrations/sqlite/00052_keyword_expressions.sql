-- +goose Up
ALTER TABLE keywords ADD COLUMN keyword_expressions TEXT;
ALTER TABLE keywords ADD COLUMN match_type TEXT NOT NULL DEFAULT 'contains';

-- +goose Down
ALTER TABLE keywords DROP COLUMN match_type;
ALTER TABLE keywords DROP COLUMN keyword_expressions;