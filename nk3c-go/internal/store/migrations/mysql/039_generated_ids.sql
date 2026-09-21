-- +goose Up
-- P0: tables whose application paths use LastInsertId or create records concurrently.
ALTER TABLE cti_waiting_task MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE cti_line_rate_audit MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;

-- +goose Down
-- AUTO_INCREMENT is retained on rollback for safety.
