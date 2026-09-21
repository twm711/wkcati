-- +goose Up
INSERT OR IGNORE INTO sys_param(param_code,param_value) VALUES
 ('predict.window.minutes','15'),
 ('predict.multiplier.min','0.50'),
 ('predict.multiplier.max','3.00');

-- +goose Down
DELETE FROM sys_param WHERE param_code LIKE 'predict.%';
