-- +goose Up
-- P0 data-domain foundation: every user/project belongs to a tenant.
ALTER TABLE sys_user ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE prj_project ADD COLUMN tenant_id BIGINT NOT NULL DEFAULT 1;
CREATE INDEX idx_prj_project_tenant ON prj_project(tenant_id);

-- +goose Down
DROP INDEX idx_prj_project_tenant ON prj_project;
ALTER TABLE prj_project DROP COLUMN tenant_id;
ALTER TABLE sys_user DROP COLUMN tenant_id;
