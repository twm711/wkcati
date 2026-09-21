-- +goose Up
CREATE TABLE IF NOT EXISTS cti_outbound_line(
  id BIGINT PRIMARY KEY,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  line_no VARCHAR(64) NOT NULL,
  host VARCHAR(255),
  port INT NOT NULL DEFAULT 5060,
  enabled TINYINT NOT NULL DEFAULT 1,
  priority INT NOT NULL DEFAULT 100,
  capacity INT NOT NULL DEFAULT 10,
  active_calls INT NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  UNIQUE KEY idx_outbound_line_tenant_no(tenant_id,line_no),
  KEY idx_outbound_line_select(tenant_id,enabled,priority)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE cti_outbound_line;
