-- +goose Up
ALTER TABLE cti_dial_strategy ADD COLUMN current_multiplier DECIMAL(6,3) NOT NULL DEFAULT 1.0, ADD COLUMN last_sample_count INT NOT NULL DEFAULT 0;
INSERT IGNORE INTO sys_param(param_code,param_value) VALUES
 ('predict.min.samples','20'),
 ('predict.smoothing.alpha','0.30');

-- +goose Down
ALTER TABLE cti_dial_strategy DROP COLUMN current_multiplier, DROP COLUMN last_sample_count;
