// Package agent 派样引擎/逐题作答/结果码去向/配额原子扣减/答卷审核（对应 nk3c-demo 已验证闭环）
package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

// Notifier 质检事件通知（realtime.EventHub 满足该接口；未注入=静默）
type Notifier interface {
	Publish(event string, data map[string]interface{})
}

type Service struct {
	db       *store.DB
	notifier Notifier
	workerID string
}

func New(db *store.DB) *Service {
	return &Service{db: db, workerID: fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())}
}

func (s *Service) acquireWorkerLease() (bool, error) {
	return s.acquireWorkerLeaseNamed("sample-task-reaper")
}

func (s *Service) acquireWorkerLeaseNamed(name string) (bool, error) {
	now := time.Now().UTC()
	nowText := store.TimeFor(s.db.Driver, now)
	untilText := store.TimeFor(s.db.Driver, now.Add(25*time.Second))
	res, err := s.db.Exec(`UPDATE cti_worker_lock SET owner=?,lease_until=? WHERE name=? AND (lease_until<? OR owner=?)`, s.workerID, untilText, name, nowText, s.workerID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		insert := `INSERT OR IGNORE INTO cti_worker_lock(name,owner,lease_until) VALUES(?,?,?)`
		if s.db.Driver == "mysql" {
			insert = `INSERT IGNORE INTO cti_worker_lock(name,owner,lease_until) VALUES(?,?,?)`
		}
		if _, e := s.db.Exec(insert, name, "bootstrap", "1970-01-01T00:00:00+00:00"); e == nil {
			res, err = s.db.Exec(`UPDATE cti_worker_lock SET owner=?,lease_until=? WHERE name=? AND (lease_until<? OR owner=?)`, s.workerID, untilText, name, nowText, s.workerID)
			n, err = res.RowsAffected()
		}
	}
	return n == 1, err
}

// SetNotifier 装配质检事件发布器（app.Build 接线）
func (s *Service) SetNotifier(n Notifier) { s.notifier = n }

