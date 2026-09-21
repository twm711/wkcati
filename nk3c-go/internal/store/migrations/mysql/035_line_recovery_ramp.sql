-- +goose Up
ALTER TABLE cti_outbound_line ADD COLUMN last_recovery_at DATETIME NULL;

-- +goose Down
ALTER TABLE cti_outbound_line DROP COLUMN last_recovery_at;
