-- +goose Up
INSERT OR IGNORE INTO cti_worker_lock(name,owner,lease_until) VALUES('waiting-task-dispatch','bootstrap','1970-01-01T00:00:00+00:00');

-- +goose Down
DELETE FROM cti_worker_lock WHERE name='waiting-task-dispatch';
