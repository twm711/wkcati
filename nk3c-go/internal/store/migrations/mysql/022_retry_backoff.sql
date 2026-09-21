-- +goose Up
ALTER TABLE smp_sample ADD COLUMN next_attempt_at DATETIME NULL;
CREATE INDEX idx_sample_next_attempt ON smp_sample(status,next_attempt_at);

-- +goose Down
ALTER TABLE smp_sample DROP COLUMN next_attempt_at;
