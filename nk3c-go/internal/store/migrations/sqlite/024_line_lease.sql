-- +goose Up
CREATE TABLE IF NOT EXISTS cti_outbound_line_lease(
  call_id INTEGER PRIMARY KEY,
  line_id INTEGER NOT NULL,
  lease_until TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_line_lease_until ON cti_outbound_line_lease(lease_until);

-- +goose Down
DROP TABLE cti_outbound_line_lease;
