-- +goose Up
CREATE TABLE IF NOT EXISTS cti_agent_queue(
  user_id INTEGER NOT NULL,
  queue_id INTEGER NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  capacity INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(user_id,queue_id)
);
CREATE INDEX idx_agent_queue_queue ON cti_agent_queue(queue_id,enabled);

-- +goose Down
DROP TABLE cti_agent_queue;
