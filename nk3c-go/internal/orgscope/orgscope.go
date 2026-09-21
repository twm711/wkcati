// Package orgscope provides the minimum organization/group administration layer
// used by tenant-aware authorization. It deliberately keeps membership changes
// explicit; assignment APIs can later add approval and audit workflows.
package orgscope

import (
	"database/sql"
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

func (s *Service) AssignUserQueue(c *gin.Context) {
	op := auth.From(c)
	if !auth.HasRoleP(op, "domainAdmin", "orgAdmin", "groupAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要队列管理权限")
		return
	}
	uid, _ := strconv.ParseInt(c.Param("uid"), 10, 64)
	var req struct {
		QueueID  int64 `json:"queueId" binding:"required"`
		Enabled  *bool `json:"enabled"`
		Capacity int   `json:"capacity"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "queueId 参数错误")
		return
	}
	if req.Capacity <= 0 {
		req.Capacity = 1
	}
	en := 1
	if req.Enabled != nil && !(*req.Enabled) {
		en = 0
	}
	var ut, qt int64
	if s.db.QueryRow(`SELECT tenant_id FROM sys_user WHERE id=?`, uid).Scan(&ut) != nil || s.db.QueryRow(`SELECT tenant_id FROM cti_queue WHERE id=? AND status=1`, req.QueueID).Scan(&qt) != nil || ut != qt || (!auth.HasRoleP(op, "domainAdmin") && ut != op.TenantID) {
		rinfo.GinFail(c, rinfo.CodePermission, "租户归属不一致")
		return
	}
	if s.db.Driver == "mysql" {
		_, _ = s.db.Exec(`INSERT INTO cti_agent_queue(user_id,queue_id,enabled,capacity) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),capacity=VALUES(capacity)`, uid, req.QueueID, en, req.Capacity)
	} else {
		_, _ = s.db.Exec(`INSERT INTO cti_agent_queue(user_id,queue_id,enabled,capacity) VALUES(?,?,?,?) ON CONFLICT(user_id,queue_id) DO UPDATE SET enabled=excluded.enabled,capacity=excluded.capacity`, uid, req.QueueID, en, req.Capacity)
	}
	rinfo.GinOK(c, gin.H{"userId": uid, "queueId": req.QueueID, "enabled": en == 1, "capacity": req.Capacity}, "坐席队列归属已更新")
}

func (s *Service) ListQueues(c *gin.Context) {
	u := auth.From(c)
	q := `SELECT id,org_id,group_id,name,priority,status FROM cti_queue WHERE tenant_id=? ORDER BY priority,id`
	args := []interface{}{u.TenantID}
	if auth.HasRoleP(u, "domainAdmin") {
		q = `SELECT id,tenant_id,org_id,group_id,name,priority,status FROM cti_queue ORDER BY priority,id`
		args = nil
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, tenant, org, group int64
		var name string
		var priority, status int
		if auth.HasRoleP(u, "domainAdmin") {
			if rows.Scan(&id, &tenant, &org, &group, &name, &priority, &status) != nil {
				continue
			}
		} else {
			if rows.Scan(&id, &org, &group, &name, &priority, &status) != nil {
				continue
			}
			tenant = u.TenantID
		}
		out = append(out, gin.H{"id": id, "tenantId": tenant, "orgId": org, "groupId": group, "name": name, "priority": priority, "status": status})
	}
	rinfo.GinOK(c, out, "ok")
}

func (s *Service) CreateQueue(c *gin.Context) {
	u := auth.From(c)
	if !auth.HasRoleP(u, "domainAdmin", "orgAdmin", "groupAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要队列管理权限")
		return
	}
	var req struct {
		Name     string `json:"name" binding:"required"`
		OrgID    int64  `json:"orgId" binding:"required"`
		GroupID  int64  `json:"groupId" binding:"required"`
		Priority int    `json:"priority"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "name/orgId/groupId 参数错误")
		return
	}
	if req.Priority <= 0 {
		req.Priority = 100
	}
	var tenant int64
	var groupOrg int64
	if s.db.QueryRow(`SELECT tenant_id FROM sys_org WHERE id=?`, req.OrgID).Scan(&tenant) != nil || s.db.QueryRow(`SELECT org_id FROM sys_group WHERE id=? AND status=1`, req.GroupID).Scan(&groupOrg) != nil || groupOrg != req.OrgID || (!auth.HasRoleP(u, "domainAdmin") && tenant != u.TenantID) {
		rinfo.GinFail(c, rinfo.CodePermission, "组织/队列归属不一致")
		return
	}
	res, err := s.db.Exec(`INSERT INTO cti_queue(tenant_id,org_id,group_id,name,priority,status) VALUES(?,?,?,?,?,1)`, tenant, req.OrgID, req.GroupID, req.Name, req.Priority)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	rinfo.GinOK(c, gin.H{"id": id, "tenantId": tenant, "orgId": req.OrgID, "groupId": req.GroupID, "priority": req.Priority}, "队列已创建")
}

