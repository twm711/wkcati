// Package project 项目/问卷/样本生命周期（创建→加题→配额→发布→导样→修订→状态机）
package project

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

type Service struct {
	db *store.DB
}

func New(db *store.DB) *Service { return &Service{db: db} }

// allowProject is the first tenant boundary: global administrators may cross
// tenants, while ordinary users can only access projects in their tenant.
func (s *Service) allowProject(c *gin.Context, pid string) bool {
	u := auth.From(c)
	if auth.HasRoleP(u, "domainAdmin") {
		return true
	}
	var tenant, groupID int64
	if err := s.db.QueryRow(`SELECT tenant_id,group_id FROM prj_project WHERE id=?`, pid).Scan(&tenant, &groupID); err != nil || tenant != u.TenantID || (u.GroupID > 0 && groupID > 0 && groupID != u.GroupID) {
		rinfo.GinFail(c, rinfo.CodeNotFound, "项目不存在")
		return false
	}
	return true
}

type row = map[string]interface{}

func (s *Service) List(c *gin.Context) {
	u := auth.From(c)
	q := `SELECT p.id,p.project_code,p.project_name,p.status,p.questionnaire_id,
		q.version,q.status FROM prj_project p LEFT JOIN qnr_questionnaire q ON q.id=p.questionnaire_id`
	args := []interface{}{}
	if !auth.HasRoleP(u, "domainAdmin") {
		q += ` WHERE p.tenant_id=? AND (p.group_id IS NULL OR p.group_id=0 OR p.group_id=?)`
		args = append(args, u.TenantID, u.GroupID)
	}
	q += ` ORDER BY p.id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	base := []struct {
		id, qid            int64
		code, name, status string
		ver, qstatus       sql.NullString
	}{}
	for rows.Next() {
		var b struct {
			id, qid            int64
			code, name, status string
			ver, qstatus       sql.NullString
		}
		if err := rows.Scan(&b.id, &b.code, &b.name, &b.status, &b.qid, &b.ver, &b.qstatus); err != nil {
			rows.Close()
			rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
			return
		}
		base = append(base, b)
	}
	rows.Close()
	out := []row{}
	for _, b := range base {
		id, qid := b.id, b.qid
		var idle, total int
		_ = s.db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(CASE WHEN status='IDLE' THEN 1 ELSE 0 END),0)
			FROM smp_sample WHERE project_id=?`, id).Scan(&total, &idle)
		var qdone, qtarget int
		_ = s.db.QueryRow(`SELECT COALESCE(SUM(done_count),0),COALESCE(SUM(target_count),0)
			FROM qnr_quota_cell WHERE quota_id IN (SELECT id FROM qnr_quota WHERE qnr_id=?)`, qid).Scan(&qdone, &qtarget)
		out = append(out, row{"projectId": id, "projectCode": b.code, "name": b.name, "status": b.status,
			"questionnaireId": qid, "qnrVersion": b.ver.String, "qnrStatus": b.qstatus.String,
			"samples": gin.H{"total": total, "idle": idle}, "quota": gin.H{"done": qdone, "target": qtarget}})
	}
	rinfo.GinOK(c, out, "ok")
}

type createReq struct {
	Name string `json:"name" binding:"required"`
}

func (s *Service) Create(c *gin.Context) {
	u := auth.From(c)
	var req createReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	var pid, nid int64
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(id),0)+1 FROM prj_project`).Scan(&pid)
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(id),0)+1 FROM qnr_questionnaire`).Scan(&nid)
	code := fmt.Sprintf("P2026-%03d", pid)
	if _, err := s.db.Exec(`INSERT INTO prj_project(id,project_code,project_name,status,questionnaire_id,tenant_id,group_id) VALUES(?,?,?,?,?,?,?)`, pid, code, req.Name, "DRAFT", nid, u.TenantID, u.GroupID); err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	if _, err := s.db.Exec(`INSERT INTO qnr_questionnaire VALUES(?,?,?,'DRAFT')`, nid, req.Name, "Draft"); err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	rinfo.GinOK(c, gin.H{"projectId": pid, "questionnaireId": nid}, "项目「"+req.Name+"」已创建（草稿）")
}

