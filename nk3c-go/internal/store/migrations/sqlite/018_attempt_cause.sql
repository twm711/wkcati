-- +goose Up
ALTER TABLE cti_task_attempt ADD COLUMN failure_code TEXT;
ALTER TABLE cti_task_attempt ADD COLUMN failure_detail TEXT;

-- +goose Down
-- SQLite does not support DROP COLUMN on all supported versions.
