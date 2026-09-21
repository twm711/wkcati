-- +goose Up
CREATE TABLE IF NOT EXISTS cti_worker_lock(
  name VARCHAR(64) PRIMARY KEY,
  owner VARCHAR(128) NOT NULL,
  lease_until DATETIME NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT IGNORE INTO cti_worker_lock(name,owner,lease_until) VALUES('sample-task-reaper','bootstrap','1970-01-01 00:00:00');

-- +goose Down
DROP TABLE cti_worker_lock;
