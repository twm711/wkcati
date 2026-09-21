// sip.go 话务桥：把业务作答/结果码核心暴露给 media 外呼腿（免 gin；与 HTTP 链路同事务同规则）
package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"nk3c/internal/store"
)

// BizErr 业务中止（事务仍提交，携带业务码与消息；HTTP/SIP 两端统一翻译）
type BizErr struct {
	Code string
	Msg  string
}

func (e *BizErr) Error() string { return e.Msg }

// OutboundOption 外呼腿可见的选项
type OutboundOption struct {
	OptionID int64
	Text     string
}

// OutboundQuestion 外呼腿可见的题目（Options 为空表示数值题）
type OutboundQuestion struct {
	QuestionID int64
	QNo        int64
	Type       string
	Title      string
	Options    []OutboundOption
}

// OutboundCall 外呼任务上下文（由 media.OutboundCaller 消费）
type OutboundCall struct {
	CallID    int64
	AgentID   int64
	SampleID  int64
	Phone     string
	CallerID  string
	Questions []OutboundQuestion
}

type OutboundLine struct {
	ID     int64
	LineNo string
	Host   string
	Port   int
}

// ReserveOutboundLine 按优先级和剩余容量原子占用线路。
func (s *Service) ReserveOutboundLine(callID int64) (OutboundLine, error) {
	var line OutboundLine
	var tenant int64
	if err := s.db.QueryRow(`SELECT tenant_id FROM prj_project WHERE id=(SELECT project_id FROM cti_call_record WHERE id=?)`, callID).Scan(&tenant); err != nil {
		return line, err
	}
	for i := 0; i < 8; i++ {
		var id int64
		var no, host, state string
		var port int
		now := store.NowFor(s.db.Driver)
		err := s.db.QueryRow(`SELECT id,line_no,COALESCE(host,''),port,circuit_state FROM cti_outbound_line WHERE tenant_id=? AND enabled=1 AND active_calls<capacity AND (circuit_state='CLOSED' OR (circuit_state='OPEN' AND opened_until IS NOT NULL AND opened_until<=?)) ORDER BY priority,id LIMIT 1`, tenant, now).Scan(&id, &no, &host, &port, &state)
		if err != nil {
			return line, err
		}
		if state == "OPEN" {
			probe, err := s.db.Exec(`UPDATE cti_outbound_line SET circuit_state='HALF_OPEN' WHERE id=? AND circuit_state='OPEN' AND opened_until<=?`, id, now)
			if err != nil {
				return line, err
			}
			if n, _ := probe.RowsAffected(); n != 1 {
				continue
			}
		}
		res, err := s.db.Exec(`UPDATE cti_outbound_line SET active_calls=active_calls+1 WHERE id=? AND enabled=1 AND active_calls<capacity AND circuit_state IN ('CLOSED','HALF_OPEN')`, id)
		if err != nil {
			return line, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			leaseUntil := store.TimeFor(s.db.Driver, time.Now().UTC().Add(90*time.Second))
			if _, leaseErr := s.db.Exec(`INSERT INTO cti_outbound_line_lease(call_id,line_id,lease_until,created_at) VALUES(?,?,?,?)`, callID, id, leaseUntil, store.NowFor(s.db.Driver)); leaseErr != nil {
				_, _ = s.db.Exec(`UPDATE cti_outbound_line SET active_calls=CASE WHEN active_calls>0 THEN active_calls-1 ELSE 0 END WHERE id=?`, id)
				return line, leaseErr
			}
			return OutboundLine{ID: id, LineNo: no, Host: host, Port: port}, nil
		}
		if state == "OPEN" {
			_, _ = s.db.Exec(`UPDATE cti_outbound_line SET circuit_state='OPEN',opened_until=? WHERE id=? AND circuit_state='HALF_OPEN'`, store.TimeFor(s.db.Driver, time.Now().UTC().Add(1*time.Minute)), id)
		}
	}
	return line, fmt.Errorf("外呼线路容量竞争失败")
}

