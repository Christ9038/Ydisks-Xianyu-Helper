-- +goose Up
-- +goose StatementBegin
ALTER TABLE automation_runs ADD COLUMN delivery_proof LONGTEXT NULL;
UPDATE automation_runs SET delivery_proof='' WHERE delivery_proof IS NULL;
ALTER TABLE automation_runs MODIFY COLUMN delivery_proof LONGTEXT NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE automation_runs DROP COLUMN delivery_proof;
-- +goose StatementEnd
