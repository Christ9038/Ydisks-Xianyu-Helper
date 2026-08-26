-- +goose Up
-- +goose StatementBegin
ALTER TABLE automation_runs ADD COLUMN delivery_proof TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE automation_runs DROP COLUMN delivery_proof;
-- +goose StatementEnd
