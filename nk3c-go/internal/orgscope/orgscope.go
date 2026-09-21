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

func (s *Service) AssignUserGroup(c *gin.Context) {
	op := auth.From(c)
	if !auth.HasRoleP(op, "domainAdmin", "orgAdmin", "groupAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要坐席组管理权限")
		return
	}
	uid, _ := strconv.ParseInt(c.Param("uid"), 10, 64)
	var req struct {
		OrgID   int64 `json:"orgId" binding:"required"`
		GroupID int64 `json:"groupId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "orgId/groupId 参数错误")
		return
	}
	var userTenant, orgTenant, groupOrg int64
	if err := s.db.QueryRow(`SELECT tenant_id FROM sys_user WHERE id=?`, uid).Scan(&userTenant); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "用户不存在")
		return
	}
	if err := s.db.QueryRow(`SELECT tenant_id FROM sys_org WHERE id=?`, req.OrgID).Scan(&orgTenant); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "机构不存在")
		return
	}
	if err := s.db.QueryRow(`SELECT org_id FROM sys_group WHERE id=? AND status=1`, req.GroupID).Scan(&groupOrg); err != nil || groupOrg != req.OrgID {
		rinfo.GinFail(c, rinfo.CodeNotFound, "坐席组不存在")
		return
	}
	if userTenant != orgTenant || (!auth.HasRoleP(op, "domainAdmin") && (userTenant != op.TenantID || orgTenant != op.TenantID)) {
		rinfo.GinFail(c, rinfo.CodePermission, "租户归属不一致")
		return
	}
	if _, err := s.db.Exec(`UPDATE sys_user SET org_id=?,group_id=? WHERE id=?`, req.OrgID, req.GroupID, uid); err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	rinfo.GinOK(c, gin.H{"userId": uid, "orgId": req.OrgID, "groupId": req.GroupID}, "坐席组归属已更新")
}

func (s *Service) AssignProjectGroup(c *gin.Context) {
	op := auth.From(c)
	if !auth.HasRoleP(op, "domainAdmin", "orgAdmin", "groupAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要坐席组管理权限")
		return
	}
	pid, _ := strconv.ParseInt(c.Param("pid"), 10, 64)
	var req struct {
		GroupID int64 `json:"groupId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "groupId 参数错误")
		return
	}
	var tenant, orgID int64
	if err := s.db.QueryRow(`SELECT tenant_id FROM prj_project WHERE id=?`, pid).Scan(&tenant); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "项目不存在")
		return
	}
	if err := s.db.QueryRow(`SELECT org_id FROM sys_group WHERE id=? AND status=1`, req.GroupID).Scan(&orgID); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "坐席组不存在")
		return
	}
	var orgTenant int64
	if err := s.db.QueryRow(`SELECT tenant_id FROM sys_org WHERE id=?`, orgID).Scan(&orgTenant); err != nil || orgTenant != tenant || (!auth.HasRoleP(op, "domainAdmin") && tenant != op.TenantID) {
		rinfo.GinFail(c, rinfo.CodePermission, "租户归属不一致")
		return
	}
	if _, err := s.db.Exec(`UPDATE prj_project SET group_id=? WHERE id=?`, req.GroupID, pid); err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	rinfo.GinOK(c, gin.H{"projectId": pid, "groupId": req.GroupID}, "项目坐席组归属已更新")
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
