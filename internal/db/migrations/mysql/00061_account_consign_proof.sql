-- +goose Up
ALTER TABLE cookies ADD COLUMN consign_proof_mode VARCHAR(16) NOT NULL DEFAULT 'delivery' AFTER auto_bargain;
ALTER TABLE cookies ADD COLUMN consign_proof_text VARCHAR(255) NOT NULL DEFAULT '' AFTER consign_proof_mode;

-- +goose Down
ALTER TABLE cookies DROP COLUMN consign_proof_text;
ALTER TABLE cookies DROP COLUMN consign_proof_mode;
