-- +goose Up
CREATE TABLE cti_agent_state(
  user_id BIGINT PRIMARY KEY,
  state VARCHAR(32) NOT NULL DEFAULT 'READY',
  reason VARCHAR(255),
  updated_at DATETIME NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE cti_agent_state;
