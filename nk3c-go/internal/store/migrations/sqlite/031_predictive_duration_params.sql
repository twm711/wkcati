-- +goose Up
INSERT OR IGNORE INTO sys_param(param_code,param_value) VALUES
 ('predict.avg.duration.seconds','180'),
 ('predict.avg.connect.seconds','20');

-- +goose Down
DELETE FROM sys_param WHERE param_code IN ('predict.avg.duration.seconds','predict.avg.connect.seconds');
