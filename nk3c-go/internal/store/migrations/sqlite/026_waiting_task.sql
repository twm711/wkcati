-- +goose Up
CREATE TABLE IF NOT EXISTS cti_waiting_task(
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  project_id INTEGER NOT NULL,
  queue_id INTEGER NOT NULL,
  agent_id INTEGER NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100,
  status TEXT NOT NULL DEFAULT 'WAITING',
  created_at TEXT NOT NULL,
  assigned_at TEXT
);
CREATE INDEX idx_waiting_task_pick ON cti_waiting_task(queue_id,status,priority,created_at);
CREATE UNIQUE INDEX idx_waiting_task_active ON cti_waiting_task(agent_id,project_id,status);

-- +goose Down
DROP TABLE cti_waiting_task;
