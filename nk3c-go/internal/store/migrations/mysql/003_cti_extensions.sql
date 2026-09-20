-- +goose Up
ALTER TABLE cti_call_record ADD COLUMN record_file TEXT NULL;
ALTER TABLE ans_answer ADD COLUMN aud_start DOUBLE NULL, ADD COLUMN aud_end DOUBLE NULL;
ALTER TABLE ivr_call_log ADD COLUMN record_file TEXT NULL;

-- +goose Down
ALTER TABLE cti_call_record DROP COLUMN record_file;
ALTER TABLE ans_answer DROP COLUMN aud_start, DROP COLUMN aud_end;
ALTER TABLE ivr_call_log DROP COLUMN record_file;
