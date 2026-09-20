-- +goose Up
ALTER TABLE ivr_flow ADD COLUMN project_id BIGINT NOT NULL DEFAULT 1;
CREATE UNIQUE INDEX idx_ivr_flow_project ON ivr_flow(project_id);
CREATE TABLE ivr_route(
  id BIGINT PRIMARY KEY,
  caller_prefix VARCHAR(64) UNIQUE,
  project_id BIGINT,
  enabled INT DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT INTO ivr_route(id,caller_prefix,project_id,enabled) VALUES(1,'',1,1);

-- +goose Down
DROP TABLE ivr_route;
DROP INDEX idx_ivr_flow_project ON ivr_flow;
ALTER TABLE ivr_flow DROP COLUMN project_id;
