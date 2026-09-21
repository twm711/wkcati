-- +goose Up
CREATE TABLE IF NOT EXISTS cti_task_attempt(
  id INTEGER PRIMARY KEY,
  task_id INTEGER NOT NULL,
  project_id INTEGER NOT NULL,
  sample_id INTEGER NOT NULL,
  call_id INTEGER,
  reason TEXT NOT NULL,
  outcome TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_task_attempt_sample ON cti_task_attempt(sample_id,created_at);

-- +goose Down
DROP TABLE cti_task_attempt;