func (s *Service) Detail(c *gin.Context) {
	pid := c.Param("pid")
	if !s.allowProject(c, pid) {
		return
	}
	var id, qid int64
	var code, name, status string
	if err := s.db.QueryRow(`SELECT id,project_code,project_name,status,questionnaire_id FROM prj_project WHERE id=?`, pid).
		Scan(&id, &code, &name, &status, &qid); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "项目不存在")
		return
	}
	var ver, qstatus string
	var title string
	_ = s.db.QueryRow(`SELECT title,version,status FROM qnr_questionnaire WHERE id=?`, qid).Scan(&title, &ver, &qstatus)
	qbase := []row{}
	qrows, _ := s.db.Query(`SELECT id,q_no,q_type,title,min_value,max_value FROM qnr_question WHERE qnr_id=? ORDER BY q_no`, qid)
	for qrows != nil && qrows.Next() {
		var qid2, qno int64
		var qt, qtitle string
		var mn, mx sql.NullFloat64
		_ = qrows.Scan(&qid2, &qno, &qt, &qtitle, &mn, &mx)
		qbase = append(qbase, row{"questionId": qid2, "qNo": qno, "type": qt, "title": qtitle,
			"min": mnSafe(mn), "max": mxSafe(mx), "options": []row{}})
	}
	if qrows != nil {
		qrows.Close()
	}
	qs := []row{}
	for _, qb := range qbase {
		opts := []row{}
		orows, _ := s.db.Query(`SELECT id,opt_text FROM qnr_option WHERE question_id=? ORDER BY opt_no`, qb["questionId"])
		for orows != nil && orows.Next() {
			var oid int64
			var txt string
			_ = orows.Scan(&oid, &txt)
			opts = append(opts, row{"optionId": oid, "text": txt})
		}
		if orows != nil {
			orows.Close()
		}
		qb["options"] = opts
		qs = append(qs, qb)
	}
	cells := []row{}
	crows, _ := s.db.Query(`SELECT id,conditions_json,target_count,done_count FROM qnr_quota_cell
		WHERE quota_id IN (SELECT id FROM qnr_quota WHERE qnr_id=?)`, qid)
	for crows != nil && crows.Next() {
		var cid, tgt, done int64
		var cond string
		_ = crows.Scan(&cid, &cond, &tgt, &done)
		var cj interface{}
		_ = json.Unmarshal([]byte(cond), &cj)
		cells = append(cells, row{"cellId": cid, "conditions": cj, "target": tgt, "done": done})
	}
	if crows != nil {
		crows.Close()
	}
	sm := row{}
	srows, _ := s.db.Query(`SELECT status,COUNT(*) FROM smp_sample WHERE project_id=? GROUP BY status`, pid)
	for srows != nil && srows.Next() {
		var st string
		var n int
		_ = srows.Scan(&st, &n)
		sm[st] = n
	}
	if srows != nil {
		srows.Close()
	}
	vers := row{}
	vrows, _ := s.db.Query(`SELECT qnr_version,COUNT(*) FROM ans_sheet WHERE project_id=? GROUP BY qnr_version`, pid)
	for vrows != nil && vrows.Next() {
		var v string
		var n int
		_ = vrows.Scan(&v, &n)
		vers[v] = n
	}
	if vrows != nil {
		vrows.Close()
	}
	rinfo.GinOK(c, row{"projectId": id, "name": name, "status": status,
		"questionnaireId": qid, "qnrVersion": ver, "qnrStatus": qstatus,
		"questions": qs, "quotaCells": cells, "samplesByStatus": sm, "sheetVersions": vers}, "ok")
}

type questionReq struct {
	Text    string   `json:"text" binding:"required"`
	QType   string   `json:"qType"`
	Options []string `json:"options"`
	Min     *float64 `json:"min"`
	Max     *float64 `json:"max"`
}

