-- +goose Up
ALTER TABLE cti_outbound_line ADD COLUMN last_recovery_at TEXT;

-- +goose Down
-- SQLite compatibility: column is retained on downgrade.
