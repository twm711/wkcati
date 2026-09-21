// Package monitor 监控墙快照 / 话务流水（WS 推送为 M0.5，当前 REST 快照）
package monitor

import (
	"context"

	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/internal/realtime"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

// SessionKiller 强签会话吊销能力（auth.Service 满足；接口隔离避免包耦合）
type SessionKiller interface {
	LogoutAll(userID int64) int
}

type CTIController interface {
	Hangup(callID int64) error
	AddSupervisor(ctx context.Context, callID int64, host string, port int, listenOnly bool) error
	SetSupervisorMode(callID int64, listenOnly bool) error
}

type Service struct {
	db       *store.DB
	qcHub    *realtime.EventHub
	sessions SessionKiller
	cti      CTIController
	msgHub   *realtime.MessageHub
}

func New(db *store.DB) *Service { return &Service{db: db} }

// WireQC 装配质检事件 Hub 与会话吊销器（app.Build 接线）
func (s *Service) WireQC(hub *realtime.EventHub, sk SessionKiller) {
	s.qcHub, s.sessions = hub, sk
}

func (s *Service) WireCTI(c CTIController)            { s.cti = c }
func (s *Service) WireMessage(h *realtime.MessageHub) { s.msgHub = h }
func (s *Service) ServeMessageWS(c *gin.Context) {
	s.msgHub.ServeWS(auth.From(c).ID, c.Writer, c.Request)
}

// ServeQCWS 督导质检事件流：仅 groupAdmin 可订阅（403 不升级）
func (s *Service) ServeQCWS(c *gin.Context) {
	u := auth.From(c)
	if u == nil || !auth.HasRoleP(u, "groupAdmin") {
		// WS 升级前拦截用真实 HTTP 状态（与 401 同语义；客户端握手即失败）
		c.AbortWithStatus(403)
		return
	}
	tenantID := int64(0)
	if !auth.HasRoleP(u, "domainAdmin") {
		tenantID = u.TenantID
	}
	s.qcHub.ServeWS(c.Writer, c.Request, tenantID)
}

type forceCheckoutReq struct {
	UserID int64 `json:"userId" binding:"required"`
}

// ForceCheckout 强签坐席：注销全部会话 + 释放占用样本（ASSIGNED/INCALL → IDLE 回池）+ 广播事件
func (s *Service) ForceCheckout(c *gin.Context) {
	op := auth.From(c)
	if op == nil || !auth.HasRoleP(op, "groupAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "仅督导可执行强签")
		return
	}
	var req forceCheckoutReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	var agentNo string
	var agentID int64
	if err := s.db.QueryRow(`SELECT id,COALESCE(agent_no,'') FROM sys_user WHERE id=?`, req.UserID).Scan(&agentID, &agentNo); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "目标用户不存在")
		return
	}
	var released int64
	// 释放样本（ASSIGNED=外呼中；INCALL=桥接通话中 → 回 IDLE 池）
	res, err := s.db.Exec(`UPDATE smp_sample SET status='IDLE', owner_agent_id=NULL
		WHERE owner_agent_id=? AND status IN ('ASSIGNED','INCALL')`, req.UserID)
	if err == nil {
		n, _ := res.RowsAffected()
		released = n
	}
	_, _ = s.db.Exec(`UPDATE cti_sample_task SET status='EXPIRED',completed_at=? WHERE assigned_user_id=? AND status='LEASED'`, store.NowFor(s.db.Driver), req.UserID)
	killed := 0
	if s.sessions != nil {
		killed = s.sessions.LogoutAll(req.UserID)
	}
	s.qcHub.Publish("FORCE_LOGOUT", gin.H{"targetUserId": req.UserID, "targetAgentNo": agentNo,
		"releasedSamples": released, "sessions": killed, "byUserId": op.ID, "byAgentNo": op.AgentNo, "tenantId": op.TenantID})
	rinfo.GinOK(c, gin.H{"sessions": killed, "releasedSamples": released}, "已强签 "+agentNo)
}

// BuildWall retains the process-wide snapshot used by the legacy WS hub.
// REST callers should use BuildWallFor so tenant scope is applied.
func (s *Service) BuildWall() map[string]interface{} { return s.buildWall(0) }

func (s *Service) BuildWallFor(tenantID int64) map[string]interface{} { return s.buildWall(tenantID) }

