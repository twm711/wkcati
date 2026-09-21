-- +goose Up
-- P0 data-domain foundation: every user/project belongs to a tenant.
ALTER TABLE sys_user ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1;
ALTER TABLE prj_project ADD COLUMN tenant_id INTEGER NOT NULL DEFAULT 1;
CREATE INDEX idx_prj_project_tenant ON prj_project(tenant_id);

-- +goose Down
DROP INDEX idx_prj_project_tenant;
