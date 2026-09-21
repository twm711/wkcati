-- +goose Up
ALTER TABLE cti_outbound_line ADD COLUMN rate_limit_per_minute INT NOT NULL DEFAULT 30;

-- +goose Down
ALTER TABLE cti_outbound_line DROP COLUMN rate_limit_per_minute;