func (s *Service) RenewOutboundLine(lineID, callID int64) error {
	leaseUntil := store.TimeFor(s.db.Driver, time.Now().UTC().Add(90*time.Second))
	res, err := s.db.Exec(`UPDATE cti_outbound_line_lease SET lease_until=? WHERE call_id=? AND line_id=?`, leaseUntil, callID, lineID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("线路租约不存在或已过期")
	}
	return nil
}

func (s *Service) ReleaseOutboundLine(lineID, callID int64) error {
	res, err := s.db.Exec(`DELETE FROM cti_outbound_line_lease WHERE call_id=? AND line_id=?`, callID, lineID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	_, err = s.db.Exec(`UPDATE cti_outbound_line SET active_calls=CASE WHEN active_calls>0 THEN active_calls-1 ELSE 0 END WHERE id=?`, lineID)
	return err
}

// ReapOutboundLineLeases 修复进程崩溃留下的线路容量占用。
func (s *Service) ReapOutboundLineLeases() (int64, error) {
	now := store.NowFor(s.db.Driver)
	var released int64
	err := s.db.Tx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT call_id,line_id FROM cti_outbound_line_lease WHERE lease_until<?`, now)
		if err != nil {
			return err
		}
		defer rows.Close()
		type lease struct{ callID, lineID int64 }
		var leases []lease
		for rows.Next() {
			var x lease
			if err := rows.Scan(&x.callID, &x.lineID); err != nil {
				return err
			}
			leases = append(leases, x)
		}
		for _, x := range leases {
			if _, err := tx.Exec(`DELETE FROM cti_outbound_line_lease WHERE call_id=?`, x.callID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE cti_outbound_line SET active_calls=CASE WHEN active_calls>0 THEN active_calls-1 ELSE 0 END WHERE id=?`, x.lineID); err != nil {
				return err
			}
			released++
		}
		return nil
	})
	return released, err
}

// LoadOutbound 装载外呼任务（话务须处于 DIALING）
func (s *Service) LoadOutbound(callID int64) (OutboundCall, error) {
	oc := OutboundCall{CallID: callID, CallerID: "95533"}
	var status string
	if err := s.db.QueryRow(`SELECT agent_id,sample_id,called_no,status FROM cti_call_record WHERE id=?`, callID).
		Scan(&oc.AgentID, &oc.SampleID, &oc.Phone, &status); err != nil {
		return oc, fmt.Errorf("话务 %d 不存在: %w", callID, err)
	}
	if status != "DIALING" {
		return oc, fmt.Errorf("话务 %d 非外呼中状态（%s）", callID, status)
	}
	var qid int64
	if err := s.db.QueryRow(`SELECT questionnaire_id FROM prj_project WHERE id=(SELECT project_id FROM cti_call_record WHERE id=?)`, callID).Scan(&qid); err != nil {
		return oc, fmt.Errorf("项目问卷装载失败: %w", err)
	}
	qrows, err := s.db.Query(`SELECT id,q_no,q_type,title FROM qnr_question WHERE qnr_id=? ORDER BY q_no`, qid)
	if err != nil {
		return oc, err
	}
	base := []OutboundQuestion{}
	for qrows.Next() {
		var q OutboundQuestion
		_ = qrows.Scan(&q.QuestionID, &q.QNo, &q.Type, &q.Title)
		base = append(base, q)
	}
	qrows.Close()
	for _, q := range base {
		if q.Type == "single" || q.Type == "multi" {
			orows, err := s.db.Query(`SELECT id,opt_text FROM qnr_option WHERE question_id=? ORDER BY opt_no`, q.QuestionID)
			if err != nil {
				return oc, err
			}
			for orows.Next() {
				var o OutboundOption
				_ = orows.Scan(&o.OptionID, &o.Text)
				q.Options = append(q.Options, o)
			}
			orows.Close()
		}
		oc.Questions = append(oc.Questions, q)
	}
	return oc, nil
}

