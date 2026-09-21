-- +goose Up
ALTER TABLE cti_outbound_line ADD COLUMN rate_window_start DATETIME NULL;
ALTER TABLE cti_outbound_line ADD COLUMN rate_window_count INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE cti_outbound_line DROP COLUMN rate_window_start, DROP COLUMN rate_window_count;