// buildWall 墙面快照；tenantID=0 仅供旧的全局 WS hub 使用。
func (s *Service) buildWall(tenantID int64) map[string]interface{} {
	agents := []map[string]interface{}{}
	base := []struct {
		uid           int64
		agentNo, name string
	}{}
	agentQuery := `SELECT u.id,u.agent_no,u.user_name FROM sys_user u WHERE u.agent_no IS NOT NULL AND u.status=1`
	agentArgs := []interface{}{}
	if tenantID > 0 {
		agentQuery += ` AND u.tenant_id=?`
		agentArgs = append(agentArgs, tenantID)
	}
	arows, _ := s.db.Query(agentQuery, agentArgs...)
	for arows != nil && arows.Next() {
		var b struct {
			uid           int64
			agentNo, name string
		}
		_ = arows.Scan(&b.uid, &b.agentNo, &b.name)
		base = append(base, b)
	}
	if arows != nil {
		arows.Close()
	}
	for _, b := range base {
		uid, agentNo, name := b.uid, b.agentNo, b.name
		state, sampleID, callID := "READY", interface{}(nil), interface{}(nil)
		_ = s.db.QueryRow(`SELECT state FROM cti_agent_state WHERE user_id=?`, uid).Scan(&state)
		var sid interface{}
		_ = s.db.QueryRow(`SELECT id FROM smp_sample WHERE owner_agent_id=? AND status='ASSIGNED' LIMIT 1`, uid).Scan(&sid)
		if sid != nil {
			state, sampleID = "DIALING", sid
		}
		var talk string
		_ = s.db.QueryRow(`SELECT 'x' FROM ans_sheet s JOIN cti_call_record c ON c.id=s.call_id
			WHERE s.agent_id=? AND s.status='DOING' AND c.connect_time IS NOT NULL LIMIT 1`, uid).Scan(&talk)
		if talk == "x" {
			state = "TALKING"
		}
		var cid interface{}
		_ = s.db.QueryRow(`SELECT id FROM cti_call_record WHERE agent_id=? AND status='DIALING' ORDER BY id DESC LIMIT 1`, uid).Scan(&cid)
		if cid != nil {
			callID = cid
			if state == "READY" {
				state = "DIALING"
			}
		}
		agents = append(agents, map[string]interface{}{"agentNo": agentNo, "userName": name, "state": state,
			"sampleId": sampleID, "callId": callID, "userId": uid})
	}
	today := store.NowISO()[:10]
	var dial, conn, succ int
	callScope := ""
	callArgs := []interface{}{}
	if tenantID > 0 {
		callScope = ` AND project_id IN (SELECT id FROM prj_project WHERE tenant_id=?)`
		callArgs = append(callArgs, tenantID)
	}
	argsDial := append([]interface{}{today}, callArgs...)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM cti_call_record WHERE substr(begin_time,1,10)=?`+callScope, argsDial...).Scan(&dial)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM cti_call_record WHERE connect_time IS NOT NULL`+callScope, callArgs...).Scan(&conn)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM cti_call_record WHERE result_code='SUCCESS'`+callScope, callArgs...).Scan(&succ)
	return map[string]interface{}{"agents": agents, "summary": gin.H{
		"dialCount": dial, "connectCount": conn, "successCount": succ, "abandonCount": 0}}
}

func (s *Service) Wall(c *gin.Context) {
	u := auth.From(c)
	tenantID := int64(0)
	if !auth.HasRoleP(u, "domainAdmin") {
		tenantID = u.TenantID
	}
	rinfo.GinOK(c, s.BuildWallFor(tenantID), "ok")
}

func (s *Service) Calls(c *gin.Context) {
	u := auth.From(c)
	limit := c.DefaultQuery("limit", "12")
	q := `SELECT c.id,c.sample_id,s.cust_name,c.agent_no,c.status,c.result_code,c.begin_time,c.connect_time,COALESCE(c.record_file,'')
		FROM cti_call_record c JOIN prj_project p ON p.id=c.project_id LEFT JOIN smp_sample s ON s.id=c.sample_id`
	args := []interface{}{}
	if !auth.HasRoleP(u, "domainAdmin") {
		q += ` WHERE p.tenant_id=?`
		args = append(args, u.TenantID)
	}
	q += ` ORDER BY c.id DESC LIMIT ` + limit
	rows, err := s.db.Query(q, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var agentNo, status, rec string
		var sampleID interface{}
		var cust, rc, begin, conn interface{}
		_ = rows.Scan(&id, &sampleID, &cust, &agentNo, &status, &rc, &begin, &conn, &rec)
		out = append(out, map[string]interface{}{"id": id, "sample_id": sampleID, "cust_name": cust,
			"agent_no": agentNo, "status": status, "result_code": rc, "begin_time": begin, "connect_time": conn, "record_file": rec})
	}
	rinfo.GinOK(c, out, "ok")
}

// LineHealth 汇总主叫线路的接通率、失败率和最近结果，供线路降级决策使用。
func (s *Service) LineHealth(c *gin.Context) {
	u := auth.From(c)
	if u == nil || !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要督导及以上权限")
		return
	}
	q := `SELECT COALESCE(c.caller_no,''),COUNT(*),SUM(CASE WHEN c.status='CLOSED' AND c.result_code IN ('SUCCESS','PARTIAL') THEN 1 ELSE 0 END),SUM(CASE WHEN c.status='CLOSED' AND c.result_code IN ('BUSY','NA','REFUSE','INVALID','BREAKOFF') THEN 1 ELSE 0 END),MAX(c.end_time) FROM cti_call_record c JOIN prj_project p ON p.id=c.project_id`
	args := []interface{}{}
	if !auth.HasRoleP(u, "domainAdmin") {
		q += ` WHERE p.tenant_id=?`
		args = append(args, u.TenantID)
	}
	q += ` GROUP BY c.caller_no ORDER BY COUNT(*) DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var line string
		var total, connected, failed int64
		var last string
		if rows.Scan(&line, &total, &connected, &failed, &last) == nil {
			rate := float64(0)
			if total > 0 {
				rate = float64(failed) / float64(total)
			}
			out = append(out, gin.H{"line": line, "total": total, "connected": connected, "failed": failed, "failureRate": rate, "lastCallAt": last, "degraded": rate >= 0.5 && total >= 10})
		}
	}
	rinfo.GinOK(c, out, "ok")
}

