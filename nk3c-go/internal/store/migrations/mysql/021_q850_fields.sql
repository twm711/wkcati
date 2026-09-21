-- +goose Up
ALTER TABLE cti_task_attempt ADD COLUMN q850_cause INT, ADD COLUMN q850_text VARCHAR(255);

-- +goose Down
ALTER TABLE cti_task_attempt DROP COLUMN q850_cause, DROP COLUMN q850_text;
