-- +goose Up
CREATE TABLE IF NOT EXISTS cti_queue(
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  org_id INTEGER NOT NULL,
  group_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100,
  status INTEGER DEFAULT 1,
  UNIQUE(tenant_id,name)
);
CREATE TABLE IF NOT EXISTS prj_queue(
  project_id INTEGER PRIMARY KEY,
  queue_id INTEGER NOT NULL,
  priority INTEGER NOT NULL DEFAULT 100
);
CREATE INDEX idx_cti_queue_group ON cti_queue(group_id,status);

-- +goose Down
DROP TABLE prj_queue;
DROP TABLE cti_queue;