func (s *Service) AssignProjectQueue(c *gin.Context) {
	u := auth.From(c)
	if !auth.HasRoleP(u, "domainAdmin", "orgAdmin", "groupAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要队列管理权限")
		return
	}
	pid, _ := strconv.ParseInt(c.Param("pid"), 10, 64)
	var req struct {
		QueueID  int64 `json:"queueId" binding:"required"`
		Priority int   `json:"priority"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "queueId 参数错误")
		return
	}
	if req.Priority <= 0 {
		req.Priority = 100
	}
	var pt, qt int64
	if s.db.QueryRow(`SELECT tenant_id FROM prj_project WHERE id=?`, pid).Scan(&pt) != nil || s.db.QueryRow(`SELECT tenant_id FROM cti_queue WHERE id=? AND status=1`, req.QueueID).Scan(&qt) != nil || pt != qt || (!auth.HasRoleP(u, "domainAdmin") && pt != u.TenantID) {
		rinfo.GinFail(c, rinfo.CodePermission, "项目/队列租户不一致")
		return
	}
	if s.db.Driver == "mysql" {
		_, _ = s.db.Exec(`INSERT INTO prj_queue(project_id,queue_id,priority) VALUES(?,?,?) ON DUPLICATE KEY UPDATE queue_id=VALUES(queue_id),priority=VALUES(priority)`, pid, req.QueueID, req.Priority)
	} else {
		_, _ = s.db.Exec(`INSERT INTO prj_queue(project_id,queue_id,priority) VALUES(?,?,?) ON CONFLICT(project_id) DO UPDATE SET queue_id=excluded.queue_id,priority=excluded.priority`, pid, req.QueueID, req.Priority)
	}
	rinfo.GinOK(c, gin.H{"projectId": pid, "queueId": req.QueueID, "priority": req.Priority}, "项目队列已更新")
}

func (s *Service) ListSkills(c *gin.Context) {
	u := auth.From(c)
	rows, err := s.db.Query(`SELECT id,name,status FROM sys_skill WHERE tenant_id=? ORDER BY id`, u.TenantID)
	if auth.HasRoleP(u, "domainAdmin") {
		rows, err = s.db.Query(`SELECT id,name,status FROM sys_skill ORDER BY id`)
	}
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var name string
		var status int
		if rows.Scan(&id, &name, &status) == nil {
			out = append(out, gin.H{"id": id, "name": name, "status": status})
		}
	}
	rinfo.GinOK(c, out, "ok")
}

func (s *Service) CreateSkill(c *gin.Context) {
	u := auth.From(c)
	if !auth.HasRoleP(u, "domainAdmin", "orgAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要技能管理权限")
		return
	}
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "name 参数错误")
		return
	}
	res, err := s.db.Exec(`INSERT INTO sys_skill(tenant_id,name,status) VALUES(?,?,1)`, u.TenantID, req.Name)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	rinfo.GinOK(c, gin.H{"id": id, "name": req.Name, "tenantId": u.TenantID}, "技能已创建")
}

func (s *Service) AssignUserSkill(c *gin.Context) {
	op := auth.From(c)
	if !auth.HasRoleP(op, "domainAdmin", "orgAdmin", "groupAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要技能管理权限")
		return
	}
	uid, _ := strconv.ParseInt(c.Param("uid"), 10, 64)
	var req struct {
		SkillID int64 `json:"skillId" binding:"required"`
		Level   int   `json:"level"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Level < 1 {
		rinfo.GinFail(c, rinfo.CodeParam, "skillId/level 参数错误")
		return
	}
	var ut, st int64
	if s.db.QueryRow(`SELECT tenant_id FROM sys_user WHERE id=?`, uid).Scan(&ut) != nil || s.db.QueryRow(`SELECT tenant_id FROM sys_skill WHERE id=? AND status=1`, req.SkillID).Scan(&st) != nil || ut != st || (!auth.HasRoleP(op, "domainAdmin") && ut != op.TenantID) {
		rinfo.GinFail(c, rinfo.CodePermission, "租户归属不一致")
		return
	}
	if s.db.Driver == "mysql" {
		_, _ = s.db.Exec(`INSERT INTO sys_user_skill(user_id,skill_id,level) VALUES(?,?,?) ON DUPLICATE KEY UPDATE level=VALUES(level)`, uid, req.SkillID, req.Level)
	} else {
		_, _ = s.db.Exec(`INSERT INTO sys_user_skill(user_id,skill_id,level) VALUES(?,?,?) ON CONFLICT(user_id,skill_id) DO UPDATE SET level=excluded.level`, uid, req.SkillID, req.Level)
	}
	rinfo.GinOK(c, gin.H{"userId": uid, "skillId": req.SkillID, "level": req.Level}, "坐席技能已更新")
}

func (s *Service) SetProjectSkills(c *gin.Context) {
	op := auth.From(c)
	if !auth.HasRoleP(op, "domainAdmin", "orgAdmin", "groupAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要技能管理权限")
		return
	}
	pid, _ := strconv.ParseInt(c.Param("pid"), 10, 64)
	var tenant int64
	if s.db.QueryRow(`SELECT tenant_id FROM prj_project WHERE id=?`, pid).Scan(&tenant) != nil || (!auth.HasRoleP(op, "domainAdmin") && tenant != op.TenantID) {
		rinfo.GinFail(c, rinfo.CodeNotFound, "项目不存在")
		return
	}
	var req struct {
		Requirements []struct {
			SkillID  int64 `json:"skillId"`
			MinLevel int   `json:"minLevel"`
		} `json:"requirements"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "requirements 参数错误")
		return
	}
	if err := s.db.Tx(func(tx *sql.Tx) error {
		if _, e := tx.Exec(`DELETE FROM prj_skill_requirement WHERE project_id=?`, pid); e != nil {
			return e
		}
		for _, r := range req.Requirements {
			if r.MinLevel < 1 {
				r.MinLevel = 1
			}
			var st int64
			if e := tx.QueryRow(`SELECT tenant_id FROM sys_skill WHERE id=? AND status=1`, r.SkillID).Scan(&st); e != nil || st != tenant {
				return fmt.Errorf("技能不属于项目租户")
			}
			if _, e := tx.Exec(`INSERT INTO prj_skill_requirement(project_id,skill_id,min_level) VALUES(?,?,?)`, pid, r.SkillID, r.MinLevel); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, err.Error())
		return
	}
	rinfo.GinOK(c, gin.H{"projectId": pid, "count": len(req.Requirements)}, "项目技能要求已更新")
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