func (s *Service) AddQuestion(c *gin.Context) {
	if !s.allowProject(c, c.Param("pid")) {
		return
	}
	var req questionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	if req.QType == "" {
		req.QType = "single"
	}
	if req.QType != "single" && req.QType != "number" && req.QType != "text" {
		rinfo.GinFail(c, rinfo.CodeParam, "qType 须为 single/number/text")
		return
	}
	var status string
	var qid int64
	err := s.db.QueryRow(`SELECT status,questionnaire_id FROM prj_project WHERE id=?`, c.Param("pid")).Scan(&status, &qid)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "项目不存在")
		return
	}
	if status != "DRAFT" {
		rinfo.GinFail(c, rinfo.CodeState, "项目 "+status+"，问卷不可编辑（发布后版本冻结）")
		return
	}
	if req.QType == "single" && len(req.Options) == 0 {
		rinfo.GinFail(c, rinfo.CodeParam, "single 题须提供 options")
		return
	}
	var newQID int64
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(id),100)+1 FROM qnr_question`).Scan(&newQID)
	var qno int64
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(q_no),0)+1 FROM qnr_question WHERE qnr_id=?`, qid).Scan(&qno)
	var mn, mx interface{}
	if req.QType == "number" {
		if req.Min != nil {
			mn = *req.Min
		}
		if req.Max != nil {
			mx = *req.Max
		}
	}
	if _, err := s.db.Exec(`INSERT INTO qnr_question VALUES(?,?,?,?,?,?,?,?)`,
		newQID, qid, qno, req.QType, req.Text, 1, mn, mx); err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	for i, txt := range req.Options {
		var oid int64
		_ = s.db.QueryRow(`SELECT COALESCE(MAX(id),100)+1 FROM qnr_option`).Scan(&oid)
		if _, err := s.db.Exec(`INSERT INTO qnr_option VALUES(?,?,?,?,?,?,NULL)`, oid, newQID, i+1, txt, fmt.Sprintf("V%d", i+1), "NEXT"); err != nil {
			rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
			return
		}
	}
	rinfo.GinOK(c, gin.H{"questionId": newQID}, fmt.Sprintf("题目 %d 已加入", newQID))
}

type quotaCellReq struct {
	QuestionID int64   `json:"questionId"`
	InList     []int64 `json:"inList"`
	Target     int64   `json:"target"`
}
type quotaReq struct {
	Name  string         `json:"name"`
	Cells []quotaCellReq `json:"cells"`
}

