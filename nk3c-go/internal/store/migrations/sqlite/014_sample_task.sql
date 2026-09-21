-- +goose Up
CREATE TABLE IF NOT EXISTS cti_sample_task(
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL,
  sample_id INTEGER NOT NULL,
  call_id INTEGER,
  queue_id INTEGER,
  assigned_user_id INTEGER,
  status TEXT NOT NULL DEFAULT 'LEASED',
  leased_at TEXT NOT NULL,
  lease_until TEXT NOT NULL,
  completed_at TEXT
);
CREATE INDEX idx_sample_task_lease ON cti_sample_task(status,lease_until);
CREATE INDEX idx_sample_task_agent ON cti_sample_task(assigned_user_id,status);

-- +goose Down
DROP TABLE cti_sample_task;
