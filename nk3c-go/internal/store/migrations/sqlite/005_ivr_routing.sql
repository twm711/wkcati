-- +goose Up
-- IVR 流程按项目隔离，并提供主叫号码前缀路由。
ALTER TABLE ivr_flow ADD COLUMN project_id INTEGER DEFAULT 1;
CREATE UNIQUE INDEX idx_ivr_flow_project ON ivr_flow(project_id);
CREATE TABLE ivr_route(
  id INTEGER PRIMARY KEY,
  caller_prefix TEXT UNIQUE,
  project_id INTEGER,
  enabled INTEGER DEFAULT 1
);
INSERT INTO ivr_route VALUES(1,'',1,1);

-- +goose Down
DROP TABLE ivr_route;
DROP INDEX idx_ivr_flow_project;
