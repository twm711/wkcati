-- +goose Up
CREATE TABLE IF NOT EXISTS cti_waiting_task(
  id BIGINT PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  project_id BIGINT NOT NULL,
  queue_id BIGINT NOT NULL,
  agent_id BIGINT NOT NULL,
  priority INT NOT NULL DEFAULT 100,
  status VARCHAR(16) NOT NULL DEFAULT 'WAITING',
  created_at DATETIME NOT NULL,
  assigned_at DATETIME NULL,
  KEY idx_waiting_task_pick(queue_id,status,priority,created_at),
  UNIQUE KEY idx_waiting_task_active(agent_id,project_id,status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE cti_waiting_task;
