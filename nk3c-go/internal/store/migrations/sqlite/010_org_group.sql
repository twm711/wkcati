-- +goose Up
CREATE TABLE IF NOT EXISTS sys_org(
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  status INTEGER DEFAULT 1
);
CREATE TABLE IF NOT EXISTS sys_group(
  id INTEGER PRIMARY KEY,
  org_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  status INTEGER DEFAULT 1
);
ALTER TABLE sys_user ADD COLUMN org_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE sys_user ADD COLUMN group_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE prj_project ADD COLUMN group_id INTEGER NOT NULL DEFAULT 1;
INSERT OR IGNORE INTO sys_org(id,tenant_id,name,status) VALUES(1,1,'默认机构',1);
INSERT OR IGNORE INTO sys_group(id,org_id,name,status) VALUES(1,1,'默认坐席组',1);

-- +goose Down
DROP TABLE sys_group;
DROP TABLE sys_org;
