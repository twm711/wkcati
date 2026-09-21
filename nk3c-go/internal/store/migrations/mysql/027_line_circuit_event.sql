-- +goose Up
CREATE TABLE IF NOT EXISTS cti_line_circuit_event(
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  line_id BIGINT NOT NULL,
  call_id BIGINT,
  from_state VARCHAR(16) NOT NULL,
  to_state VARCHAR(16) NOT NULL,
  reason VARCHAR(64) NOT NULL,
  created_at DATETIME NOT NULL,
  KEY idx_line_circuit_event_line(line_id,created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE cti_line_circuit_event;
