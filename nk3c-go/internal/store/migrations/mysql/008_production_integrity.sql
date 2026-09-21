-- +goose Up
-- M3/P0: repair databases created before generated IDs and widen JSON/text payloads.
-- Fresh installs already receive these properties from 001_init.sql; MODIFY is
-- intentionally idempotent for the supported MySQL 8 schema.
ALTER TABLE prj_project MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE qnr_questionnaire MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE qnr_question MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE qnr_option MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE qnr_quota MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE qnr_quota_cell MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE smp_sample MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE smp_phone MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE smp_blacklist MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE cti_call_record MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE ans_sheet MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE ans_answer MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE wko_ticket MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE ivr_flow MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE ivr_call_log MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE cti_monitor_event MODIFY COLUMN id BIGINT NOT NULL AUTO_INCREMENT;
ALTER TABLE sys_op_log MODIFY COLUMN id VARCHAR(32) NOT NULL;
ALTER TABLE qnr_quota_cell MODIFY COLUMN conditions_json TEXT;
ALTER TABLE smp_sample MODIFY COLUMN ext_json TEXT;
ALTER TABLE wko_ticket MODIFY COLUMN detail TEXT;
ALTER TABLE ivr_flow MODIFY COLUMN flow_json TEXT;
ALTER TABLE ivr_call_log MODIFY COLUMN path_json TEXT, MODIFY COLUMN answers_json TEXT;

INSERT IGNORE INTO smp_status_code(code,name,category,closes_call,reopen_sample,hit_black_flag) VALUES
('SUCCESS','访问成功','SUCCESS',1,0,0),
('PARTIAL','部分完成','NEUTRAL',1,1,0),
('QUFAIL','甄别不合格','FAIL',1,0,0),
('REFUSE','拒访','FAIL',1,1,1),
('BREAKOFF','中途挂断','FAIL',1,1,0),
('APPOINT','预约回拨','APPOINT',0,0,0),
('NA','无人接听','FAIL',0,1,0),
('BUSY','占线','FAIL',0,1,0),
('INVALID','空号','FAIL',1,0,1),
('FAX','传真/数据线','FAIL',1,0,0);

-- +goose Down
-- AUTO_INCREMENT and TEXT widening are retained on rollback; reference rows are
-- application data and are not deleted by a schema rollback.
