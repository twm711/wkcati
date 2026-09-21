-- +goose Up
ALTER TABLE cti_task_attempt ADD COLUMN q850_cause INTEGER;
ALTER TABLE cti_task_attempt ADD COLUMN q850_text TEXT;

-- +goose Down
-- SQLite compatibility: columns are retained on downgrade.
