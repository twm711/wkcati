-- +goose Up
ALTER TABLE cti_sample_task ADD COLUMN retry_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cti_sample_task ADD COLUMN max_retries INTEGER NOT NULL DEFAULT 3;
CREATE INDEX idx_sample_task_retry ON cti_sample_task(status,retry_count);

-- +goose Down
DROP INDEX idx_sample_task_retry;
