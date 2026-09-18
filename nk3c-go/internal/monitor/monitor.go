// Package monitor 监控墙快照 / 话务流水（WS 推送为 M0.5，当前 REST 快照）
package monitor

import (
	"github.com/gin-gonic/gin"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

func (s *Service) Wall(c *gin.Context) {
	agents := []map[string]interface{}{}
	base := []struct {
		uid          int64
		agentNo, name string
	}{}
	arows, _ := s.db.Query(`SELECT u.id,u.agent_no,u.user_name FROM sys_user u WHERE u.agent_no IS NOT NULL AND u.status=1`)
	for arows != nil && arows.Next() {
		var b struct{ uid int64; agentNo, name string }
		_ = arows.Scan(&b.uid, &b.agentNo, &b.name)
		base = append(base, b)
	}
	if arows != nil { arows.Close() }
	for _, b := range base {
		uid, agentNo, name := b.uid, b.agentNo, b.name
		state, sampleID, callID := "READY", interface{}(nil), interface{}(nil)
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
			"sampleId": sampleID, "callId": callID})
	}
	today := store.NowISO()[:10]
	var dial, conn, succ int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM cti_call_record WHERE substr(begin_time,1,10)=?`, today).Scan(&dial)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM cti_call_record WHERE connect_time IS NOT NULL`).Scan(&conn)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM cti_call_record WHERE result_code='SUCCESS'`).Scan(&succ)
	rinfo.GinOK(c, gin.H{"agents": agents, "summary": gin.H{
		"dialCount": dial, "connectCount": conn, "successCount": succ, "abandonCount": 0}}, "ok")
}

func (s *Service) Calls(c *gin.Context) {
	limit := c.DefaultQuery("limit", "12")
	rows, err := s.db.Query(`SELECT c.id,c.sample_id,s.cust_name,c.agent_no,c.status,c.result_code,c.begin_time,c.connect_time,COALESCE(c.record_file,'')
		FROM cti_call_record c LEFT JOIN smp_sample s ON s.id=c.sample_id ORDER BY c.id DESC LIMIT ` + limit)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error()); return
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
