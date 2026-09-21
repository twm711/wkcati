-- +goose Up
CREATE TABLE IF NOT EXISTS cti_line_rate_audit(
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  line_no TEXT NOT NULL,
  operator_id INTEGER NOT NULL,
  old_rate INTEGER NOT NULL,
  new_rate INTEGER NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_line_rate_audit_line ON cti_line_rate_audit(tenant_id,line_no,created_at);

-- +goose Down
DROP TABLE cti_line_rate_audit;