// publishQC 事件落库（cti_monitor_event）+ 广播督导（尽力而为，不阻塞主流程）
func (s *Service) publishQC(u *auth.User, event string, callID, sampleID int64, detail string, extra map[string]interface{}) {
	if s.notifier == nil {
		return
	}
	data := map[string]interface{}{"agentNo": "", "userId": int64(0), "tenantId": int64(0)}
	if u != nil {
		data["agentNo"] = u.AgentNo
		data["userId"] = u.ID
		data["tenantId"] = u.TenantID
	}
	if callID != 0 {
		data["callId"] = callID
	}
	if sampleID != 0 {
		data["sampleId"] = sampleID
	}
	if detail != "" {
		data["detail"] = detail
	}
	for k, v := range extra {
		data[k] = v
	}
	tenantID := int64(0)
	if u != nil {
		tenantID = u.TenantID
	}
	_, _ = s.db.Exec(`INSERT INTO cti_monitor_event(agent_id,agent_no,tenant_id,event,call_id,sample_id,detail,created_at)
		VALUES(?,?,?,?,?,?,?,?)`, data["userId"], data["agentNo"], tenantID, event, callID, sampleID, detail, store.NowISO())
	s.notifier.Publish(event, data)
}

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
	var agentState string
	if err := s.db.QueryRow(`SELECT state FROM cti_agent_state WHERE user_id=?`, u.ID).Scan(&agentState); err == nil && agentState != "READY" {
		rinfo.GinFail(c, rinfo.CodeState, "坐席当前状态 "+agentState+"，不可派样")
		return
	}
	projectID := c.Query("projectId")
	var okCall, okSample int64
	var okName string
	// 主事务：串行化取样（生产 MySQL 为 SELECT ... FOR UPDATE SKIP LOCKED）
	err := s.db.Tx(func(tx *sql.Tx) error {
		if projectID == "" {
			var chosen int64
			var pickErr error
			if auth.HasRoleP(u, "domainAdmin") {
				pickErr = tx.QueryRow(`SELECT p.id FROM prj_project p JOIN prj_queue pq ON pq.project_id=p.id JOIN cti_queue q ON q.id=pq.queue_id JOIN cti_agent_queue aq ON aq.queue_id=q.id AND aq.user_id=? AND aq.enabled=1 WHERE p.status='RUNNING' AND q.status=1 ORDER BY q.priority,pq.priority,p.id LIMIT 1`, u.ID).Scan(&chosen)
			} else {
				pickErr = tx.QueryRow(`SELECT p.id FROM prj_project p JOIN prj_queue pq ON pq.project_id=p.id JOIN cti_queue q ON q.id=pq.queue_id JOIN cti_agent_queue aq ON aq.queue_id=q.id AND aq.user_id=? AND aq.enabled=1 WHERE p.status='RUNNING' AND q.status=1 AND p.tenant_id=? AND (p.group_id=0 OR p.group_id=?) ORDER BY q.priority,pq.priority,p.id LIMIT 1`, u.ID, u.TenantID, u.GroupID).Scan(&chosen)
			}
			if pickErr != nil {
				rinfo.GinOK(c, nil, "派样失败：没有可用的优先级队列项目")
				return errAbort
			}
			projectID = strconv.FormatInt(chosen, 10)
		}
		var pstatus string
		var qid, tenantID, groupID int64
		if err := tx.QueryRow(`SELECT status,questionnaire_id,tenant_id,group_id FROM prj_project WHERE id=?`, projectID).Scan(&pstatus, &qid, &tenantID, &groupID); err != nil {
			rinfo.GinFail(c, rinfo.CodeNotFound, fmt.Sprintf("项目 %s 不存在", projectID))
			return errAbort
		}
		if !auth.HasRoleP(u, "domainAdmin") && (tenantID != u.TenantID || (u.GroupID > 0 && groupID > 0 && groupID != u.GroupID)) {
			rinfo.GinFail(c, rinfo.CodeNotFound, "项目不存在")
			return errAbort
		}
		var qstatus string
		_ = tx.QueryRow(`SELECT status FROM qnr_questionnaire WHERE id=?`, qid).Scan(&qstatus)
		if pstatus != "RUNNING" || qstatus != "PUBLISHED" {
			rinfo.GinFail(c, rinfo.CodeState, fmt.Sprintf("项目 %s 非运行中（%s）或问卷未发布（%s）", projectID, pstatus, qstatus))
			return errAbort
		}
		var missingSkills int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM prj_skill_requirement r WHERE r.project_id=? AND NOT EXISTS (SELECT 1 FROM sys_user_skill us WHERE us.user_id=? AND us.skill_id=r.skill_id AND us.level>=r.min_level)`, projectID, u.ID).Scan(&missingSkills)
		if missingSkills > 0 {
			rinfo.GinFail(c, rinfo.CodePermission, "坐席技能不满足项目要求")
			return errAbort
		}
		var queueID sql.NullInt64
		if err := tx.QueryRow(`SELECT queue_id FROM prj_queue WHERE project_id=?`, projectID).Scan(&queueID); err == nil && queueID.Valid {
			var member int
			_ = tx.QueryRow(`SELECT COUNT(*) FROM cti_agent_queue WHERE user_id=? AND queue_id=? AND enabled=1`, u.ID, queueID.Int64).Scan(&member)
			if member == 0 {
				rinfo.GinFail(c, rinfo.CodePermission, "坐席未加入项目队列")
				return errAbort
			}
			var capacity, active int
			_ = tx.QueryRow(`SELECT capacity FROM cti_agent_queue WHERE user_id=? AND queue_id=? AND enabled=1`, u.ID, queueID.Int64).Scan(&capacity)
			_ = tx.QueryRow(`SELECT COUNT(*) FROM cti_sample_task WHERE assigned_user_id=? AND queue_id=? AND status='LEASED'`, u.ID, queueID.Int64).Scan(&active)
			if capacity > 0 && active >= capacity {
				var waitID, queuePriority int64
				_ = tx.QueryRow(`SELECT COALESCE(MAX(id),0)+1 FROM cti_waiting_task`).Scan(&waitID)
				_ = tx.QueryRow(`SELECT priority FROM prj_queue WHERE project_id=?`, projectID).Scan(&queuePriority)
				if queuePriority == 0 {
					queuePriority = 100
				}
				waitSQL := `INSERT INTO cti_waiting_task(id,tenant_id,project_id,queue_id,agent_id,priority,status,created_at) VALUES(?,?,?,?,?,?, 'WAITING',?) ON CONFLICT(agent_id,project_id,status) DO NOTHING`
				if s.db.Driver == "mysql" {
					waitSQL = `INSERT IGNORE INTO cti_waiting_task(id,tenant_id,project_id,queue_id,agent_id,priority,status,created_at) VALUES(?,?,?,?,?,?, 'WAITING',?)`
				}
				_, _ = tx.Exec(waitSQL, waitID, tenantID, projectID, queueID.Int64, u.ID, queuePriority, store.NowFor(s.db.Driver))
				rinfo.GinOK(c, gin.H{"status": "WAITING", "queueId": queueID.Int64, "projectId": projectID}, "已进入等待队列")
				return errAbort
			}
		}
		var strategyMode string
		var strategyMax int
		if tx.QueryRow(`SELECT mode,max_concurrent FROM cti_dial_strategy WHERE project_id=? AND enabled=1`, projectID).Scan(&strategyMode, &strategyMax) == nil {
			var projectActive int
			_ = tx.QueryRow(`SELECT COUNT(*) FROM cti_sample_task WHERE project_id=? AND status='LEASED'`, projectID).Scan(&projectActive)
			if strategyMax > 0 && projectActive >= strategyMax {
				var waitID, queuePriority int64
				if queueID.Valid {
					_ = tx.QueryRow(`SELECT COALESCE(MAX(id),0)+1 FROM cti_waiting_task`).Scan(&waitID)
					_ = tx.QueryRow(`SELECT priority FROM prj_queue WHERE project_id=?`, projectID).Scan(&queuePriority)
					if queuePriority == 0 {
						queuePriority = 100
					}
					waitSQL := `INSERT INTO cti_waiting_task(id,tenant_id,project_id,queue_id,agent_id,priority,status,created_at) VALUES(?,?,?,?,?,?, 'WAITING',?) ON CONFLICT(agent_id,project_id,status) DO NOTHING`
					if s.db.Driver == "mysql" {
						waitSQL = `INSERT IGNORE INTO cti_waiting_task(id,tenant_id,project_id,queue_id,agent_id,priority,status,created_at) VALUES(?,?,?,?,?,?, 'WAITING',?)`
					}
					_, _ = tx.Exec(waitSQL, waitID, tenantID, projectID, queueID.Int64, u.ID, queuePriority, store.NowFor(s.db.Driver))
				}
				rinfo.GinOK(c, gin.H{"status": "WAITING", "projectId": projectID, "mode": strategyMode}, "拨号策略并发已满，已进入等待队列")
				return errAbort
			}
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
			AND (s.next_attempt_at IS NULL OR s.next_attempt_at<=?)
			AND (s.last_connected_at IS NULL OR s.last_connected_at<?)
			AND NOT EXISTS (SELECT 1 FROM smp_blacklist b WHERE b.phone_no=p.phone_no)
			ORDER BY s.shuffle_key LIMIT 1`, projectID, redialMax, store.NowFor(s.db.Driver), cutoff).Scan(&sid, &custName, &attempts, &phone)
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
		queueValue := interface{}(nil)
		if queueID.Valid {
			queueValue = queueID.Int64
		}
		now := time.Now().UTC()
		_, err = tx.Exec(`INSERT INTO cti_sample_task(project_id,sample_id,call_id,queue_id,assigned_user_id,status,leased_at,lease_until) VALUES(?,?,?,?,?,'LEASED',?,?)`,
			projectID, sid, callID, queueValue, u.ID, store.TimeFor(s.db.Driver, now), store.TimeFor(s.db.Driver, now.Add(5*time.Minute)))
		if err != nil {
			return err
		}
		_, _ = tx.Exec(`UPDATE cti_waiting_task SET status='ASSIGNED',assigned_at=? WHERE id=(SELECT id FROM cti_waiting_task WHERE agent_id=? AND project_id=? AND status='WAITING' ORDER BY priority,created_at LIMIT 1)`, store.NowFor(s.db.Driver), u.ID, projectID)
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
				qrows.Close()
				return err
			}
			opts := []map[string]interface{}{}
			orows, err := tx.Query(`SELECT id,opt_text,opt_value FROM qnr_option WHERE question_id=? ORDER BY opt_no`, qid2)
			if err != nil {
				qrows.Close()
				return err
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
		okCall, okSample, okName = callID, sid, custName // 事件在事务提交后发布（单连接池红线 #5）
		return errAbort
	})
	if err != nil && err != errAbort {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
	}
	if okCall != 0 {
		s.publishQC(u, "DIAL", okCall, okSample, okName, nil)
	}
}

