-- +goose Up
-- 统一操作审计：记录业务写操作与敏感导出访问，不记录请求体中的 PII。
CREATE TABLE sys_op_log(
  id TEXT PRIMARY KEY,
  user_id INTEGER,
  login_name TEXT,
  method TEXT,
  path TEXT,
  action TEXT,
  status_code INTEGER,
  created_at TEXT
);
CREATE INDEX idx_sys_op_log_created ON sys_op_log(created_at);

-- +goose Down
DROP TABLE sys_op_log;
