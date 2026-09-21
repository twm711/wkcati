-- +goose Up
CREATE TABLE IF NOT EXISTS cti_task_attempt(
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  task_id BIGINT NOT NULL,
  project_id BIGINT NOT NULL,
  sample_id BIGINT NOT NULL,
  call_id BIGINT,
  reason VARCHAR(64) NOT NULL,
  outcome VARCHAR(64),
  created_at DATETIME NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_task_attempt_sample ON cti_task_attempt(sample_id,created_at);

-- +goose Down
DROP TABLE cti_task_attempt;
