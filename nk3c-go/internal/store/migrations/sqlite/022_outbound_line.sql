-- +goose Up
CREATE TABLE IF NOT EXISTS cti_outbound_line(
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL DEFAULT 0,
  line_no TEXT NOT NULL,
  host TEXT,
  port INTEGER NOT NULL DEFAULT 5060,
  enabled INTEGER NOT NULL DEFAULT 1,
  priority INTEGER NOT NULL DEFAULT 100,
  capacity INTEGER NOT NULL DEFAULT 10,
  active_calls INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX idx_outbound_line_tenant_no ON cti_outbound_line(tenant_id,line_no);
CREATE INDEX idx_outbound_line_select ON cti_outbound_line(tenant_id,enabled,priority);

-- +goose Down
DROP TABLE cti_outbound_line;
