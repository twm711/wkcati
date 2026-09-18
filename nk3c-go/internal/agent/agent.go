// Package agent 派样引擎/逐题作答/结果码去向/配额原子扣减/答卷审核（对应 nk3c-demo 已验证闭环）
package agent

import (
	"database/sql"
	"errors"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

type Service struct {
	db *store.DB
}

func New(db *store.DB) *Service { return &Service{db: db} }

func param(tx *sql.Tx, code, def string) string {
	var v string
	err := tx.QueryRow(`SELECT param_value FROM sys_param WHERE param_code=?`, code).Scan(&v)
	if err != nil {
		return def
	}
	return v
}

// Dispatch 派样：项目 RUNNING+问卷 PUBLISHED 校验 → 半年原则/黑名单/重拨上限过滤 → BEGIN IMMEDIATE 派样锁
func (s *Service) Dispatch(c *gin.Context) {
	u := auth.From(c)
	projectID := c.DefaultQuery("projectId", "1")
	// 主事务：串行化取样（生产 MySQL 为 SELECT ... FOR UPDATE SKIP LOCKED）
	err := s.db.Tx(func(tx *sql.Tx) error {
		var pstatus string
		var qid int64
		if err := tx.QueryRow(`SELECT status,questionnaire_id FROM prj_project WHERE id=?`, projectID).Scan(&pstatus, &qid); err != nil {
			rinfo.GinFail(c, rinfo.CodeNotFound, fmt.Sprintf("项目 %s 不存在", projectID))
			return errAbort
		}
		var qstatus string
		_ = tx.QueryRow(`SELECT status FROM qnr_questionnaire WHERE id=?`, qid).Scan(&qstatus)
		if pstatus != "RUNNING" || qstatus != "PUBLISHED" {
			rinfo.GinFail(c, rinfo.CodeState, fmt.Sprintf("项目 %s 非运行中（%s）或问卷未发布（%s）", projectID, pstatus, qstatus))
			return errAbort
		}
		halfyear, _ := strconv.ParseInt(param(tx, "halfyear.days", "180"), 10, 64)
		redialMax, _ := strconv.ParseInt(param(tx, "redial.max", "3"), 10, 64)
		cutoff := time.Now().UTC().Add(-time.Duration(halfyear) * 24 * time.Hour).Format("2006-01-02T15:04:05+00:00")
		var sid int64
		var custName, phone string
		var attempts int64
		err := tx.QueryRow(`SELECT s.id,s.cust_name,s.attempts,p.phone_no FROM smp_sample s
			JOIN smp_phone p ON p.sample_id=s.id AND p.valid_flag=1
			WHERE s.project_id=? AND s.status='IDLE' AND s.attempts<?
			AND (s.last_connected_at IS NULL OR s.last_connected_at<?)
			AND NOT EXISTS (SELECT 1 FROM smp_blacklist b WHERE b.phone_no=p.phone_no)
			ORDER BY s.shuffle_key LIMIT 1`, projectID, redialMax, cutoff).Scan(&sid, &custName, &attempts, &phone)
		if err == sql.ErrNoRows {
			rinfo.GinOK(c, nil, "派样失败：过滤后无可用样本（黑名单/半年原则/重拨上限/池空）")
			return errAbort
		}
		if err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE smp_sample SET status='ASSIGNED', owner_agent_id=? WHERE id=? AND status='IDLE'`, u.ID, sid)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			rinfo.GinOK(c, nil, "派样竞争失败，请重试")
			return errAbort
		}
		ts := store.NowISO()
		ins, err := tx.Exec(`INSERT INTO cti_call_record(project_id,sample_id,agent_id,agent_no,caller_no,called_no,status,begin_time)
			VALUES(?,?,?,?,?,?,'DIALING',?)`, projectID, sid, u.ID, u.AgentNo, "95533", phone, ts)
		if err != nil {
			return err
		}
		callID, _ := ins.LastInsertId()
		phones := []string{}
		prows, err := tx.Query(`SELECT phone_no FROM smp_phone WHERE sample_id=? AND valid_flag=1 ORDER BY sort_no`, sid)
		if err != nil {
			return err
		}
		for prows.Next() {
			var p string
			_ = prows.Scan(&p)
			phones = append(phones, p)
		}
		prows.Close()
		// 问卷随项目下发（含选项/范围）
		var qtitle, qver string
		_ = tx.QueryRow(`SELECT title,version FROM qnr_questionnaire WHERE id=?`, qid).Scan(&qtitle, &qver)
		qs := []map[string]interface{}{}
		qrows, err := tx.Query(`SELECT id,q_no,q_type,title,required,min_value,max_value FROM qnr_question WHERE qnr_id=? ORDER BY q_no`, qid)
		if err != nil {
			return err
		}
		for qrows.Next() {
			var qid2, qno int64
			var qt, qtitle2 string
			var req int
			var mn, mx sql.NullFloat64
			if err := qrows.Scan(&qid2, &qno, &qt, &qtitle2, &req, &mn, &mx); err != nil {
				qrows.Close(); return err
			}
			opts := []map[string]interface{}{}
			orows, err := tx.Query(`SELECT id,opt_text,opt_value FROM qnr_option WHERE question_id=? ORDER BY opt_no`, qid2)
			if err != nil {
				qrows.Close(); return err
			}
			for orows.Next() {
				var oid int64
				var txt, val string
				_ = orows.Scan(&oid, &txt, &val)
				opts = append(opts, map[string]interface{}{"optionId": oid, "text": txt, "value": val})
			}
			orows.Close()
			qs = append(qs, map[string]interface{}{"questionId": qid2, "qNo": qno, "type": qt, "title": qtitle2,
				"required": req == 1, "min": mnSafe(mn), "max": mxSafe(mx), "options": opts})
		}
		qrows.Close()
		rinfo.GinOK(c, gin.H{"callId": callID, "sampleId": sid, "custName": custName,
			"attempts": attempts, "phones": phones, "currentPhone": phone,
			"questionnaire": gin.H{"questionnaireId": qid, "title": qtitle, "version": qver, "questions": qs}}, "派样成功")
		return errAbort
	})
	if err != nil && err != errAbort {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
	}
}

var errAbort = store.ErrAbort

func mnSafe(v sql.NullFloat64) interface{} {
	if v.Valid { return v.Float64 }
	return nil
}
func mxSafe(v sql.NullFloat64) interface{} {
	if v.Valid { return v.Float64 }
	return nil
}

type answerReq struct {
	CallID       int64     `json:"callId" binding:"required"`
	QuestionID   int64     `json:"questionId" binding:"required"`
	OptionIDs    []int64   `json:"optionIds"`
	AnswerText   string    `json:"answerText"`
	NumericValue *float64  `json:"numericValue"`
}

// Answer 逐题实时 upsert（断点续答依据）；首题=接通；跨项目题目守卫
func (s *Service) Answer(c *gin.Context) {
	u := auth.From(c)
	var req answerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误"); return
	}
	sheetID, answeredAt, err := s.answerCore(u.ID, req.CallID, req.QuestionID, req.OptionIDs, req.AnswerText, req.NumericValue)
	var be *BizErr
	if errors.As(err, &be) {
		rinfo.GinFail(c, be.Code, be.Msg); return
	}
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error()); return
	}
	rinfo.GinOK(c, gin.H{"sheetId": sheetID, "answeredAt": answeredAt}, "答案已实时入库（断点续答依据）")
}

type resultReq struct {
	CallID     int64  `json:"callId" binding:"required"`
	ResultCode string `json:"resultCode" binding:"required"`
}

// Result 结果码提交：幂等 + 去向判定 + 配额原子扣减
func (s *Service) Result(c *gin.Context) {
	u := auth.From(c)
	var req resultReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误"); return
	}
	data, msg, err := s.resultCore(u.ID, req.CallID, req.ResultCode)
	var be *BizErr
	if errors.As(err, &be) {
		rinfo.GinFail(c, be.Code, be.Msg); return
	}
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error()); return
	}
	if data == nil { // 幂等重复提交：读取首写结果
		var first string
		_ = s.db.QueryRow(`SELECT result_code FROM cti_call_record WHERE id=?`, req.CallID).Scan(&first)
		rinfo.GinOK(c, gin.H{"duplicate": true, "firstResultCode": first}, "幂等：返回首写结果")
		return
	}
	rinfo.GinOK(c, data, msg)
}

// tryConsumeQuota 配额原子扣减：UPDATE ... WHERE done<target（0 行=满格 overflow）
func tryConsumeQuota(tx *sql.Tx, sheetID int64) (bool, error) {
	answers := map[int64][]int64{}
	arows, err := tx.Query(`SELECT question_id,option_ids FROM ans_answer WHERE sheet_id=?`, sheetID)
	if err != nil {
		return false, err
	}
	for arows.Next() {
		var qid int64
		var opts string
		_ = arows.Scan(&qid, &opts)
		var ids []int64
		_ = json.Unmarshal([]byte(opts), &ids)
		answers[qid] = ids
	}
	arows.Close()
	crows, err := tx.Query(`SELECT c.id,c.conditions_json FROM qnr_quota_cell c
		JOIN qnr_quota q ON q.id=c.quota_id
		JOIN ans_sheet s ON s.qnr_id=q.qnr_id WHERE s.id=?`, sheetID)
	if err != nil {
		return false, err
	}
	type cell struct {
		id   int64
		cond string
	}
	var cells []cell
	for crows.Next() {
		var cp cell
		_ = crows.Scan(&cp.id, &cp.cond)
		cells = append(cells, cp)
	}
	crows.Close()
	for _, cp := range cells {
		var conds []struct {
			QuestionID int64   `json:"questionId"`
			In         []int64 `json:"in"`
		}
		if err := json.Unmarshal([]byte(cp.cond), &conds); err != nil {
			return false, err
		}
		hit := true
		for _, cond := range conds {
			chose, ok := answers[cond.QuestionID]
			if !ok {
				hit = false
				break
			}
			match := false
			for _, want := range cond.In {
				for _, got := range chose {
					if want == got {
						match = true
					}
				}
			}
			if !match {
				hit = false
				break
			}
		}
		if hit {
			res, err := tx.Exec(`UPDATE qnr_quota_cell SET done_count=done_count+1 WHERE id=? AND done_count<target_count`, cp.id)
			if err != nil {
				return false, err
			}
			n, _ := res.RowsAffected()
			return n == 1, nil
		}
	}
	return false, nil
}

type auditReq struct {
	Action string  `json:"action" binding:"required"`
	Remark *string `json:"remark"`
}

// Audit 审核状态机：仅 SUBMITTED 可审；REJECT → 样本回 IDLE（重访）
func (s *Service) Audit(c *gin.Context) {
	u := auth.From(c)
	if !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要督导及以上权限（groupAdmin）"); return
	}
	var req auditReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误"); return
	}
	target := map[string]string{"PASS": "AUDITED", "REJECT": "REJECTED", "VOID": "VOID"}[req.Action]
	if target == "" {
		rinfo.GinFail(c, rinfo.CodeParam, "action 须为 PASS/REJECT/VOID"); return
	}
	sheetID, _ := strconv.ParseInt(c.Param("sheetId"), 10, 64)
	err := s.db.Tx(func(tx *sql.Tx) error {
		var status string
		var sampleID int64
		if err := tx.QueryRow(`SELECT status,sample_id FROM ans_sheet WHERE id=?`, sheetID).Scan(&status, &sampleID); err != nil {
			rinfo.GinFail(c, rinfo.CodeNotFound, "答卷不存在"); return errAbort
		}
		if status != "SUBMITTED" {
			rinfo.GinFail(c, rinfo.CodeState, fmt.Sprintf("答卷当前 %s，仅 SUBMITTED 可审核", status)); return errAbort
		}
		remark := ""
		if req.Remark != nil {
			remark = *req.Remark
		}
		if _, err := tx.Exec(`UPDATE ans_sheet SET status=?,audit_remark=? WHERE id=?`, target, remark, sheetID); err != nil { return err }
		if req.Action == "REJECT" {
			tx.Exec(`UPDATE smp_sample SET status='IDLE',owner_agent_id=NULL WHERE id=? AND status='SUCCESS'`, sampleID)
		}
		rinfo.GinOK(c, gin.H{"sheetId": sheetID, "status": target}, "审核完成")
		return errAbort
	})
	if err != nil && err != errAbort {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
	}
}
