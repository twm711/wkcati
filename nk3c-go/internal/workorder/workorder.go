// Package workorder 工单：IVR 转人工落单 → 受理→办结→归档（归档自动生成回访样本）
package workorder

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

type Service struct{ db *store.DB }

func New(db *store.DB) *Service { return &Service{db: db} }

// CreateFromIVR 转人工自动落单（供 IVR 模块调用）
func (s *Service) CreateFromIVR(projectID, callID int64, callerNo, path string, ts string) (int64, error) {
	if projectID == 0 {
		projectID = 1
	}
	res, err := s.db.Exec(`INSERT INTO wko_ticket(project_id,call_id,caller_no,subject,detail,status,priority,created_at)
		VALUES(?,?,?,?,?,'PENDING','HIGH',?)`, projectID, callID, callerNo, "IVR转人工来电", "呼入菜单按键0转人工；通话轨迹："+path, ts)
	if err != nil { return 0, err }
	tid, err := res.LastInsertId()
	if err != nil { return 0, err }
	return tid, nil
}

func (s *Service) List(c *gin.Context) {
	u := auth.From(c)
	q := `SELECT w.id,w.caller_no,w.subject,w.detail,w.status,w.priority,w.remark,w.revisit_sample_id,
		w.created_at,u.user_name FROM wko_ticket w JOIN prj_project p ON p.id=w.project_id LEFT JOIN sys_user u ON u.id=w.assigned_agent_id`
	args := []interface{}{}
	where := ""
	if !auth.HasRoleP(u, "domainAdmin") {
		where = ` WHERE p.tenant_id=?`
		args = append(args, u.TenantID)
	}
	if st := c.Query("status"); st != "" {
		if where == "" {
			where = ` WHERE w.status=?`
		} else {
			where += ` AND w.status=?`
		}
		args = append(args, st)
	}
	q += where
	q += ` ORDER BY w.id DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var callerNo, subject, detail, status, priority, created string
		var remark sql.NullString
		var revisit, agent interface{}
		_ = rows.Scan(&id, &callerNo, &subject, &detail, &status, &priority, &remark, &revisit, &created, &agent)
		out = append(out, map[string]interface{}{"id": id, "caller_no": callerNo, "subject": subject,
			"detail": detail, "status": status, "priority": priority, "remark": remark.String,
			"revisit_sample_id": revisit, "created_at": created, "agent_name": agent})
	}
	rinfo.GinOK(c, gin.H{"total": len(out), "rows": out}, "ok")
}

func (s *Service) Detail(c *gin.Context) {
	u := auth.From(c)
	tid, _ := strconv.ParseInt(c.Param("tid"), 10, 64)
	var callerNo, subject, detail, status, priority, created string
	var remark sql.NullString
	var revisit *int64
	var agent *string
	q := `SELECT w.caller_no,w.subject,w.detail,w.status,w.priority,w.remark,w.revisit_sample_id,w.created_at,u.user_name
		FROM wko_ticket w JOIN prj_project p ON p.id=w.project_id LEFT JOIN sys_user u ON u.id=w.assigned_agent_id WHERE w.id=?`
	args := []interface{}{tid}
	if !auth.HasRoleP(u, "domainAdmin") {
		q += ` AND p.tenant_id=?`
		args = append(args, u.TenantID)
	}
	if err := s.db.QueryRow(q, args...).
		Scan(&callerNo, &subject, &detail, &status, &priority, &remark, &revisit, &created, &agent); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "工单不存在")
		return
	}
	out := map[string]interface{}{"id": tid, "caller_no": callerNo, "subject": subject, "detail": detail,
		"status": status, "priority": priority, "remark": remark.String, "created_at": created}
	if agent != nil {
		out["agent_name"] = *agent
	}
	if revisit != nil { // 回访链路：样本 → 最新答卷
		var custName, smStatus string
		if err := s.db.QueryRow(`SELECT cust_name,status FROM smp_sample WHERE id=?`, *revisit).
			Scan(&custName, &smStatus); err == nil {
			rv := map[string]interface{}{"sampleId": *revisit, "custName": custName, "status": smStatus}
			var sheetID int64
			var sheetStatus, qver string
			err := s.db.QueryRow(`SELECT id,status,qnr_version FROM ans_sheet WHERE sample_id=? ORDER BY id DESC LIMIT 1`, *revisit).
				Scan(&sheetID, &sheetStatus, &qver)
			if err == nil {
				rv["sheet"] = map[string]interface{}{"id": sheetID, "status": sheetStatus, "qnr_version": qver}
			}
			out["revisit"] = rv
		}
	}
	rinfo.GinOK(c, out, "ok")
}

type remarkReq struct {
	Remark *string `json:"remark"`
}

func (s *Service) Accept(c *gin.Context) {
	u := auth.From(c)
	tid, _ := strconv.ParseInt(c.Param("tid"), 10, 64)
	err := s.db.Tx(func(tx *sql.Tx) error {
		var status string
		q := `SELECT w.status FROM wko_ticket w JOIN prj_project p ON p.id=w.project_id WHERE w.id=?`
		args := []interface{}{tid}
		if !auth.HasRoleP(u, "domainAdmin") {
			q += ` AND p.tenant_id=?`
			args = append(args, u.TenantID)
		}
		if err := tx.QueryRow(q, args...).Scan(&status); err != nil {
			rinfo.GinFail(c, rinfo.CodeNotFound, "工单不存在")
			return errAbortW
		}
		if status != "PENDING" {
			rinfo.GinFail(c, rinfo.CodeState, fmt.Sprintf("工单当前 %s，仅 PENDING 可受理", status))
			return errAbortW
		}
		if _, err := tx.Exec(`UPDATE wko_ticket SET status='ACCEPTED',assigned_agent_id=?,accepted_at=? WHERE id=?`,
			u.ID, store.NowISO(), tid); err != nil {
			return err
		}
		rinfo.GinOK(c, gin.H{"ticketId": tid, "status": "ACCEPTED", "agent": u.Name}, "工单已受理")
		return errAbortW
	})
	if err != nil && err != errAbortW {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
	}
}

var wkoPrev = map[string]string{"RESOLVED": "ACCEPTED", "CLOSED": "RESOLVED"}

func (s *Service) advance(c *gin.Context, target string) {
	u := auth.From(c)
	tid, _ := strconv.ParseInt(c.Param("tid"), 10, 64)
	if target == "CLOSED" && !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "归档需要督导及以上权限（groupAdmin）")
		return
	}
	var body struct {
		Remark *string `json:"remark"`
	}
	_ = c.ShouldBindJSON(&body)
	remark := ""
	if body.Remark != nil {
		remark = *body.Remark
	}
	col := "resolved_at"
	if target == "CLOSED" {
		col = "closed_at"
	}
	err := s.db.Tx(func(tx *sql.Tx) error {
		var status string
		var revisit *int64
		q := `SELECT w.status,w.revisit_sample_id FROM wko_ticket w JOIN prj_project p ON p.id=w.project_id WHERE w.id=?`
		args := []interface{}{tid}
		if !auth.HasRoleP(u, "domainAdmin") {
			q += ` AND p.tenant_id=?`
			args = append(args, u.TenantID)
		}
		if err := tx.QueryRow(q, args...).Scan(&status, &revisit); err != nil {
			rinfo.GinFail(c, rinfo.CodeNotFound, "工单不存在")
			return errAbortW
		}
		if status != wkoPrev[target] {
			rinfo.GinFail(c, rinfo.CodeState, fmt.Sprintf("工单当前 %s，不能直接置 %s", status, target))
			return errAbortW
		}
		if target == "CLOSED" && revisit == nil { // 归档 → 自动生成回访样本进项目1（P0 行96）
			var sk int64
			sk = time.Now().UnixNano()
			var callerNo string
			_ = tx.QueryRow(`SELECT caller_no FROM wko_ticket WHERE id=?`, tid).Scan(&callerNo)
			sampleRes, err := tx.Exec(`INSERT INTO smp_sample(project_id,cust_name,gender,status,ext_json,attempts,last_connected_at,shuffle_key,owner_agent_id) VALUES(?,?,?,?,?,?,?,?,?)`,
				1, fmt.Sprintf("回访-工单#%d", tid), "未知", "IDLE", nil, 0, nil, sk, nil)
			if err != nil { return err }
			sid, err := sampleRes.LastInsertId(); if err != nil { return err }
			if _, err := tx.Exec(`INSERT INTO smp_phone VALUES(?,?,?,1,1)`, sid, sid, callerNo); err != nil {
				return err
			}
			revisit = &sid
		}
		ts := store.NowISO()
		if _, err := tx.Exec(`UPDATE wko_ticket SET status=?,remark=?,`+col+`=?,revisit_sample_id=? WHERE id=?`,
			target, remark, ts, revisit, tid); err != nil {
			return err
		}
		data := gin.H{"ticketId": tid, "status": target}
		msg := "工单已办结"
		if target == "CLOSED" {
			msg = "工单已归档"
			if revisit != nil {
				data["revisitSampleId"] = *revisit
				msg += fmt.Sprintf("，已生成回访样本 #%d 进入项目1样本池", *revisit)
			}
		}
		rinfo.GinOK(c, data, msg)
		return errAbortW
	})
	if err != nil && err != errAbortW {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
	}
}

var errAbortW = store.ErrAbort

func (s *Service) Resolve(c *gin.Context) { s.advance(c, "RESOLVED") }
func (s *Service) Close(c *gin.Context)   { s.advance(c, "CLOSED") }
