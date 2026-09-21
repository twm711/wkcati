-- +goose Up
ALTER TABLE cti_outbound_line ADD COLUMN circuit_state TEXT NOT NULL DEFAULT 'CLOSED';
ALTER TABLE cti_outbound_line ADD COLUMN failure_streak INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cti_outbound_line ADD COLUMN opened_until TEXT;

-- +goose Down
-- SQLite compatibility: columns are retained on downgrade.
