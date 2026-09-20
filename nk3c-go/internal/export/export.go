// Package export 导出中心：答卷明细矩阵 → CSV / XLSX / SPSS .sav（对齐 demo 导出中心与【S3】导出能力）
package export

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"nk3c/internal/store"
)

// Question 导出列（题目）
type Question struct {
	ID    int64
	QNo   int64
	Title string
	Type  string
}

// Matrix 导出矩阵：行=答卷，列=元数据+题目
type Matrix struct {
	ProjectID int64
	Headers   []string
	Types     []int8 // 0=字符串 1=数值（SAV 变量类型）
	Rows      [][]string
}

// SelectColumns 返回列裁剪后的副本，保持导出格式实现对原矩阵透明。
func (m *Matrix) SelectColumns(indexes []int) *Matrix {
	out := &Matrix{ProjectID: m.ProjectID, Headers: make([]string, 0, len(indexes)), Types: make([]int8, 0, len(indexes))}
	for _, i := range indexes {
		if i < 0 || i >= len(m.Headers) {
			continue
		}
		out.Headers = append(out.Headers, m.Headers[i])
		if i < len(m.Types) {
			out.Types = append(out.Types, m.Types[i])
		} else {
			out.Types = append(out.Types, 0)
		}
	}
	for _, row := range m.Rows {
		r := make([]string, 0, len(indexes))
		for _, i := range indexes {
			if i >= 0 && i < len(row) {
				r = append(r, row[i])
			} else {
				r = append(r, "")
			}
		}
		out.Rows = append(out.Rows, r)
	}
	return out
}

// BuildMatrix 组装项目答卷明细（样本/客户/坐席/工号/结果码/版本/状态 + 逐题答案）
func BuildMatrix(db *store.DB, projectID int64) (*Matrix, error) {
	// ① 题目列（按问卷）
	qrows, err := db.Query(`SELECT q.id,q.q_no,q.title,q.q_type FROM qnr_question q
		JOIN prj_project p ON p.questionnaire_id=q.qnr_id WHERE p.id=? ORDER BY q.q_no`, projectID)
	if err != nil {
		return nil, err
	}
	qs := []Question{}
	for qrows.Next() {
		var q Question
		_ = qrows.Scan(&q.ID, &q.QNo, &q.Title, &q.Type)
		qs = append(qs, q)
	}
	qrows.Close()

	// ② 答案 map[sheet]map[qid]展示值
	// 先收集（红线 #5：单连接池下 rows 未关禁止再查询），再统一补选项标签
	type rawAns struct {
		sheetID, qid int64
		opts, txt    sql.NullString
		num          sql.NullFloat64
	}
	raws := []rawAns{}
	arows, err := db.Query(`SELECT a.sheet_id,a.question_id,a.option_ids,a.answer_text,a.numeric_value
		FROM ans_answer a JOIN ans_sheet s ON s.id=a.sheet_id WHERE s.project_id=?`, projectID)
	if err != nil {
		return nil, err
	}
	for arows.Next() {
		var r rawAns
		_ = arows.Scan(&r.sheetID, &r.qid, &r.opts, &r.txt, &r.num)
		raws = append(raws, r)
	}
	arows.Close()
	// 选项标签批量装载
	needOpt := map[string]string{}
	for _, r := range raws {
		if r.opts.Valid && r.opts.String != "" && r.opts.String != "[]" {
			for _, id := range strings.Split(strings.Trim(r.opts.String, "[]"), ",") {
				id = strings.TrimSpace(id)
				if id != "" {
					needOpt[id] = ""
				}
			}
		}
	}
	for id := range needOpt {
		var label string
		_ = db.QueryRow(`SELECT opt_text FROM qnr_option WHERE id=?`, id).Scan(&label)
		needOpt[id] = label
	}
	ans := map[int64]map[int64]string{}
	for _, r := range raws {
		v := ""
		switch {
		case r.opts.Valid && r.opts.String != "" && r.opts.String != "[]":
			names := make([]string, 0, 4)
			for _, id := range strings.Split(strings.Trim(r.opts.String, "[]"), ",") {
				id = strings.TrimSpace(id)
				if id != "" && needOpt[id] != "" {
					names = append(names, needOpt[id])
				}
			}
			v = strings.Join(names, "|")
		case r.num.Valid:
			v = strconv.FormatFloat(r.num.Float64, 'f', -1, 64)
		case r.txt.Valid:
			v = r.txt.String
		}
		if ans[r.sheetID] == nil {
			ans[r.sheetID] = map[int64]string{}
		}
		ans[r.sheetID][r.qid] = v
	}

	// ③ 明细行
	m := &Matrix{ProjectID: projectID}
	m.Headers = []string{"sheetId", "样本ID", "客户", "坐席工号", "坐席", "结果码", "问卷版本", "答卷状态"}
	m.Types = []int8{1, 1, 0, 0, 0, 0, 0, 0} // sheetId/样本ID 数值
	for _, q := range qs {
		m.Headers = append(m.Headers, fmt.Sprintf("Q%d %s", q.QNo, q.Title))
		if q.Type == "number" { // 填空数值题 → SAV 数值变量
			m.Types = append(m.Types, 1)
		} else {
			m.Types = append(m.Types, 0)
		}
	}
	srows, err := db.Query(`SELECT s.id,s.sample_id,smp.cust_name,COALESCE(u.agent_no,''),COALESCE(u.user_name,''),
		COALESCE(c.result_code,''),s.qnr_version,s.status
		FROM ans_sheet s
		JOIN cti_call_record c ON c.id=s.call_id
		JOIN smp_sample smp ON smp.id=s.sample_id
		LEFT JOIN sys_user u ON u.id=s.agent_id
		WHERE s.project_id=? ORDER BY s.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var sheetID, sampleID int64
		var cust, agentNo, agentName, rc, ver, status string
		_ = srows.Scan(&sheetID, &sampleID, &cust, &agentNo, &agentName, &rc, &ver, &status)
		row := []string{fmt.Sprintf("%d", sheetID), fmt.Sprintf("%d", sampleID), cust, agentNo, agentName, rc, ver, status}
		for _, q := range qs {
			row = append(row, ans[sheetID][q.ID])
		}
		m.Rows = append(m.Rows, row)
	}
	return m, nil
}
