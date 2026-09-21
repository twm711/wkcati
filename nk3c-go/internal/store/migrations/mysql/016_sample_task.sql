-- +goose Up
CREATE TABLE IF NOT EXISTS cti_sample_task(
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  project_id BIGINT NOT NULL,
  sample_id BIGINT NOT NULL,
  call_id BIGINT,
  queue_id BIGINT,
  assigned_user_id BIGINT,
  status VARCHAR(32) NOT NULL DEFAULT 'LEASED',
  leased_at DATETIME NOT NULL,
  lease_until DATETIME NOT NULL,
  completed_at DATETIME
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_sample_task_lease ON cti_sample_task(status,lease_until);
CREATE INDEX idx_sample_task_agent ON cti_sample_task(assigned_user_id,status);

-- +goose Down
DROP TABLE cti_sample_task;
