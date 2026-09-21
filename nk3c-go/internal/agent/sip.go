// sip.go 话务桥：把业务作答/结果码核心暴露给 media 外呼腿（免 gin；与 HTTP 链路同事务同规则）
package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

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
		var sampleID, recAgent int64
		var status string
		var rc *string
		q := `SELECT sample_id,agent_id,status,result_code FROM cti_call_record WHERE id=?`
		args := []interface{}{callID}
		if agentID != 0 {
			q += ` AND agent_id=?`
			args = append(args, agentID)
		}
		if err := tx.QueryRow(q, args...).Scan(&sampleID, &recAgent, &status, &rc); err != nil {
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
			if _, err := tx.Exec(`UPDATE smp_sample SET status='IDLE', attempts=attempts+1 WHERE id=?`, sampleID); err != nil {
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
		_, _ = tx.Exec(`UPDATE cti_sample_task SET status='COMPLETED',completed_at=? WHERE call_id=? AND status='LEASED'`, ts, callID)
		// 保留真实业务结果作为一次尝试原因，便于区分未接、忙线、拒接等结果码。
		var taskID, projectID int64
		if tx.QueryRow(`SELECT id,project_id FROM cti_sample_task WHERE call_id=? ORDER BY id DESC LIMIT 1`, callID).Scan(&taskID, &projectID) == nil {
			_, _ = tx.Exec(`INSERT INTO cti_task_attempt(task_id,project_id,sample_id,call_id,reason,outcome,failure_code,failure_detail,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, taskID, projectID, sampleID, callID, "RESULT_CODE", resultCode, resultCode, category, ts)
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
		return outData, outMsg, nil
	}
	if err != nil {
		var be *BizErr
		if errors.As(err, &be) {
			return nil, "", be
		}
		return nil, "", err
	}
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
