-- +goose Up
ALTER TABLE cti_task_attempt ADD COLUMN failure_code VARCHAR(64), ADD COLUMN failure_detail VARCHAR(255);

-- +goose Down
ALTER TABLE cti_task_attempt DROP COLUMN failure_code, DROP COLUMN failure_detail;