// AnswerOutbound 外呼自动作答（agentID=0 时取话务记录所属坐席）
func (s *Service) AnswerOutbound(callID, questionID int64, optionIDs []int64, answerText string, numeric *float64) (int64, string, error) {
	return s.answerCore(0, callID, questionID, optionIDs, answerText, numeric)
}

// FinishOutbound 外呼收尾结果码（幂等语义与 HTTP 一致）
func (s *Service) FinishOutbound(callID int64, resultCode string) (map[string]interface{}, string, error) {
	return s.resultCore(0, callID, resultCode)
}

// RecordFailureCause enriches the latest result attempt with SIP/Q.850 diagnostics.
func (s *Service) RecordFailureCause(callID int64, resultCode, detail string) error {
	var attemptID int64
	if err := s.db.QueryRow(`SELECT id FROM cti_task_attempt WHERE call_id=? AND reason='RESULT_CODE' ORDER BY id DESC LIMIT 1`, callID).Scan(&attemptID); err != nil {
		return err
	}
	q850Cause, q850Text := parseQ850(detail)
	_, err := s.db.Exec(`UPDATE cti_task_attempt SET failure_code=?,failure_detail=?,q850_cause=?,q850_text=? WHERE id=?`, resultCode, detail, q850Cause, q850Text, attemptID)
	return err
}

var q850CauseRE = regexp.MustCompile(`(?i)(?:cause|cause-code)\s*[=:]\s*(\d+)`)
var q850TextRE = regexp.MustCompile(`(?i)text\s*=\s*"([^"]*)"`)

func parseQ850(detail string) (interface{}, interface{}) {
	m := q850CauseRE.FindStringSubmatch(detail)
	if len(m) == 0 {
		return nil, nil
	}
	cause, err := strconv.Atoi(m[1])
	if err != nil {
		return nil, nil
	}
	text := ""
	if t := q850TextRE.FindStringSubmatch(detail); len(t) > 1 {
		text = t[1]
	}
	return cause, text
}

func resultRetryDelay(tx *sql.Tx, resultCode string) time.Duration {
	if param(tx, "redial.backoff.enabled", "0") != "1" {
		return 0
	}
	seconds, _ := strconv.Atoi(param(tx, "redial.delay."+resultCode, "300"))
	if seconds < 0 {
		seconds = 0
	}
	return time.Duration(seconds) * time.Second
}

// ── 核心（HTTP handler 与 SIP 桥共用；BizErr=业务分支已定论、事务提交）──────────

