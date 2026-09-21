-- +goose Up
CREATE TABLE IF NOT EXISTS cti_queue(
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  tenant_id BIGINT NOT NULL,
  org_id BIGINT NOT NULL,
  group_id BIGINT NOT NULL,
  name VARCHAR(255) NOT NULL,
  priority BIGINT NOT NULL DEFAULT 100,
  status BIGINT DEFAULT 1,
  UNIQUE KEY uk_queue_tenant_name(tenant_id,name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS prj_queue(
  project_id BIGINT PRIMARY KEY,
  queue_id BIGINT NOT NULL,
  priority BIGINT NOT NULL DEFAULT 100
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE INDEX idx_cti_queue_group ON cti_queue(group_id,status);

-- +goose Down
DROP TABLE prj_queue;
DROP TABLE cti_queue;
