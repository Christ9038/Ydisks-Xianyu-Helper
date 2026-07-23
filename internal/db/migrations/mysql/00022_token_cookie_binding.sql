-- +goose Up
ALTER TABLE account_tokens ADD COLUMN cookie_fingerprint VARCHAR(64) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE account_tokens DROP COLUMN cookie_fingerprint;