func (s *Service) WaitingTasks(c *gin.Context) {
	u := auth.From(c)
	q := `SELECT id,project_id,queue_id,agent_id,priority,status,created_at,COALESCE(assigned_at,'') FROM cti_waiting_task WHERE tenant_id=?`
	args := []interface{}{u.TenantID}
	if !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		q += ` AND agent_id=?`
		args = append(args, u.ID)
	}
	q += ` ORDER BY priority,created_at LIMIT 200`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, pid, qid, aid, pri int64
		var status, created, assigned string
		if rows.Scan(&id, &pid, &qid, &aid, &pri, &status, &created, &assigned) == nil {
			out = append(out, gin.H{"id": id, "projectId": pid, "queueId": qid, "agentId": aid, "priority": pri, "status": status, "createdAt": created, "assignedAt": assigned})
		}
	}
	rinfo.GinOK(c, out, "ok")
}

func (s *Service) CancelWaitingTask(c *gin.Context) {
	u := auth.From(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	cancelSQL := `UPDATE cti_waiting_task SET status='CANCELLED' WHERE id=? AND tenant_id=? AND status='WAITING' AND agent_id=?`
	args := []interface{}{id, u.TenantID, u.ID}
	if auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		cancelSQL = `UPDATE cti_waiting_task SET status='CANCELLED' WHERE id=? AND tenant_id=? AND status='WAITING'`
		args = []interface{}{id, u.TenantID}
	}
	res, err := s.db.Exec(cancelSQL, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		rinfo.GinFail(c, rinfo.CodeNotFound, "等待任务不存在或已处理")
		return
	}
	rinfo.GinOK(c, nil, "等待任务已取消")
}

func (s *Service) DeadTasks(c *gin.Context) {
	u := auth.From(c)
	if !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要督导及以上权限")
		return
	}
	q := `SELECT t.id,t.project_id,t.sample_id,t.call_id,t.retry_count,t.max_retries,t.completed_at,s.cust_name FROM cti_sample_task t JOIN prj_project p ON p.id=t.project_id JOIN smp_sample s ON s.id=t.sample_id WHERE t.status='DEAD'`
	args := []interface{}{}
	if !auth.HasRoleP(u, "domainAdmin") {
		q += ` AND p.tenant_id=?`
		args = append(args, u.TenantID)
	}
	q += ` ORDER BY t.completed_at DESC LIMIT 200`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, pid, sid, cid, retry, max int64
		var completed, name string
		if rows.Scan(&id, &pid, &sid, &cid, &retry, &max, &completed, &name) == nil {
			out = append(out, gin.H{"taskId": id, "projectId": pid, "sampleId": sid, "callId": cid, "retryCount": retry, "maxRetries": max, "completedAt": completed, "custName": name})
		}
	}
	rinfo.GinOK(c, out, "ok")
}

func (s *Service) DeadTaskAttempts(c *gin.Context) {
	u := auth.From(c)
	if !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要督导及以上权限")
		return
	}
	sid, _ := strconv.ParseInt(c.Param("sampleId"), 10, 64)
	var tenant int64
	if err := s.db.QueryRow(`SELECT p.tenant_id FROM smp_sample s JOIN prj_project p ON p.id=s.project_id WHERE s.id=?`, sid).Scan(&tenant); err != nil || (!auth.HasRoleP(u, "domainAdmin") && tenant != u.TenantID) {
		rinfo.GinFail(c, rinfo.CodeNotFound, "样本不存在")
		return
	}
	rows, err := s.db.Query(`SELECT id,task_id,call_id,reason,outcome,COALESCE(failure_code,''),COALESCE(failure_detail,''),q850_cause,COALESCE(q850_text,''),created_at FROM cti_task_attempt WHERE sample_id=? ORDER BY created_at DESC LIMIT 100`, sid)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, taskID, callID int64
		var cause sql.NullInt64
		var reason, outcome, code, detail, q850Text, created string
		if rows.Scan(&id, &taskID, &callID, &reason, &outcome, &code, &detail, &cause, &q850Text, &created) == nil {
			out = append(out, gin.H{"id": id, "taskId": taskID, "callId": callID, "reason": reason, "outcome": outcome, "failureCode": code, "failureDetail": detail, "q850Cause": cause, "q850Text": q850Text, "createdAt": created})
		}
	}
	rinfo.GinOK(c, out, "ok")
}

