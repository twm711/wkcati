-- +goose Up
ALTER TABLE ivr_call_log ADD COLUMN project_id BIGINT NOT NULL DEFAULT 1;
CREATE INDEX idx_ivr_call_log_project ON ivr_call_log(project_id);

-- +goose Down
DROP INDEX idx_ivr_call_log_project ON ivr_call_log;
ALTER TABLE ivr_call_log DROP COLUMN project_id;
