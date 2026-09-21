// Package orgscope provides the minimum organization/group administration layer
// used by tenant-aware authorization. It deliberately keeps membership changes
// explicit; assignment APIs can later add approval and audit workflows.
package orgscope

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

func (s *Service) ListOrgs(c *gin.Context) {
	u := auth.From(c)
	q := `SELECT id,tenant_id,name,status FROM sys_org`
	args := []interface{}{}
	if !auth.HasRoleP(u, "domainAdmin") {
		q += ` WHERE tenant_id=?`
		args = append(args, u.TenantID)
	}
	q += ` ORDER BY id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, tenant int64
		var name string
		var status int
		if err := rows.Scan(&id, &tenant, &name, &status); err != nil {
			continue
		}
		out = append(out, gin.H{"id": id, "tenantId": tenant, "name": name, "status": status})
	}
	rinfo.GinOK(c, out, "ok")
}

func (s *Service) CreateOrg(c *gin.Context) {
	u := auth.From(c)
	if !auth.HasRoleP(u, "domainAdmin", "orgAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要机构管理员权限")
		return
	}
	var req struct {
		Name     string `json:"name" binding:"required"`
		TenantID int64  `json:"tenantId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "name 参数错误")
		return
	}
	if req.TenantID == 0 {
		req.TenantID = u.TenantID
	}
	if !auth.HasRoleP(u, "domainAdmin") && req.TenantID != u.TenantID {
		rinfo.GinFail(c, rinfo.CodePermission, "不能操作其他租户")
		return
	}
	res, err := s.db.Exec(`INSERT INTO sys_org(tenant_id,name,status) VALUES(?,?,1)`, req.TenantID, req.Name)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	rinfo.GinOK(c, gin.H{"id": id, "tenantId": req.TenantID, "name": req.Name}, "机构已创建")
}

func (s *Service) ListGroups(c *gin.Context) {
	u := auth.From(c)
	oid, _ := strconv.ParseInt(c.Param("oid"), 10, 64)
	var tenant int64
	if err := s.db.QueryRow(`SELECT tenant_id FROM sys_org WHERE id=?`, oid).Scan(&tenant); err != nil || (!auth.HasRoleP(u, "domainAdmin") && tenant != u.TenantID) {
		rinfo.GinFail(c, rinfo.CodeNotFound, "机构不存在")
		return
	}
	rows, err := s.db.Query(`SELECT id,org_id,name,status FROM sys_group WHERE org_id=? ORDER BY id`, oid)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, org int64
		var name string
		var status int
		if rows.Scan(&id, &org, &name, &status) == nil {
			out = append(out, gin.H{"id": id, "orgId": org, "name": name, "status": status})
		}
	}
	rinfo.GinOK(c, out, "ok")
}

func (s *Service) CreateGroup(c *gin.Context) {
	u := auth.From(c)
	oid, _ := strconv.ParseInt(c.Param("oid"), 10, 64)
	if !auth.HasRoleP(u, "domainAdmin", "orgAdmin", "groupAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要坐席组管理权限")
		return
	}
	var tenant int64
	if err := s.db.QueryRow(`SELECT tenant_id FROM sys_org WHERE id=?`, oid).Scan(&tenant); err != nil || (!auth.HasRoleP(u, "domainAdmin") && tenant != u.TenantID) {
		rinfo.GinFail(c, rinfo.CodeNotFound, "机构不存在")
		return
	}
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, fmt.Sprintf("name 参数错误: %v", err))
		return
	}
	res, err := s.db.Exec(`INSERT INTO sys_group(org_id,name,status) VALUES(?,?,1)`, oid, req.Name)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	rinfo.GinOK(c, gin.H{"id": id, "orgId": oid, "name": req.Name}, "坐席组已创建")
}
