-- +goose Up
-- Defensive creation supports databases that recorded 004 before the audit table existed.
CREATE TABLE IF NOT EXISTS sys_op_log(
  id TEXT PRIMARY KEY, user_id INTEGER, login_name TEXT, method TEXT,
  path TEXT, action TEXT, status_code INTEGER, created_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_sys_op_log_created ON sys_op_log(created_at);
ALTER TABLE sys_op_log ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE cti_monitor_event ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1;
CREATE INDEX idx_sys_op_log_tenant_created ON sys_op_log(tenant_id,created_at);
CREATE INDEX idx_cti_monitor_event_tenant_created ON cti_monitor_event(tenant_id,created_at);

-- +goose Down
DROP INDEX idx_sys_op_log_tenant_created;
DROP INDEX idx_cti_monitor_event_tenant_created;
