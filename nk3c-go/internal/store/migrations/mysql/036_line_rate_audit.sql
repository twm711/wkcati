-- +goose Up
CREATE TABLE IF NOT EXISTS cti_line_rate_audit(
  id BIGINT PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  line_no VARCHAR(128) NOT NULL,
  operator_id BIGINT NOT NULL,
  old_rate INT NOT NULL,
  new_rate INT NOT NULL,
  reason VARCHAR(500) NOT NULL,
  created_at DATETIME NOT NULL,
  KEY idx_line_rate_audit_line(tenant_id,line_no,created_at)
);

-- +goose Down
DROP TABLE cti_line_rate_audit;
