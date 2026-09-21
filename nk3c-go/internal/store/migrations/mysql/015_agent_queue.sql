-- +goose Up
CREATE TABLE IF NOT EXISTS cti_agent_queue(
  user_id BIGINT NOT NULL,
  queue_id BIGINT NOT NULL,
  enabled BIGINT NOT NULL DEFAULT 1,
  capacity BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(user_id,queue_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_agent_queue_queue ON cti_agent_queue(queue_id,enabled);

-- +goose Down
DROP TABLE cti_agent_queue;