func (s *Service) SetQuota(c *gin.Context) {
	if !s.allowProject(c, c.Param("pid")) {
		return
	}
	var req quotaReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	var status string
	var qid int64
	if err := s.db.QueryRow(`SELECT status,questionnaire_id FROM prj_project WHERE id=?`, c.Param("pid")).Scan(&status, &qid); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "项目不存在")
		return
	}
	if status != "DRAFT" {
		rinfo.GinFail(c, rinfo.CodeState, "项目已是 "+status+"，配额不可编辑")
		return
	}
	for _, cell := range req.Cells {
		var qt string
		err := s.db.QueryRow(`SELECT q_type FROM qnr_question WHERE id=? AND qnr_id=?`, cell.QuestionID, qid).Scan(&qt)
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeParam, fmt.Sprintf("题目 %d 不属于本项目问卷", cell.QuestionID))
			return
		}
		if qt != "single" || len(cell.InList) == 0 {
			rinfo.GinFail(c, rinfo.CodeParam, "配额格仅支持 single 题")
			return
		}
		for _, oid := range cell.InList {
			var one int
			_ = s.db.QueryRow(`SELECT 1 FROM qnr_option WHERE id=? AND question_id=?`, oid, cell.QuestionID).Scan(&one)
			if one != 1 {
				rinfo.GinFail(c, rinfo.CodeParam, fmt.Sprintf("选项 %d 不属于题目 %d", oid, cell.QuestionID))
				return
			}
		}
		if cell.Target < 1 {
			rinfo.GinFail(c, rinfo.CodeParam, "target 须 ≥1")
			return
		}
	}
	total := int64(0)
	err := s.db.Tx(func(tx *sql.Tx) error {
		var old int64
		err := tx.QueryRow(`SELECT id FROM qnr_quota WHERE qnr_id=?`, qid).Scan(&old)
		if err == nil {
			if _, err := tx.Exec(`DELETE FROM qnr_quota_cell WHERE quota_id=?`, old); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM qnr_quota WHERE id=?`, old); err != nil {
				return err
			}
		}
		var qnID int64
		_ = tx.QueryRow(`SELECT COALESCE(MAX(id),0)+1 FROM qnr_quota`).Scan(&qnID)
		for _, cell := range req.Cells {
			total += cell.Target
			cond, _ := json.Marshal([]map[string]interface{}{{"questionId": cell.QuestionID, "in": cell.InList}})
			var cid int64
			_ = tx.QueryRow(`SELECT COALESCE(MAX(id),0)+1 FROM qnr_quota_cell`).Scan(&cid)
			if _, err := tx.Exec(`INSERT INTO qnr_quota_cell VALUES(?,?,?,?,0)`, cid, qnID, string(cond), cell.Target); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO qnr_quota VALUES(?,?,?,?)`, qnID, qid, req.Name, total); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	rinfo.GinOK(c, gin.H{"cells": len(req.Cells), "target": total}, "配额已设置")
}

func (s *Service) Publish(c *gin.Context) {
	if !s.allowProject(c, c.Param("pid")) {
		return
	}
	var status string
	var qid int64
	if err := s.db.QueryRow(`SELECT status,questionnaire_id FROM prj_project WHERE id=?`, c.Param("pid")).Scan(&status, &qid); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "项目不存在")
		return
	}
	if status != "DRAFT" {
		rinfo.GinFail(c, rinfo.CodeState, "项目已是 "+status+"，无需重复发布")
		return
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM qnr_question WHERE qnr_id=?`, qid).Scan(&n)
	if n == 0 {
		rinfo.GinFail(c, rinfo.CodeParam, "问卷至少需要 1 道题")
		return
	}
	if _, err := s.db.Exec(`UPDATE qnr_questionnaire SET version='v1.0', status='PUBLISHED' WHERE id=?`, qid); err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	if _, err := s.db.Exec(`UPDATE prj_project SET status='RUNNING' WHERE id=?`, c.Param("pid")); err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	rinfo.GinOK(c, gin.H{"projectId": c.Param("pid"), "questions": n}, fmt.Sprintf("项目 %s 已发布并投入运行（问卷 v1.0，%d 题）", c.Param("pid"), n))
}

type reviseReq struct {
	AddQuestions []questionReq `json:"addQuestions" binding:"required"`
}

var versionRe = regexp.MustCompile(`^v(\d+)\.(\d+)$`)

func (s *Service) Revise(c *gin.Context) {
	if !s.allowProject(c, c.Param("pid")) {
		return
	}
	var req reviseReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	var status string
	var qid int64
	var ver string
	if err := s.db.QueryRow(`SELECT p.status,p.questionnaire_id,q.version FROM prj_project p
		JOIN qnr_questionnaire q ON q.id=p.questionnaire_id WHERE p.id=?`, c.Param("pid")).Scan(&status, &qid, &ver); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "项目不存在")
		return
	}
	if status != "RUNNING" {
		rinfo.GinFail(c, rinfo.CodeState, "仅 RUNNING 项目可修订（当前 "+status+"；DRAFT 请用加题端点）")
		return
	}
	m := versionRe.FindStringSubmatch(ver)
	newVer := "v1.1"
	if m != nil {
		minor, _ := strconv.Atoi(m[2])
		newVer = fmt.Sprintf("v%s.%d", m[1], minor+1)
	}
	added := []int64{}
	for _, spec := range req.AddQuestions {
		if spec.QType == "" {
			spec.QType = "single"
		}
		if spec.QType != "single" && spec.QType != "number" && spec.QType != "text" {
			rinfo.GinFail(c, rinfo.CodeParam, "qType 须为 single/number/text")
			return
		}
		if spec.QType == "single" && len(spec.Options) == 0 {
			rinfo.GinFail(c, rinfo.CodeParam, "single 题须提供 options")
			return
		}
		var newQID int64
		_ = s.db.QueryRow(`SELECT COALESCE(MAX(id),100)+1 FROM qnr_question`).Scan(&newQID)
		var qno int64
		_ = s.db.QueryRow(`SELECT COALESCE(MAX(q_no),0)+1 FROM qnr_question WHERE qnr_id=?`, qid).Scan(&qno)
		var mn, mx interface{}
		if spec.QType == "number" {
			if spec.Min != nil {
				mn = *spec.Min
			}
			if spec.Max != nil {
				mx = *spec.Max
			}
		}
		if _, err := s.db.Exec(`INSERT INTO qnr_question VALUES(?,?,?,?,?,?,?,?)`,
			newQID, qid, qno, spec.QType, spec.Text, 1, mn, mx); err != nil {
			rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
			return
		}
		for i, txt := range spec.Options {
			var oid int64
			_ = s.db.QueryRow(`SELECT COALESCE(MAX(id),100)+1 FROM qnr_option`).Scan(&oid)
			s.db.Exec(`INSERT INTO qnr_option VALUES(?,?,?,?,?,?,NULL)`, oid, newQID, i+1, txt, fmt.Sprintf("V%d", i+1), "NEXT")
		}
		added = append(added, newQID)
	}
	s.db.Exec(`UPDATE qnr_questionnaire SET version=? WHERE id=?`, newVer, qid)
	rinfo.GinOK(c, gin.H{"projectId": c.Param("pid"), "oldVersion": ver, "newVersion": newVer, "addedQuestionIds": added},
		fmt.Sprintf("问卷已修订 %s → %s；已答答卷保留 %s 快照，新话务按 %s 执行", ver, newVer, ver, newVer))
}

type statusReq struct {
	Action string `json:"action" binding:"required"`
}

var prjFlow = map[string][2]string{
	"pause": {"RUNNING", "PAUSED"}, "resume": {"PAUSED", "RUNNING"}, "finish": {"RUNNING", "FINISHED"},
}

func (s *Service) Status(c *gin.Context) {
	if !s.allowProject(c, c.Param("pid")) {
		return
	}
	var req statusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	flow, ok := prjFlow[req.Action]
	if !ok {
		rinfo.GinFail(c, rinfo.CodeParam, "action 须为 pause/resume/finish")
		return
	}
	var status string
	if err := s.db.QueryRow(`SELECT status FROM prj_project WHERE id=?`, c.Param("pid")).Scan(&status); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "项目不存在")
		return
	}
	if status != flow[0] {
		rinfo.GinFail(c, rinfo.CodeState, fmt.Sprintf("项目当前 %s，%s 仅允许 %s → %s", status, req.Action, flow[0], flow[1]))
		return
	}
	s.db.Exec(`UPDATE prj_project SET status=? WHERE id=?`, flow[1], c.Param("pid"))
	msgs := map[string]string{"pause": "项目已暂停（派样挂起，进行中话务不受影响）", "resume": "项目已恢复运行", "finish": "项目已结束（终态，不可再变更）"}
	rinfo.GinOK(c, gin.H{"projectId": c.Param("pid"), "status": flow[1]}, msgs[req.Action])
}

type samplesReq struct {
	Names  []string `json:"names" binding:"required"`
	Phones []string `json:"phones"`
}

func (s *Service) ImportSamples(c *gin.Context) {
	if !s.allowProject(c, c.Param("pid")) {
		return
	}
	var req samplesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	pidS := c.Param("pid")
	var pid int64
	pid, _ = strconv.ParseInt(pidS, 10, 64)
	if req.Phones != nil && len(req.Phones) != len(req.Names) {
		rinfo.GinFail(c, rinfo.CodeParam, "phones 与 names 长度不一致")
		return
	}
	var first int64
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(id),900000)+1 FROM smp_sample`).Scan(&first)
	made := []int64{}
	skipped := 0
	for i, nm := range req.Names {
		phone := fmt.Sprintf("137%08d", (first+int64(i))%100000000)
		if req.Phones != nil {
			phone = req.Phones[i]
		}
		var dup int
		_ = s.db.QueryRow(`SELECT 1 FROM smp_phone p JOIN smp_sample s ON s.id=p.sample_id
			WHERE s.project_id=? AND p.phone_no=?`, pid, phone).Scan(&dup)
		if dup == 1 {
			skipped++
			continue
		}
		sid := first + int64(i)
		var sk int64
		_ = s.db.QueryRow(`SELECT COALESCE(MAX(shuffle_key),0)+1 FROM smp_sample`).Scan(&sk)
		if _, err := s.db.Exec(`INSERT INTO smp_sample VALUES(?,?,?,?,?,?,?,?,?,?)`, sid, pid, nm, "未知", "IDLE", nil, 0, nil, nil, sk); err != nil {
			rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
			return
		}
		s.db.Exec(`INSERT INTO smp_phone VALUES(?,?,?,1,1)`, sid, sid, phone)
		made = append(made, sid)
	}
	msg := fmt.Sprintf("已导入 %d 个样本到项目 %s", len(made), pidS)
	if skipped > 0 {
		msg += fmt.Sprintf("，跳过重复 %d 条", skipped)
	}
	rinfo.GinOK(c, gin.H{"sampleIds": made, "count": len(made), "skipped": skipped}, msg)
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
