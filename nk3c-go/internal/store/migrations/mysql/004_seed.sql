-- +goose Up
-- MySQL 演练种子：生产环境应由独立初始化/密钥管理流程覆盖默认密码。
INSERT INTO sys_user(id,login_name,password,user_name,agent_no,roles,status) VALUES
(1,'admin','123456','系统管理员',NULL,'domainAdmin,orgAdmin,groupAdmin,phoneAdmin',1),
(2,'agent01','123456','坐席演示','1020','phoneAdmin',1),
(3,'sup01','123456','督导演示',NULL,'groupAdmin',1);
INSERT INTO sys_param(param_code,param_value) VALUES
('halfyear.days','180'),('redial.max','3'),('predict.abandon.max','3.0');

-- +goose Down
DELETE FROM sys_param WHERE param_code IN ('halfyear.days','redial.max','predict.abandon.max');
DELETE FROM sys_user WHERE id IN (1,2,3);