func (s *Service) RetryDeadTask(c *gin.Context) {
	u := auth.From(c)
	if !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要督导及以上权限")
		return
	}
	sid, _ := strconv.ParseInt(c.Param("sampleId"), 10, 64)
	var tenant int64
	if err := s.db.QueryRow(`SELECT p.tenant_id FROM smp_sample s JOIN prj_project p ON p.id=s.project_id WHERE s.id=? AND s.status='DEAD'`, sid).Scan(&tenant); err != nil || (!auth.HasRoleP(u, "domainAdmin") && tenant != u.TenantID) {
		rinfo.GinFail(c, rinfo.CodeNotFound, "死信样本不存在")
		return
	}
	if _, err := s.db.Exec(`UPDATE smp_sample SET status='IDLE',owner_agent_id=NULL,attempts=0,next_attempt_at=NULL WHERE id=? AND status='DEAD'`, sid); err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	_, _ = s.db.Exec(`UPDATE cti_sample_task SET status='REQUEUED' WHERE sample_id=? AND status='DEAD'`, sid)
	rinfo.GinOK(c, gin.H{"sampleId": sid, "status": "IDLE"}, "死信样本已重新入池")
}

// RenewTask extends the current agent task lease after a heartbeat or answer.
func (s *Service) RenewTask(c *gin.Context) {
	u := auth.From(c)
	var req struct {
		CallID int64 `json:"callId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "callId 参数错误")
		return
	}
	now := time.Now().UTC()
	res, err := s.db.Exec(`UPDATE cti_sample_task SET lease_until=? WHERE call_id=? AND assigned_user_id=? AND status='LEASED' AND lease_until>=?`,
		store.TimeFor(s.db.Driver, now.Add(5*time.Minute)), req.CallID, u.ID, store.TimeFor(s.db.Driver, now))
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		rinfo.GinFail(c, rinfo.CodeState, "任务不存在或租约已过期")
		return
	}
	rinfo.GinOK(c, gin.H{"callId": req.CallID, "leaseSeconds": 300}, "任务租约已续期")
}

// RecoverStaleTasks reconciles leases left by a previous process. SIP legs are
// in-memory, so a DIALING call cannot survive a process restart safely.
func (s *Service) RecoverStaleTasks() (int64, error) {
	ok, err := s.acquireWorkerLease()
	if err != nil || !ok {
		return 0, err
	}
	now := store.NowFor(s.db.Driver)
	var recovered int64
	err = s.db.Tx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT t.id,t.project_id,t.sample_id,t.call_id,t.retry_count,t.max_retries FROM cti_sample_task t JOIN cti_call_record c ON c.id=t.call_id WHERE t.status='LEASED' AND c.status='DIALING'`)
		if err != nil {
			return err
		}
		type task struct{ id, projectID, sampleID, callID, retryCount, maxRetries int64 }
		var tasks []task
		for rows.Next() {
			var x task
			if err := rows.Scan(&x.id, &x.projectID, &x.sampleID, &x.callID, &x.retryCount, &x.maxRetries); err != nil {
				rows.Close()
				return err
			}
			tasks = append(tasks, x)
		}
		rows.Close()
		for _, x := range tasks {
			nextTaskStatus := "EXPIRED"
			if x.retryCount+1 >= x.maxRetries {
				nextTaskStatus = "DEAD"
			}
			res, err := tx.Exec(`UPDATE cti_sample_task SET status=?,retry_count=retry_count+1,completed_at=? WHERE id=? AND status='LEASED'`, nextTaskStatus, now, x.id)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				continue
			}
			_, _ = tx.Exec(`INSERT INTO cti_task_attempt(task_id,project_id,sample_id,call_id,reason,outcome,failure_code,failure_detail,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, x.id, x.projectID, x.sampleID, x.callID, "PROCESS_RESTART", nextTaskStatus, "NA", "服务重启时回收陈旧话务", now)
			nextSampleStatus := "IDLE"
			if x.retryCount+1 >= x.maxRetries {
				nextSampleStatus = "DEAD"
			}
			nextAttemptAt := interface{}(nil)
			if nextSampleStatus == "IDLE" {
				nextAttemptAt = retryAvailableAt(s.db.Driver, x.retryCount+1)
			}
			_, _ = tx.Exec(`UPDATE smp_sample SET status=?,owner_agent_id=NULL,next_attempt_at=? WHERE id=? AND status IN ('ASSIGNED','INCALL')`, nextSampleStatus, nextAttemptAt, x.sampleID)
			_, _ = tx.Exec(`UPDATE cti_call_record SET status='CLOSED',end_time=?,result_code='NA' WHERE id=? AND status='DIALING'`, now, x.callID)
			recovered++
		}
		return nil
	})
	return recovered, err
}

// ReapExpiredTasks safely returns lease-expired tasks to the sample pool.
// It is idempotent and may be called by a periodic worker.
func (s *Service) ReapExpiredTasks() (int64, error) {
	ok, err := s.acquireWorkerLease()
	if err != nil || !ok {
		return 0, err
	}
	now := store.NowFor(s.db.Driver)
	var reclaimed int64
	err = s.db.Tx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT id,project_id,sample_id,call_id,retry_count,max_retries FROM cti_sample_task WHERE status='LEASED' AND lease_until<?`, now)
		if err != nil {
			return err
		}
		type task struct{ id, projectID, sampleID, callID, retryCount, maxRetries int64 }
		var tasks []task
		for rows.Next() {
			var x task
			if err := rows.Scan(&x.id, &x.projectID, &x.sampleID, &x.callID, &x.retryCount, &x.maxRetries); err != nil {
				rows.Close()
				return err
			}
			tasks = append(tasks, x)
		}
		rows.Close()
		for _, x := range tasks {
			// Claim expiry atomically so two service instances cannot both reclaim
			// the same lease after the initial SELECT.
			nextTaskStatus := "EXPIRED"
			if x.retryCount+1 >= x.maxRetries {
				nextTaskStatus = "DEAD"
			}
			res, err := tx.Exec(`UPDATE cti_sample_task SET status=?,retry_count=retry_count+1,completed_at=? WHERE id=? AND status='LEASED' AND lease_until<?`, nextTaskStatus, now, x.id, now)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				continue
			}
			_, _ = tx.Exec(`INSERT INTO cti_task_attempt(task_id,project_id,sample_id,call_id,reason,outcome,failure_code,failure_detail,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, x.id, x.projectID, x.sampleID, x.callID, "LEASE_EXPIRED", nextTaskStatus, "NA", "租约超时未完成", now)
			nextSampleStatus := "IDLE"
			if x.retryCount+1 >= x.maxRetries {
				nextSampleStatus = "DEAD"
			}
			nextAttemptAt := interface{}(nil)
			if nextSampleStatus == "IDLE" {
				nextAttemptAt = retryAvailableAt(s.db.Driver, x.retryCount+1)
			}
			if _, err := tx.Exec(`UPDATE smp_sample SET status=?,owner_agent_id=NULL,next_attempt_at=? WHERE id=? AND status IN ('ASSIGNED','INCALL')`, nextSampleStatus, nextAttemptAt, x.sampleID); err != nil {
				return err
			}
			_, _ = tx.Exec(`UPDATE cti_call_record SET status='CLOSED',end_time=?,result_code='NA' WHERE id=? AND status='DIALING' AND (result_code IS NULL OR result_code='')`, now, x.callID)
			reclaimed++
		}
		return nil
	})
	return reclaimed, err
}

var errAbort = store.ErrAbort

func retryAvailableAt(driver string, retry int64) string {
	delay := 30 * time.Second
	if retry >= 2 {
		delay = 2 * time.Minute
	}
	if retry >= 3 {
		delay = 10 * time.Minute
	}
	return store.TimeFor(driver, time.Now().UTC().Add(delay))
}

func mnSafe(v sql.NullFloat64) interface{} {
	if v.Valid {
		return v.Float64
	}
	return nil
}
func mxSafe(v sql.NullFloat64) interface{} {
	if v.Valid {
		return v.Float64
	}
	return nil
}

type answerReq struct {
	CallID       int64    `json:"callId" binding:"required"`
	QuestionID   int64    `json:"questionId" binding:"required"`
	OptionIDs    []int64  `json:"optionIds"`
	AnswerText   string   `json:"answerText"`
	NumericValue *float64 `json:"numericValue"`
}

// Answer 逐题实时 upsert（断点续答依据）；首题=接通；跨项目题目守卫
func (s *Service) Answer(c *gin.Context) {
	u := auth.From(c)
	var req answerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	sheetID, answeredAt, err := s.answerCore(u.ID, req.CallID, req.QuestionID, req.OptionIDs, req.AnswerText, req.NumericValue)
	var be *BizErr
	if errors.As(err, &be) {
		rinfo.GinFail(c, be.Code, be.Msg)
		return
	}
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	rinfo.GinOK(c, gin.H{"sheetId": sheetID, "answeredAt": answeredAt}, "答案已实时入库（断点续答依据）")
	s.publishQC(u, "ANSWER", req.CallID, 0, "", gin.H{"sheetId": sheetID, "questionId": req.QuestionID})
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
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	data, msg, err := s.resultCore(u.ID, req.CallID, req.ResultCode)
	var be *BizErr
	if errors.As(err, &be) {
		rinfo.GinFail(c, be.Code, be.Msg)
		return
	}
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	if data == nil { // 幂等重复提交：读取首写结果
		var first string
		_ = s.db.QueryRow(`SELECT result_code FROM cti_call_record WHERE id=?`, req.CallID).Scan(&first)
		rinfo.GinOK(c, gin.H{"duplicate": true, "firstResultCode": first}, "幂等：返回首写结果")
		return
	}
	rinfo.GinOK(c, data, msg)
	sid, _ := data["sampleId"].(int64)
	dest, _ := data["destination"].(string)
	s.publishQC(u, "RESULT", req.CallID, sid, req.ResultCode, gin.H{"sampleDest": dest})
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
		rinfo.GinFail(c, rinfo.CodePermission, "需要督导及以上权限（groupAdmin）")
		return
	}
	var req auditReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	target := map[string]string{"PASS": "AUDITED", "REJECT": "REJECTED", "VOID": "VOID"}[req.Action]
	if target == "" {
		rinfo.GinFail(c, rinfo.CodeParam, "action 须为 PASS/REJECT/VOID")
		return
	}
	sheetID, _ := strconv.ParseInt(c.Param("sheetId"), 10, 64)
	err := s.db.Tx(func(tx *sql.Tx) error {
		var status string
		var sampleID int64
		q := `SELECT s.status,s.sample_id,p.tenant_id FROM ans_sheet s JOIN prj_project p ON p.id=s.project_id WHERE s.id=?`
		args := []interface{}{sheetID}
		if !auth.HasRoleP(u, "domainAdmin") {
			q += ` AND p.tenant_id=?`
			args = append(args, u.TenantID)
		}
		var tenantID int64
		if err := tx.QueryRow(q, args...).Scan(&status, &sampleID, &tenantID); err != nil {
			rinfo.GinFail(c, rinfo.CodeNotFound, "答卷不存在")
			return errAbort
		}
		if status != "SUBMITTED" {
			rinfo.GinFail(c, rinfo.CodeState, fmt.Sprintf("答卷当前 %s，仅 SUBMITTED 可审核", status))
			return errAbort
		}
		remark := ""
		if req.Remark != nil {
			remark = *req.Remark
		}
		if _, err := tx.Exec(`UPDATE ans_sheet SET status=?,audit_remark=? WHERE id=?`, target, remark, sheetID); err != nil {
			return err
		}
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

// ProcessWaitingTasks 自动为有空闲容量的等待任务分配样本。
func (s *Service) ProcessWaitingTasks() (int64, error) {
	ok, err := s.acquireWorkerLeaseNamed("waiting-task-dispatch")
	if err != nil || !ok {
		return 0, err
	}
	var assigned int64
	err = s.db.Tx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT w.id,w.project_id,w.queue_id,w.agent_id,u.agent_no
			FROM cti_waiting_task w JOIN sys_user u ON u.id=w.agent_id
			WHERE w.status='WAITING' ORDER BY w.priority,w.created_at LIMIT 50`)
		if err != nil {
			return err
		}
		type waiting struct {
			id, projectID, queueID, agentID int64
			agentNo                         string
		}
		var list []waiting
		for rows.Next() {
			var x waiting
			if err := rows.Scan(&x.id, &x.projectID, &x.queueID, &x.agentID, &x.agentNo); err != nil {
				rows.Close()
				return err
			}
			list = append(list, x)
		}
		rows.Close()
		assignedAgents := map[int64]bool{}
		for _, w := range list {
			// 每轮每个坐席最多自动分配一个，避免单个坐席/队列长期霸占调度批次。
			if assignedAgents[w.agentID] {
				continue
			}
			var strategyMax, projectActive int
			if tx.QueryRow(`SELECT max_concurrent FROM cti_dial_strategy WHERE project_id=? AND enabled=1`, w.projectID).Scan(&strategyMax) == nil {
				_ = tx.QueryRow(`SELECT COUNT(*) FROM cti_sample_task WHERE project_id=? AND status='LEASED'`, w.projectID).Scan(&projectActive)
				if strategyMax > 0 && projectActive >= strategyMax {
					continue
				}
			}
			var capacity, active int
			_ = tx.QueryRow(`SELECT capacity FROM cti_agent_queue WHERE user_id=? AND queue_id=? AND enabled=1`, w.agentID, w.queueID).Scan(&capacity)
			_ = tx.QueryRow(`SELECT COUNT(*) FROM cti_sample_task WHERE assigned_user_id=? AND queue_id=? AND status='LEASED'`, w.agentID, w.queueID).Scan(&active)
			if capacity > 0 && active >= capacity {
				continue
			}
			var sampleID int64
			var phone string
			now := store.NowFor(s.db.Driver)
			if err := tx.QueryRow(`SELECT s.id,p.phone_no FROM smp_sample s JOIN smp_phone p ON p.sample_id=s.id AND p.valid_flag=1 WHERE s.project_id=? AND s.status='IDLE' AND (s.next_attempt_at IS NULL OR s.next_attempt_at<=?) ORDER BY s.shuffle_key LIMIT 1`, w.projectID, now).Scan(&sampleID, &phone); err != nil {
				continue
			}
			res, err := tx.Exec(`UPDATE smp_sample SET status='ASSIGNED',owner_agent_id=? WHERE id=? AND status='IDLE'`, w.agentID, sampleID)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				continue
			}
			ts := store.NowFor(s.db.Driver)
			ins, err := tx.Exec(`INSERT INTO cti_call_record(project_id,sample_id,agent_id,agent_no,caller_no,called_no,status,begin_time) VALUES(?,?,?,?,?,?,'DIALING',?)`, w.projectID, sampleID, w.agentID, w.agentNo, "95533", phone, ts)
			if err != nil {
				return err
			}
			callID, _ := ins.LastInsertId()
			if _, err = tx.Exec(`INSERT INTO cti_sample_task(project_id,sample_id,call_id,queue_id,assigned_user_id,status,leased_at,lease_until) VALUES(?,?,?,?,?,'LEASED',?,?)`, w.projectID, sampleID, callID, w.queueID, w.agentID, ts, store.TimeFor(s.db.Driver, time.Now().UTC().Add(5*time.Minute))); err != nil {
				return err
			}
			if _, err = tx.Exec(`UPDATE cti_waiting_task SET status='ASSIGNED',assigned_at=? WHERE id=? AND status='WAITING'`, ts, w.id); err != nil {
				return err
			}
			assigned++
			assignedAgents[w.agentID] = true
			if s.notifier != nil {
				s.notifier.Publish("WAITING_ASSIGNED", map[string]interface{}{"callId": callID, "sampleId": sampleID, "agentId": w.agentID, "queueId": w.queueID})
			}
		}
		return nil
	})
	return assigned, err
}