type outboundLineReq struct {
	LineNo   string `json:"lineNo" binding:"required"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Priority int    `json:"priority"`
	Capacity int    `json:"capacity"`
	Enabled  *bool  `json:"enabled"`
}

// Lines 返回当前租户外呼线路及容量状态。
func (s *Service) Lines(c *gin.Context) {
	u := auth.From(c)
	if u == nil || !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要管理权限")
		return
	}
	q := `SELECT id,line_no,COALESCE(host,''),port,enabled,priority,capacity,active_calls,circuit_state,failure_streak,COALESCE(opened_until,''),created_at FROM cti_outbound_line`
	args := []interface{}{}
	if !auth.HasRoleP(u, "domainAdmin") {
		q += ` WHERE tenant_id=?`
		args = append(args, u.TenantID)
	}
	q += ` ORDER BY priority,id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, port, en, pri, cap, active, streak int64
		var no, host, state, opened, created string
		if rows.Scan(&id, &no, &host, &port, &en, &pri, &cap, &active, &state, &streak, &opened, &created) == nil {
			out = append(out, gin.H{"id": id, "lineNo": no, "host": host, "port": port, "enabled": en == 1, "priority": pri, "capacity": cap, "activeCalls": active, "available": cap > active && state != "OPEN", "circuitState": state, "failureStreak": streak, "openedUntil": opened, "createdAt": created})
		}
	}
	rinfo.GinOK(c, out, "ok")
}

// UpsertLine 创建或更新线路配置；实际拨号接线仍由媒体域负责。
func (s *Service) UpsertLine(c *gin.Context) {
	u := auth.From(c)
	if u == nil || !auth.HasRoleP(u, "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要机构管理员权限")
		return
	}
	var req outboundLineReq
	if c.ShouldBindJSON(&req) != nil || req.LineNo == "" {
		rinfo.GinFail(c, rinfo.CodeParam, "线路参数错误")
		return
	}
	if req.Port == 0 {
		req.Port = 5060
	}
	if req.Priority == 0 {
		req.Priority = 100
	}
	if req.Capacity <= 0 {
		req.Capacity = 10
	}
	enabled := 1
	if req.Enabled != nil && !(*req.Enabled) {
		enabled = 0
	}
	var id int64
	_ = s.db.QueryRow(`SELECT id FROM cti_outbound_line WHERE tenant_id=? AND line_no=?`, u.TenantID, req.LineNo).Scan(&id)
	if id == 0 {
		_ = s.db.QueryRow(`SELECT COALESCE(MAX(id),0)+1 FROM cti_outbound_line`).Scan(&id)
		_, err := s.db.Exec(`INSERT INTO cti_outbound_line(id,tenant_id,line_no,host,port,enabled,priority,capacity,active_calls,created_at) VALUES(?,?,?,?,?,?,?,?,0,?)`, id, u.TenantID, req.LineNo, req.Host, req.Port, enabled, req.Priority, req.Capacity, store.NowFor(s.db.Driver))
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
			return
		}
	} else {
		_, err := s.db.Exec(`UPDATE cti_outbound_line SET host=?,port=?,enabled=?,priority=?,capacity=? WHERE id=? AND tenant_id=?`, req.Host, req.Port, enabled, req.Priority, req.Capacity, id, u.TenantID)
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
			return
		}
	}
	rinfo.GinOK(c, gin.H{"id": id}, "线路已保存")
}

// LineCircuitEvents 查询线路熔断状态变化时间线。
func (s *Service) LineCircuitEvents(c *gin.Context) {
	u := auth.From(c)
	if u == nil || !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要管理权限")
		return
	}
	q := `SELECT e.id,e.line_id,l.line_no,e.call_id,e.from_state,e.to_state,e.reason,e.created_at FROM cti_line_circuit_event e JOIN cti_outbound_line l ON l.id=e.line_id`
	args := []interface{}{}
	if !auth.HasRoleP(u, "domainAdmin") {
		q += ` WHERE l.tenant_id=?`
		args = append(args, u.TenantID)
	}
	q += ` ORDER BY e.created_at DESC LIMIT 200`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, lineID, callID int64
		var lineNo, from, to, reason, created string
		if rows.Scan(&id, &lineID, &lineNo, &callID, &from, &to, &reason, &created) == nil {
			out = append(out, gin.H{"id": id, "lineId": lineID, "lineNo": lineNo, "callId": callID, "fromState": from, "toState": to, "reason": reason, "createdAt": created})
		}
	}
	rinfo.GinOK(c, out, "ok")
}
