-- +goose Up
ALTER TABLE cti_outbound_line ADD COLUMN rate_window_start TEXT;
ALTER TABLE cti_outbound_line ADD COLUMN rate_window_count INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- SQLite compatibility: columns are retained on downgrade.
