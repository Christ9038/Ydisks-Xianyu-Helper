-- +goose Up
ALTER TABLE default_reply_records ADD COLUMN lease_expires_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE cookies ADD COLUMN paused_until BIGINT NOT NULL DEFAULT 0;
ALTER TABLE item_publish_batches
    ADD COLUMN worker_token VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN lease_expires_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE item_publish_batch_rows
    ADD COLUMN worker_token VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN failure_kind VARCHAR(32) NOT NULL DEFAULT '';

CREATE INDEX idx_publish_batches_lease
    ON item_publish_batches(status, lease_expires_at);
CREATE INDEX idx_publish_rows_claim
    ON item_publish_batch_rows(batch_id, status, id);

-- +goose Down
DROP INDEX idx_publish_rows_claim ON item_publish_batch_rows;
DROP INDEX idx_publish_batches_lease ON item_publish_batches;
ALTER TABLE item_publish_batch_rows DROP COLUMN failure_kind, DROP COLUMN worker_token;
ALTER TABLE item_publish_batches DROP COLUMN lease_expires_at, DROP COLUMN worker_token;
ALTER TABLE cookies DROP COLUMN paused_until;
ALTER TABLE default_reply_records DROP COLUMN lease_expires_at;
