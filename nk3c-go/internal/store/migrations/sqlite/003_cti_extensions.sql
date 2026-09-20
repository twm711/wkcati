-- +goose Up
-- M3 CTI 扩展列：录音索引与答题质检时间，独立于核心表迁移，便于旧库增量升级。
ALTER TABLE cti_call_record ADD COLUMN record_file TEXT;
ALTER TABLE ans_answer ADD COLUMN aud_start REAL;
ALTER TABLE ans_answer ADD COLUMN aud_end REAL;
ALTER TABLE ivr_call_log ADD COLUMN record_file TEXT;

-- +goose Down
-- SQLite 不支持安全 DROP COLUMN；演示环境通过 --reset 回滚。
