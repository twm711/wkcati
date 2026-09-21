-- +goose Up
CREATE TABLE IF NOT EXISTS cti_worker_lock(
  name TEXT PRIMARY KEY,
  owner TEXT NOT NULL,
  lease_until TEXT NOT NULL
);
INSERT OR IGNORE INTO cti_worker_lock(name,owner,lease_until) VALUES('sample-task-reaper','bootstrap','1970-01-01T00:00:00+00:00');

-- +goose Down
DROP TABLE cti_worker_lock;