func (s *Service) triggerWaitingTasks() {
	var n int
	if s.db.QueryRow(`SELECT COUNT(*) FROM cti_waiting_task WHERE status='WAITING'`).Scan(&n) == nil && n > 0 {
		_, _ = s.ProcessWaitingTasks()
	}
}

// PreviewTask 预览模式锁定一个样本，不立即发起话务。
func (s *Service) PreviewTask(c *gin.Context) {
	u := auth.From(c)
	projectID := c.DefaultQuery("projectId", "1")
	var data map[string]interface{}
	err := s.db.Tx(func(tx *sql.Tx) error {
		var mode string
		if err := tx.QueryRow(`SELECT mode FROM cti_dial_strategy WHERE project_id=? AND enabled=1`, projectID).Scan(&mode); err != nil || mode != "PREVIEW" {
			rinfo.GinFail(c, rinfo.CodeState, "项目未启用预览拨号")
			return errAbort
		}
		var sid int64
		var name, phone string
		now := store.NowFor(s.db.Driver)
		if err := tx.QueryRow(`SELECT s.id,s.cust_name,p.phone_no FROM smp_sample s JOIN smp_phone p ON p.sample_id=s.id AND p.valid_flag=1 WHERE s.project_id=? AND s.status='IDLE' AND (s.next_attempt_at IS NULL OR s.next_attempt_at<=?) ORDER BY s.shuffle_key LIMIT 1`, projectID, now).Scan(&sid, &name, &phone); err != nil {
			rinfo.GinOK(c, nil, "暂无可预览样本")
			return errAbort
		}
		res, err := tx.Exec(`UPDATE smp_sample SET status='PREVIEW',owner_agent_id=? WHERE id=? AND status='IDLE'`, u.ID, sid)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return errAbort
		}
		var qid int64
		_ = tx.QueryRow(`SELECT queue_id FROM prj_queue WHERE project_id=?`, projectID).Scan(&qid)
		var pid int64
		_ = tx.QueryRow(`SELECT COALESCE(MAX(id),0)+1 FROM cti_sample_task`).Scan(&pid)
		if _, err = tx.Exec(`INSERT INTO cti_sample_task(id,project_id,sample_id,call_id,queue_id,assigned_user_id,status,leased_at,lease_until) VALUES(?,?,?,NULL,?,?, 'PREVIEW',?,?)`, pid, projectID, sid, qid, u.ID, now, store.TimeFor(s.db.Driver, time.Now().UTC().Add(30*time.Second))); err != nil {
			return err
		}
		data = map[string]interface{}{"taskId": pid, "sampleId": sid, "custName": name, "phone": phone, "previewSeconds": 30}
		rinfo.GinOK(c, data, "预览样本已锁定")
		return errAbort
	})
	_ = err
}

