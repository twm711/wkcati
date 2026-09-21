-- +goose Up
CREATE TABLE IF NOT EXISTS sys_skill(
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  tenant_id BIGINT NOT NULL,
  name VARCHAR(255) NOT NULL,
  status BIGINT DEFAULT 1,
  UNIQUE KEY uk_skill_tenant_name(tenant_id,name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS sys_user_skill(
  user_id BIGINT NOT NULL,
  skill_id BIGINT NOT NULL,
  level BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(user_id,skill_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS sys_group_skill(
  group_id BIGINT NOT NULL,
  skill_id BIGINT NOT NULL,
  level BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(group_id,skill_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS prj_skill_requirement(
  project_id BIGINT NOT NULL,
  skill_id BIGINT NOT NULL,
  min_level BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(project_id,skill_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_user_skill_skill ON sys_user_skill(skill_id);
CREATE INDEX idx_project_skill_skill ON prj_skill_requirement(skill_id);

-- +goose Down
DROP TABLE prj_skill_requirement;
DROP TABLE sys_group_skill;
DROP TABLE sys_user_skill;
DROP TABLE sys_skill;
