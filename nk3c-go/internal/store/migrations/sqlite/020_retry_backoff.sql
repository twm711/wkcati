-- +goose Up
ALTER TABLE smp_sample ADD COLUMN next_attempt_at TEXT;
CREATE INDEX idx_sample_next_attempt ON smp_sample(status,next_attempt_at);

-- +goose Down
-- SQLite compatibility: column is retained on downgrade.
