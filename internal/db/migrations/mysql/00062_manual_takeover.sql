-- +goose Up
ALTER TABLE cookies ADD COLUMN manual_takeover_minutes INT NOT NULL DEFAULT 0 AFTER consign_proof_text;
ALTER TABLE cookies ADD COLUMN manual_takeover_scope VARCHAR(16) NOT NULL DEFAULT 'ai' AFTER manual_takeover_minutes;
ALTER TABLE keywords ADD COLUMN notify_manual INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE keywords DROP COLUMN notify_manual;
ALTER TABLE cookies DROP COLUMN manual_takeover_scope;
ALTER TABLE cookies DROP COLUMN manual_takeover_minutes;
