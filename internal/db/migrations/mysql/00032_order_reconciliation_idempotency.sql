-- +goose Up
ALTER TABLE order_reconciliations ADD COLUMN idempotency_key VARCHAR(700) NULL;
CREATE UNIQUE INDEX idx_order_reconciliations_idempotency
    ON order_reconciliations(idempotency_key);

-- +goose Down
DROP INDEX idx_order_reconciliations_idempotency ON order_reconciliations;
ALTER TABLE order_reconciliations DROP COLUMN idempotency_key;