// ConfirmPreview 将预览样本转为普通 LEASED 话务。
func (s *Service) ConfirmPreview(c *gin.Context) {
	u := auth.From(c)
	tid, _ := strconv.ParseInt(c.Param("taskId"), 10, 64)
	var out map[string]interface{}
	err := s.db.Tx(func(tx *sql.Tx) error {
		var pid, sid, qid int64
		var phone, custName string
		var status string
		if err := tx.QueryRow(`SELECT project_id,sample_id,COALESCE(queue_id,0),status FROM cti_sample_task WHERE id=? AND assigned_user_id=?`, tid, u.ID).Scan(&pid, &sid, &qid, &status); err != nil || status != "PREVIEW" {
			rinfo.GinFail(c, rinfo.CodeNotFound, "预览任务不存在或已失效")
			return errAbort
		}
		_ = tx.QueryRow(`SELECT cust_name FROM smp_sample WHERE id=?`, sid).Scan(&custName)
		if err := tx.QueryRow(`SELECT phone_no FROM smp_phone WHERE sample_id=? AND valid_flag=1 ORDER BY sort_no LIMIT 1`, sid).Scan(&phone); err != nil {
			return err
		}
		now := store.NowFor(s.db.Driver)
		var no string
		_ = tx.QueryRow(`SELECT agent_no FROM sys_user WHERE id=?`, u.ID).Scan(&no)
		ins, err := tx.Exec(`INSERT INTO cti_call_record(project_id,sample_id,agent_id,agent_no,caller_no,called_no,status,begin_time) VALUES(?,?,?,?,?,?,'DIALING',?)`, pid, sid, u.ID, no, "95533", phone, now)
		if err != nil {
			return err
		}
		cid, _ := ins.LastInsertId()
		if _, err = tx.Exec(`UPDATE cti_sample_task SET call_id=?,status='LEASED',leased_at=?,lease_until=? WHERE id=? AND status='PREVIEW'`, cid, now, store.TimeFor(s.db.Driver, time.Now().UTC().Add(5*time.Minute)), tid); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE smp_sample SET status='ASSIGNED' WHERE id=? AND status='PREVIEW'`, sid); err != nil {
			return err
		}
		var qnrID int64
		var qtitle, qver string
		_ = tx.QueryRow(`SELECT questionnaire_id FROM prj_project WHERE id=?`, pid).Scan(&qnrID)
		_ = tx.QueryRow(`SELECT title,version FROM qnr_questionnaire WHERE id=?`, qnrID).Scan(&qtitle, &qver)
		questions := []map[string]interface{}{}
		qrows, qerr := tx.Query(`SELECT id,q_no,q_type,title,required,min_value,max_value FROM qnr_question WHERE qnr_id=? ORDER BY q_no`, qnrID)
		if qerr != nil {
			return qerr
		}
		for qrows.Next() {
			var qid2, qno int64
			var qt, qtit string
			var req int
			var mn, mx sql.NullFloat64
			if qerr = qrows.Scan(&qid2, &qno, &qt, &qtit, &req, &mn, &mx); qerr != nil {
				qrows.Close()
				return qerr
			}
			opts := []map[string]interface{}{}
			orows, e := tx.Query(`SELECT id,opt_text,opt_value FROM qnr_option WHERE question_id=? ORDER BY opt_no`, qid2)
			if e != nil {
				qrows.Close()
				return e
			}
			for orows.Next() {
				var oid int64
				var txt, val string
				_ = orows.Scan(&oid, &txt, &val)
				opts = append(opts, map[string]interface{}{"optionId": oid, "text": txt, "value": val})
			}
			orows.Close()
			questions = append(questions, map[string]interface{}{"questionId": qid2, "qNo": qno, "type": qt, "title": qtit, "required": req == 1, "min": mnSafe(mn), "max": mxSafe(mx), "options": opts})
		}
		qrows.Close()
		out = map[string]interface{}{"taskId": tid, "sampleId": sid, "callId": cid, "custName": custName, "attempts": 0, "phones": []string{phone}, "currentPhone": phone, "questionnaire": gin.H{"questionnaireId": qnrID, "title": qtitle, "version": qver, "questions": questions}}
		rinfo.GinOK(c, out, "预览已确认，话务已建立")
		return errAbort
	})
	_ = err
}

func (s *Service) SkipPreview(c *gin.Context) {
	u := auth.From(c)
	tid, _ := strconv.ParseInt(c.Param("taskId"), 10, 64)
	res, err := s.db.Exec(`UPDATE cti_sample_task SET status='SKIPPED',completed_at=? WHERE id=? AND assigned_user_id=? AND status='PREVIEW'`, store.NowFor(s.db.Driver), tid, u.ID)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		rinfo.GinFail(c, rinfo.CodeNotFound, "预览任务不存在或已失效")
		return
	}
	var sampleID int64
	_ = s.db.QueryRow(`SELECT sample_id FROM cti_sample_task WHERE id=?`, tid).Scan(&sampleID)
	_, _ = s.db.Exec(`UPDATE smp_sample SET status='IDLE',owner_agent_id=NULL WHERE id=? AND status='PREVIEW'`, sampleID)
	rinfo.GinOK(c, nil, "已跳过预览样本")
}

// ClaimProgressiveTask 原子领取一个渐进拨号任务；媒体域负责随后实际发起 SIP 外呼。
func (s *Service) ClaimProgressiveTask() (int64, bool, error) {
	var callID int64
	err := s.db.Tx(func(tx *sql.Tx) error {
		var pid, maxConcurrent int64
		var mode string
		var abandonTarget float64
		if err := tx.QueryRow(`SELECT d.project_id,d.mode,d.max_concurrent,d.abandon_target FROM cti_dial_strategy d JOIN prj_project p ON p.id=d.project_id WHERE d.enabled=1 AND d.mode IN ('PROGRESSIVE','PREDICTIVE') AND p.status='RUNNING' ORDER BY d.project_id LIMIT 1`).Scan(&pid, &mode, &maxConcurrent, &abandonTarget); err != nil {
			return err
		}
		var active int64
		_ = tx.QueryRow(`SELECT COUNT(*) FROM cti_sample_task WHERE project_id=? AND status='LEASED'`, pid).Scan(&active)
		limit := maxConcurrent
		if mode == "PREDICTIVE" {
			var ready int64
			_ = tx.QueryRow(`SELECT COUNT(*) FROM cti_agent_state st JOIN sys_user u ON u.id=st.user_id WHERE st.state='READY' AND u.tenant_id=(SELECT tenant_id FROM prj_project WHERE id=?)`, pid).Scan(&ready)
			var total, connected, abandoned int64
			_ = tx.QueryRow(`SELECT COUNT(*),COALESCE(SUM(CASE WHEN result_code IN ('SUCCESS','PARTIAL') THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN result_code='BREAKOFF' THEN 1 ELSE 0 END),0) FROM cti_call_record WHERE project_id=? AND status='CLOSED'`, pid).Scan(&total, &connected, &abandoned)
			connectRate := 0.5
			abandonRate := 0.0
			if total > 0 {
				connectRate = float64(connected) / float64(total)
				abandonRate = float64(abandoned) / float64(total) * 100
			}
			multiplier := 1.0 + (1.0 - connectRate)
			// 呼损高于目标时立即收缩，明显低于目标时才小幅增加，避免振荡。
			if abandonTarget > 0 && abandonRate > abandonTarget {
				multiplier *= 0.5
			}
			if abandonTarget > 0 && abandonRate < abandonTarget/2 {
				multiplier *= 1.1
			}
			limit = int64(float64(ready) * multiplier)
			if limit < 1 {
				limit = 1
			}
			if limit > maxConcurrent {
				limit = maxConcurrent
			}
		}
		if active >= limit {
			return errAbort
		}
		var agentID, queueID int64
		var agentNo string
		if err := tx.QueryRow(`SELECT aq.user_id,u.agent_no,aq.queue_id FROM cti_agent_queue aq JOIN cti_agent_state st ON st.user_id=aq.user_id AND st.state='READY' JOIN sys_user u ON u.id=aq.user_id JOIN prj_queue pq ON pq.queue_id=aq.queue_id WHERE pq.project_id=? AND aq.enabled=1 ORDER BY aq.user_id LIMIT 1`, pid).Scan(&agentID, &agentNo, &queueID); err != nil {
			return err
		}
		var cap, agentActive int64
		_ = tx.QueryRow(`SELECT capacity FROM cti_agent_queue WHERE user_id=? AND queue_id=?`, agentID, queueID).Scan(&cap)
		_ = tx.QueryRow(`SELECT COUNT(*) FROM cti_sample_task WHERE assigned_user_id=? AND queue_id=? AND status='LEASED'`, agentID, queueID).Scan(&agentActive)
		if cap > 0 && agentActive >= cap {
			return errAbort
		}
		var sid int64
		var phone string
		now := store.NowFor(s.db.Driver)
		if err := tx.QueryRow(`SELECT s.id,p.phone_no FROM smp_sample s JOIN smp_phone p ON p.sample_id=s.id AND p.valid_flag=1 WHERE s.project_id=? AND s.status='IDLE' AND (s.next_attempt_at IS NULL OR s.next_attempt_at<=?) ORDER BY s.shuffle_key LIMIT 1`, pid, now).Scan(&sid, &phone); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE smp_sample SET status='ASSIGNED',owner_agent_id=? WHERE id=? AND status='IDLE'`, agentID, sid)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return errAbort
		}
		ins, err := tx.Exec(`INSERT INTO cti_call_record(project_id,sample_id,agent_id,agent_no,caller_no,called_no,status,begin_time) VALUES(?,?,?,?,?,?,'DIALING',?)`, pid, sid, agentID, agentNo, "95533", phone, now)
		if err != nil {
			return err
		}
		callID, _ = ins.LastInsertId()
		_, err = tx.Exec(`INSERT INTO cti_sample_task(project_id,sample_id,call_id,queue_id,assigned_user_id,status,leased_at,lease_until) VALUES(?,?,?,?,?,'LEASED',?,?)`, pid, sid, callID, queueID, agentID, now, store.TimeFor(s.db.Driver, time.Now().UTC().Add(5*time.Minute)))
		return err
	})
	if err == errAbort {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return callID, callID > 0, nil
}
