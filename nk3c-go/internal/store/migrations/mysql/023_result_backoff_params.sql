-- +goose Up
INSERT IGNORE INTO sys_param(param_code,param_value) VALUES
 ('redial.backoff.enabled','0'),
 ('redial.delay.BUSY','60'),
 ('redial.delay.NA','120'),
 ('redial.delay.REFUSE','600'),
 ('redial.delay.BREAKOFF','300'),
 ('redial.delay.PARTIAL','300');

-- +goose Down
DELETE FROM sys_param WHERE param_code LIKE 'redial.delay.%';
