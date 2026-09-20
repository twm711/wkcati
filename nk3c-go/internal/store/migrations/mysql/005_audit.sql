-- +goose Up
CREATE TABLE sys_op_log(
  id VARCHAR(32) PRIMARY KEY,
  user_id BIGINT,
  login_name VARCHAR(255),
  method VARCHAR(16),
  path VARCHAR(255),
  action VARCHAR(255),
  status_code INT,
  created_at DATETIME NOT NULL,
  KEY idx_sys_op_log_created(created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE sys_op_log;
