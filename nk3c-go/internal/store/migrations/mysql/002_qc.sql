-- +goose Up
CREATE TABLE cti_monitor_event(
  id BIGINT PRIMARY KEY,
  agent_id BIGINT,
  agent_no VARCHAR(255),
  event VARCHAR(64),
  call_id BIGINT,
  sample_id BIGINT,
  detail VARCHAR(255),
  created_at VARCHAR(64),
  KEY idx_cti_monitor_event_created(created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE cti_monitor_event;
