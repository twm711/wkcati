-- +goose Up
ALTER TABLE cti_outbound_line ADD COLUMN circuit_state VARCHAR(16) NOT NULL DEFAULT 'CLOSED', ADD COLUMN failure_streak INT NOT NULL DEFAULT 0, ADD COLUMN opened_until DATETIME NULL;

-- +goose Down
ALTER TABLE cti_outbound_line DROP COLUMN circuit_state, DROP COLUMN failure_streak, DROP COLUMN opened_until;
