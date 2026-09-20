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
	s.qcHub.ServeWS(c.Writer, c.Request)
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
	killed := 0
	if s.sessions != nil {
		killed = s.sessions.LogoutAll(req.UserID)
	}
	s.qcHub.Publish("FORCE_LOGOUT", gin.H{"targetUserId": req.UserID, "targetAgentNo": agentNo,
		"releasedSamples": released, "sessions": killed, "byUserId": op.ID, "byAgentNo": op.AgentNo})
	rinfo.GinOK(c, gin.H{"sessions": killed, "releasedSamples": released}, "已强签 "+agentNo)
}

// BuildWall 墙面快照（HTTP 与 WS Hub 共用）
func (s *Service) BuildWall() map[string]interface{} {
	agents := []map[string]interface{}{}
	base := []struct {
		uid           int64
		agentNo, name string
	}{}
	arows, _ := s.db.Query(`SELECT u.id,u.agent_no,u.user_name FROM sys_user u WHERE u.agent_no IS NOT NULL AND u.status=1`)
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
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM cti_call_record WHERE substr(begin_time,1,10)=?`, today).Scan(&dial)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM cti_call_record WHERE connect_time IS NOT NULL`).Scan(&conn)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM cti_call_record WHERE result_code='SUCCESS'`).Scan(&succ)
	return map[string]interface{}{"agents": agents, "summary": gin.H{
		"dialCount": dial, "connectCount": conn, "successCount": succ, "abandonCount": 0}}
}

func (s *Service) Wall(c *gin.Context) {
	rinfo.GinOK(c, s.BuildWall(), "ok")
}

func (s *Service) Calls(c *gin.Context) {
	limit := c.DefaultQuery("limit", "12")
	rows, err := s.db.Query(`SELECT c.id,c.sample_id,s.cust_name,c.agent_no,c.status,c.result_code,c.begin_time,c.connect_time,COALESCE(c.record_file,'')
		FROM cti_call_record c LEFT JOIN smp_sample s ON s.id=c.sample_id ORDER BY c.id DESC LIMIT ` + limit)
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
