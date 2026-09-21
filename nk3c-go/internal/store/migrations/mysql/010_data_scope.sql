-- +goose Up
ALTER TABLE sys_op_log ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE cti_monitor_event ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
CREATE INDEX idx_sys_op_log_tenant_created ON sys_op_log(tenant_id,created_at);
CREATE INDEX idx_cti_monitor_event_tenant_created ON cti_monitor_event(tenant_id,created_at);

-- +goose Down
DROP INDEX idx_sys_op_log_tenant_created ON sys_op_log;
DROP INDEX idx_cti_monitor_event_tenant_created ON cti_monitor_event;
ALTER TABLE sys_op_log DROP COLUMN tenant_id;
ALTER TABLE cti_monitor_event DROP COLUMN tenant_id;
