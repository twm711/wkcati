-- +goose Up
CREATE TABLE IF NOT EXISTS sys_skill(
  id INTEGER PRIMARY KEY,
  tenant_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  status INTEGER DEFAULT 1,
  UNIQUE(tenant_id,name)
);
CREATE TABLE IF NOT EXISTS sys_user_skill(
  user_id INTEGER NOT NULL,
  skill_id INTEGER NOT NULL,
  level INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(user_id,skill_id)
);
CREATE TABLE IF NOT EXISTS sys_group_skill(
  group_id INTEGER NOT NULL,
  skill_id INTEGER NOT NULL,
  level INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(group_id,skill_id)
);
CREATE TABLE IF NOT EXISTS prj_skill_requirement(
  project_id INTEGER NOT NULL,
  skill_id INTEGER NOT NULL,
  min_level INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(project_id,skill_id)
);
CREATE INDEX idx_user_skill_skill ON sys_user_skill(skill_id);
CREATE INDEX idx_project_skill_skill ON prj_skill_requirement(skill_id);

-- +goose Down
DROP TABLE prj_skill_requirement;
DROP TABLE sys_group_skill;
DROP TABLE sys_user_skill;
DROP TABLE sys_skill;
