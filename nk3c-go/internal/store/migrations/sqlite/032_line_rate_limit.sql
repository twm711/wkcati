-- +goose Up
ALTER TABLE cti_outbound_line ADD COLUMN rate_limit_per_minute INTEGER NOT NULL DEFAULT 30;

-- +goose Down
-- SQLite does not support portable DROP COLUMN across supported versions.