func (s *Service) answerCore(agentID, callID, questionID int64, optionIDs []int64, answerText string, numeric *float64) (int64, string, error) {
	var sheetID int64
	var answeredAt string
	err := s.db.Tx(func(tx *sql.Tx) error {
		var callProject, callSample, recAgent int64
		var status string
		var connect *string
		q := `SELECT project_id,sample_id,agent_id,status,connect_time FROM cti_call_record WHERE id=?`
		args := []interface{}{callID}
		if agentID != 0 {
			q += ` AND agent_id=?`
			args = append(args, agentID)
		}
		if err := tx.QueryRow(q, args...).Scan(&callProject, &callSample, &recAgent, &status, &connect); err != nil {
			return &BizErr{"4041", "话务不存在或非本坐席话务"}
		}
		if status == "CLOSED" {
			return &BizErr{"4091", "话务已结束，不能再作答"}
		}
		var qnrID int64
		if err := tx.QueryRow(`SELECT questionnaire_id FROM prj_project WHERE id=?`, callProject).Scan(&qnrID); err != nil {
			return err
		}
		var one int
		_ = tx.QueryRow(`SELECT 1 FROM qnr_question WHERE id=? AND qnr_id=?`, questionID, qnrID).Scan(&one)
		if one != 1 {
			return &BizErr{"4001", fmt.Sprintf("题目 %d 不属于项目 %d 的问卷（跨项目作答被拒）", questionID, callProject)}
		}
		answeredAt = store.NowISO()
		if connect == nil || *connect == "" {
			if _, err := tx.Exec(`UPDATE cti_call_record SET connect_time=? WHERE id=?`, answeredAt, callID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE smp_sample SET status='INCALL' WHERE id=?`, callSample); err != nil {
				return err
			}
		}
		var qver string
		err := tx.QueryRow(`SELECT s.id, s.qnr_version FROM ans_sheet s WHERE s.call_id=?`, callID).Scan(&sheetID, &qver)
		if err == sql.ErrNoRows {
			var version string
			_ = tx.QueryRow(`SELECT version FROM qnr_questionnaire WHERE id=?`, qnrID).Scan(&version)
			ins, err := tx.Exec(`INSERT INTO ans_sheet(call_id,project_id,sample_id,agent_id,qnr_id,qnr_version,status)
				VALUES(?,?,?,?,?,?,'DOING')`, callID, callProject, callSample, recAgent, qnrID, version)
			if err != nil {
				return err
			}
			sheetID, _ = ins.LastInsertId()
		} else if err != nil {
			return err
		}
		optsJSON := "[]"
		if len(optionIDs) > 0 {
			b, _ := json.Marshal(optionIDs)
			optsJSON = string(b)
		}
		var nv interface{}
		if numeric != nil {
			nv = *numeric
		}
		var mn, mx sql.NullFloat64
		var qt string
		_ = tx.QueryRow(`SELECT q_type,min_value,max_value FROM qnr_question WHERE id=?`, questionID).Scan(&qt, &mn, &mx)
		if qt == "number" && numeric != nil {
			if mn.Valid && *numeric < mn.Float64 || mx.Valid && *numeric > mx.Float64 {
				return &BizErr{"4001", fmt.Sprintf("数值超出范围 %v ~ %v", mnSafe(mn), mxSafe(mx))}
			}
		}
		upsert := `INSERT INTO ans_answer(sheet_id,question_id,option_ids,answer_text,numeric_value,answered_at)
			VALUES(?,?,?,?,?,?)
			ON CONFLICT(sheet_id,question_id) DO UPDATE SET option_ids=excluded.option_ids,
			answer_text=excluded.answer_text,numeric_value=excluded.numeric_value,answered_at=excluded.answered_at`
		if s.db.Driver == "mysql" {
			upsert = `INSERT INTO ans_answer(sheet_id,question_id,option_ids,answer_text,numeric_value,answered_at)
				VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE option_ids=VALUES(option_ids),
				answer_text=VALUES(answer_text),numeric_value=VALUES(numeric_value),answered_at=VALUES(answered_at)`
		}
		_, err = tx.Exec(upsert, sheetID, questionID, optsJSON, answerText, nv, answeredAt)
		return err
	})
	return sheetID, answeredAt, err
}

func (s *Service) resultCore(agentID, callID int64, resultCode string) (map[string]interface{}, string, error) {
	var outData map[string]interface{}
	var outMsg string
	err := s.db.Tx(func(tx *sql.Tx) error {
		var sampleID, recAgent, projectID int64
		var callerNo string
		var status string
		var rc *string
		q := `SELECT sample_id,agent_id,project_id,caller_no,status,result_code FROM cti_call_record WHERE id=?`
		args := []interface{}{callID}
		if agentID != 0 {
			q += ` AND agent_id=?`
			args = append(args, agentID)
		}
		if err := tx.QueryRow(q, args...).Scan(&sampleID, &recAgent, &projectID, &callerNo, &status, &rc); err != nil {
			return &BizErr{"4041", "话务不存在或非本坐席话务"}
		}
		if rc != nil && *rc != "" {
			return store.ErrAbort // 幂等：数据已首写；外层读取返回
		}
		var closes, reopen, hitBlack int
		var category string
		if err := tx.QueryRow(`SELECT category,closes_call,reopen_sample,hit_black_flag FROM smp_status_code WHERE code=?`,
			resultCode).Scan(&category, &closes, &reopen, &hitBlack); err != nil {
			return &BizErr{"4001", "未知结果码"}
		}
		ts := store.NowISO()
		dest := "REDIAL_POOL"
		switch {
		case category == "SUCCESS":
			dest = "CLOSED_SUCCESS"
		case category == "APPOINT":
			dest = "APPOINT_QUEUE"
		case hitBlack == 1:
			dest = "BANNED"
		case category == "FAIL" && closes == 1:
			dest = "CLOSED_" + resultCode
		}
		var sheetID int64
		var sheetStatus string
		err := tx.QueryRow(`SELECT id,status FROM ans_sheet WHERE call_id=?`, callID).Scan(&sheetID, &sheetStatus)
		hasSheet := err == nil
		if hasSheet && closes == 1 {
			sheetStatus = "SUBMITTED"
			if _, err := tx.Exec(`UPDATE ans_sheet SET status='SUBMITTED' WHERE id=?`, sheetID); err != nil {
				return err
			}
		}
		if hitBlack == 1 {
			var phone string
			_ = tx.QueryRow(`SELECT phone_no FROM smp_phone WHERE sample_id=? AND valid_flag=1 ORDER BY sort_no LIMIT 1`, sampleID).Scan(&phone)
			if phone != "" {
				var bl int
				_ = tx.QueryRow(`SELECT 1 FROM smp_blacklist WHERE phone_no=?`, phone).Scan(&bl)
				if bl != 1 {
					var bid int64
					_ = tx.QueryRow(`SELECT COALESCE(MAX(id),0)+1 FROM smp_blacklist`).Scan(&bid)
					if _, err := tx.Exec(`INSERT INTO smp_blacklist VALUES(?,?,?,?)`, bid, phone, "GLOBAL", "自动-"+resultCode); err != nil {
						return err
					}
				}
			}
		}
		if dest == "CLOSED_SUCCESS" {
			if _, err := tx.Exec(`UPDATE smp_sample SET status='SUCCESS', last_connected_at=? WHERE id=?`, ts, sampleID); err != nil {
				return err
			}
		} else if hitBlack == 1 {
			if _, err := tx.Exec(`UPDATE smp_sample SET status='BANNED' WHERE id=?`, sampleID); err != nil {
				return err
			}
		} else if closes == 1 {
			if _, err := tx.Exec(`UPDATE smp_sample SET status='CLOSED' WHERE id=?`, sampleID); err != nil {
				return err
			}
		} else {
			nextAttemptAt := store.TimeFor(s.db.Driver, time.Now().UTC().Add(resultRetryDelay(tx, resultCode)))
			if _, err := tx.Exec(`UPDATE smp_sample SET status='IDLE', attempts=attempts+1, next_attempt_at=? WHERE id=?`, nextAttemptAt, sampleID); err != nil {
				return err
			}
		}
		quotaConsume := ""
		if hasSheet && sheetID > 0 {
			hit, err := tryConsumeQuota(tx, sheetID)
			if err != nil {
				return err
			}
			if hit {
				quotaConsume = "CONSUMED"
			} else {
				quotaConsume = "QUOTA_FULL_OVERFLOW"
			}
		}
		if _, err := tx.Exec(`UPDATE cti_call_record SET status='CLOSED', end_time=?, result_code=? WHERE id=?`,
			ts, resultCode, callID); err != nil {
			return err
		}
		// 线路连续失败触发熔断；成功或非失败结果清零失败连击。
		var tenantID, lineID int64
		var oldState string
		_ = tx.QueryRow(`SELECT tenant_id FROM prj_project WHERE id=?`, projectID).Scan(&tenantID)
		_ = tx.QueryRow(`SELECT id,circuit_state FROM cti_outbound_line WHERE tenant_id=? AND line_no=?`, tenantID, callerNo).Scan(&lineID, &oldState)
		if category == "FAIL" {
			openedUntil := store.TimeFor(s.db.Driver, time.Now().UTC().Add(5*time.Minute))
			_, _ = tx.Exec(`UPDATE cti_outbound_line SET failure_streak=failure_streak+1,circuit_state=CASE WHEN failure_streak+1>=5 THEN 'OPEN' ELSE 'CLOSED' END,opened_until=CASE WHEN failure_streak+1>=5 THEN ? ELSE NULL END WHERE tenant_id=? AND line_no=?`, openedUntil, tenantID, callerNo)
		} else {
			_, _ = tx.Exec(`UPDATE cti_outbound_line SET failure_streak=0,circuit_state='CLOSED',opened_until=NULL WHERE tenant_id=? AND line_no=?`, tenantID, callerNo)
		}
		if lineID > 0 {
			var newState string
			_ = tx.QueryRow(`SELECT circuit_state FROM cti_outbound_line WHERE id=?`, lineID).Scan(&newState)
			recordLineCircuitEvent(tx, lineID, callID, oldState, newState, resultCode, ts)
		}
		_, _ = tx.Exec(`UPDATE cti_sample_task SET status='COMPLETED',completed_at=? WHERE call_id=? AND status='LEASED'`, ts, callID)
		// 保留真实业务结果作为一次尝试原因，便于区分未接、忙线、拒接等结果码。
		var taskID, taskProjectID int64
		if tx.QueryRow(`SELECT id,project_id FROM cti_sample_task WHERE call_id=? ORDER BY id DESC LIMIT 1`, callID).Scan(&taskID, &taskProjectID) == nil {
			_, _ = tx.Exec(`INSERT INTO cti_task_attempt(task_id,project_id,sample_id,call_id,reason,outcome,failure_code,failure_detail,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, taskID, taskProjectID, sampleID, callID, "RESULT_CODE", resultCode, resultCode, category, ts)
		}
		outData = map[string]interface{}{"sampleId": sampleID, "destination": dest}
		if hasSheet {
			outData["sheetStatus"] = sheetStatus
			outData["quotaConsume"] = quotaConsume
		}
		outMsg = fmt.Sprintf("结果码 %s 已提交，样本 → %s", resultCode, dest)
		return store.ErrAbort
	})
	if errors.Is(err, store.ErrAbort) {
		_, _ = s.ProcessWaitingTasks()
		return outData, outMsg, nil
	}
	if err != nil {
		var be *BizErr
		if errors.As(err, &be) {
			return nil, "", be
		}
		return nil, "", err
	}
	_, _ = s.ProcessWaitingTasks()
	return outData, outMsg, nil
}

// MarkBridgeConnect 桥接接通：写 connect_time + 样本 INCALL（真人坐席模式；结果码由坐席事后手工提交）
func (s *Service) MarkBridgeConnect(callID int64) error {
	return s.db.Tx(func(tx *sql.Tx) error {
		var sampleID int64
		var connect *string
		if err := tx.QueryRow(`SELECT sample_id,connect_time FROM cti_call_record WHERE id=?`, callID).Scan(&sampleID, &connect); err != nil {
			return &BizErr{"4041", "话务不存在"}
		}
		if connect != nil && *connect != "" {
			return nil // 已标记（幂等）
		}
		ts := store.NowISO()
		if _, err := tx.Exec(`UPDATE cti_call_record SET connect_time=? WHERE id=?`, ts, callID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE smp_sample SET status='INCALL' WHERE id=?`, sampleID)
		return err
	})
}

func recordLineCircuitEvent(tx *sql.Tx, lineID, callID int64, fromState, toState, reason, created string) {
	if fromState == toState {
		return
	}
	_, _ = tx.Exec(`INSERT INTO cti_line_circuit_event(line_id,call_id,from_state,to_state,reason,created_at) VALUES(?,?,?,?,?,?)`, lineID, callID, fromState, toState, reason, created)
}
