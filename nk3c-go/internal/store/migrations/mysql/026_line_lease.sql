-- +goose Up
CREATE TABLE IF NOT EXISTS cti_outbound_line_lease(
  call_id BIGINT PRIMARY KEY,
  line_id BIGINT NOT NULL,
  lease_until DATETIME NOT NULL,
  created_at DATETIME NOT NULL,
  KEY idx_line_lease_until(lease_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE cti_outbound_line_lease;
