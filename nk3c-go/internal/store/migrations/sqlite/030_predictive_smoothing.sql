-- +goose Up
ALTER TABLE cti_dial_strategy ADD COLUMN current_multiplier REAL NOT NULL DEFAULT 1.0;
ALTER TABLE cti_dial_strategy ADD COLUMN last_sample_count INTEGER NOT NULL DEFAULT 0;
INSERT OR IGNORE INTO sys_param(param_code,param_value) VALUES
 ('predict.min.samples','20'),
 ('predict.smoothing.alpha','0.30');

-- +goose Down
-- SQLite compatibility: columns are retained on downgrade.
