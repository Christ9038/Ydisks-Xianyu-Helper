-- +goose Up
ALTER TABLE cookies ADD COLUMN consign_proof_mode TEXT NOT NULL DEFAULT 'delivery';
ALTER TABLE cookies ADD COLUMN consign_proof_text TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cookies DROP COLUMN consign_proof_text;
ALTER TABLE cookies DROP COLUMN consign_proof_mode;
