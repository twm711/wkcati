// Package itests 集成测试：nk3c-demo 31 条断言的核心移植（M1 验收基线）
// 运行：go test ./internal/itests/ -race -v
package itests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"nk3c/internal/app"
	"nk3c/internal/store"
)

var (
	ts     *httptest.Server
	helper *H
)

type H struct{ base string }

func (h *H) do(t *testing.T, method, path, token string, body interface{}) map[string]interface{} {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req, _ := http.NewRequest(method, h.base+path, buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out := map[string]interface{}{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解码失败 %s %s: %v", method, path, err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("%s %s → HTTP %d: %v", method, path, resp.StatusCode, out)
	}
	return out
}

func (h *H) login(t *testing.T, name string) string {
	t.Helper()
	r := h.do(t, "POST", "/api/auth/login", "", map[string]string{"loginName": name, "password": "123456"})
	if r["success"] != true {
		t.Fatalf("登录失败 %s: %v", name, r)
	}
	return r["data"].(map[string]interface{})["sessionId"].(string)
}

// reset 管理员一键重置种子数据（测试间隔离；会话全部失效需重登）
func (h *H) reset(t *testing.T) {
	t.Helper()
	adm := h.login(t, "admin")
	r := h.do(t, "POST", "/api/sys/reset", adm, nil)
	if r["success"] != true {
		t.Fatalf("重置失败: %v", r)
	}
}

func (h *H) fullCall(t *testing.T, token, projectID string, genderOpt int64, score float64, code string) map[string]interface{} {
	t.Helper()
	d := h.do(t, "GET", "/api/agent/dispatch?projectId="+projectID, token, nil)
	if d["success"] != true || d["data"] == nil {
		t.Fatalf("派样失败: %v", d)
	}
	data := d["data"].(map[string]interface{})
	callID := int64(data["callId"].(float64))
	h.do(t, "POST", "/api/agent/answer", token, map[string]interface{}{"callId": callID, "questionId": 11, "optionIds": []int64{genderOpt}})
	h.do(t, "POST", "/api/agent/answer", token, map[string]interface{}{"callId": callID, "questionId": 12, "numericValue": score})
	r := h.do(t, "POST", "/api/agent/result", token, map[string]interface{}{"callId": callID, "resultCode": code})
	if r["success"] != true {
		t.Fatalf("结果码失败: %v", r)
	}
	return r["data"].(map[string]interface{})
}

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "nk3c-test-*")
	defer os.RemoveAll(dir)
	db, err := store.Open("sqlite", filepath.Join(dir, "t.db")+"?_journal=WAL")
	if err != nil {
		panic(err)
	}
	if err := db.Migrate(true); err != nil {
		panic(err)
	}
	a := app.Build(db)
	ts = httptest.NewServer(a.Engine)
	helper = &H{base: ts.URL}
	code := m.Run()
	ts.Close()
	os.Exit(code)
}

// ① 登录/鉴权/角色
func Test01LoginAndAuth(t *testing.T) {
	tok := helper.login(t, "agent01")
	helper.do(t, "GET", "/api/monitor/wall", tok, nil)
	// 错误密码
	r := helper.do(t, "POST", "/api/auth/login", "", map[string]string{"loginName": "agent01", "password": "x"})
	if r["success"] != false {
		t.Fatal("错误密码应被拒")
	}
	// 无 token
	resp, _ := http.Get(ts.URL + "/api/monitor/wall")
	if resp.StatusCode != 401 {
		t.Fatalf("无 token 应 401，得到 %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// ② 首派 101 全流程（配额扣减/统计）
func Test02DispatchFullFlow(t *testing.T) {
	tok := helper.login(t, "agent01")
	d := helper.do(t, "GET", "/api/agent/dispatch?projectId=1", tok, nil)["data"].(map[string]interface{})
	if int64(d["sampleId"].(float64)) != 101 {
		t.Fatalf("首派应为 101（103黑名单/104半年/105超限/102闭环 被过滤），得到 %v", d["sampleId"])
	}
	if d["currentPhone"] != "13800000101" {
		t.Fatalf("号码错误: %v", d["currentPhone"])
	}
	qnr := d["questionnaire"].(map[string]interface{})
	if qnr["title"] != "客户满意度调查_2026" || len(qnr["questions"].([]interface{})) != 2 {
		t.Fatalf("问卷应随项目下发: %v", qnr)
	}
	callID := int64(d["callId"].(float64))
	helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 11, "optionIds": []int64{112}})
	helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 12, "numericValue": 9.0})
	r := helper.do(t, "POST", "/api/agent/result", tok, map[string]interface{}{"callId": callID, "resultCode": "SUCCESS"})["data"].(map[string]interface{})
	if r["destination"] != "CLOSED_SUCCESS" || r["quotaConsume"] != "CONSUMED" {
		t.Fatalf("去向/配额错误: %v", r)
	}
	rep := helper.do(t, "GET", "/api/report/single?questionId=11", tok, nil)["data"].(map[string]interface{})
	if rep["total"].(float64) != 1 {
		t.Fatalf("统计应为 1: %v", rep["total"])
	}
}

// ③ 结果码幂等
func Test03Idempotent(t *testing.T) {
	tok := helper.login(t, "agent01")
	d := helper.do(t, "GET", "/api/agent/dispatch?projectId=1", tok, nil)["data"].(map[string]interface{})
	callID := int64(d["callId"].(float64))
	helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 11, "optionIds": []int64{111}})
	helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 12, "numericValue": 7.0})
	_ = helper.do(t, "POST", "/api/agent/result", tok, map[string]interface{}{"callId": callID, "resultCode": "SUCCESS"})
	r2 := helper.do(t, "POST", "/api/agent/result", tok, map[string]interface{}{"callId": callID, "resultCode": "NA"})
	d2 := r2["data"].(map[string]interface{})
	if r2["success"] != true || d2["duplicate"] != true || d2["firstResultCode"] != "SUCCESS" {
		t.Fatalf("幂等应返回首写结果: %v", r2)
	}
}

// ④ 过滤规则排空 + 数值越界 + NA 回池
func Test04FilterAndDrain(t *testing.T) {
	helper.reset(t)
	tok := helper.login(t, "agent01")
	// 数值越界（后端兜底）
	d := helper.do(t, "GET", "/api/agent/dispatch?projectId=1", tok, nil)["data"].(map[string]interface{})
	callID := int64(d["callId"].(float64))
	helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 11, "optionIds": []int64{111}})
	bad := helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 12, "numericValue": 99.0})
	if bad["success"] != false {
		t.Fatalf("数值越界应被拒: %v", bad)
	}
	// NA 回池（attempts 0→1，仍 < redial.max → 优先级不变，下次仍派 101）
	helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 12, "numericValue": 5.0})
	r := helper.do(t, "POST", "/api/agent/result", tok, map[string]interface{}{"callId": callID, "resultCode": "NA"})["data"].(map[string]interface{})
	if r["destination"] != "REDIAL_POOL" {
		t.Fatalf("NA 应回池: %v", r)
	}
	// 回池重拨：101 连续 NA 两次（attempts 1→2→3 达 redial.max 被排除）
	for i := 0; i < 2; i++ {
		d2 := helper.do(t, "GET", "/api/agent/dispatch?projectId=1", tok, nil)["data"].(map[string]interface{})
		if int64(d2["sampleId"].(float64)) != 101 {
			t.Fatalf("回池后应再派 101（重拨），得到 %v", d2["sampleId"])
		}
		if int64(d2["attempts"].(float64)) != int64(i+1) {
			t.Fatalf("attempts 应递增: %v", d2["attempts"])
		}
		c2 := int64(d2["callId"].(float64))
		helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": c2, "questionId": 11, "optionIds": []int64{112}})
		helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": c2, "questionId": 12, "numericValue": 5.0})
		helper.do(t, "POST", "/api/agent/result", tok, map[string]interface{}{"callId": c2, "resultCode": "NA"})
	}
	// 此时 101 超限被过滤 → 仅剩 106 → 派 106 → NA 回池（106 attempts=1 < 3，仍会再派 106）
	d106 := helper.do(t, "GET", "/api/agent/dispatch?projectId=1", tok, nil)["data"].(map[string]interface{})
	if int64(d106["sampleId"].(float64)) != 106 {
		t.Fatalf("超限后应派 106: %v", d106["sampleId"])
	}
	na106 := int64(d106["callId"].(float64))
	helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": na106, "questionId": 11, "optionIds": []int64{112}})
	helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": na106, "questionId": 12, "numericValue": 6.0})
	helper.do(t, "POST", "/api/agent/result", tok, map[string]interface{}{"callId": na106, "resultCode": "SUCCESS"})
	empty := helper.do(t, "GET", "/api/agent/dispatch?projectId=1", tok, nil)
	if empty["success"] != true || empty["data"] != nil {
		t.Fatalf("106 闭环后应无可用样本: %v", empty)
	}
}

// ⑤ 审核状态机 + 驳回重访
func Test05AuditFlow(t *testing.T) {
	helper.reset(t)
	agt := helper.login(t, "agent01")
	sup := helper.login(t, "sup01")
	d := helper.fullCall(t, agt, "1", 111, 8, "SUCCESS")
	if d["destination"] != "CLOSED_SUCCESS" {
		t.Fatalf("前置 SUCCESS 失败")
	}
	// 坐席无审核权限
	rp := helper.do(t, "POST", fmt.Sprintf("/api/sheet/%v/audit", d["sampleId"]), agt, map[string]string{"action": "PASS"})
	if rp["code"] != "4032" {
		t.Fatalf("坐席审核应 4032: %v", rp)
	}
	sh := helper.do(t, "GET", "/api/sheet?status=SUBMITTED", sup, nil)["data"].(map[string]interface{})
	rows := sh["rows"].([]interface{})
	if len(rows) == 0 {
		t.Fatal("应有待审答卷")
	}
	sheetID := int64(rows[0].(map[string]interface{})["id"].(float64))
	// PASS → AUDITED
	ok := helper.do(t, "POST", fmt.Sprintf("/api/sheet/%d/audit", sheetID), sup, map[string]string{"action": "PASS", "remark": "质检通过"})
	if ok["success"] != true || ok["data"].(map[string]interface{})["status"] != "AUDITED" {
		t.Fatalf("审核失败: %v", ok)
	}
	// 再审 → 4031
	again := helper.do(t, "POST", fmt.Sprintf("/api/sheet/%d/audit", sheetID), sup, map[string]string{"action": "PASS"})
	if again["code"] != "4031" {
		t.Fatalf("重复审核应 4031: %v", again)
	}
}

// ⑥ 跨项目作答守卫 + 项目2 派样
func Test06CrossProjectGuard(t *testing.T) {
	helper.reset(t)
	tok := helper.login(t, "agent01")
	d := helper.do(t, "GET", "/api/agent/dispatch?projectId=2", tok, nil)["data"].(map[string]interface{})
	if int64(d["sampleId"].(float64)) != 201 {
		t.Fatalf("项目2 首派应 201: %v", d["sampleId"])
	}
	callID := int64(d["callId"].(float64))
	cross := helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 11, "optionIds": []int64{111}})
	if cross["success"] != false || cross["code"] != "4001" {
		t.Fatalf("跨项目作答应拒: %v", cross)
	}
	helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 21, "optionIds": []int64{212}})
	helper.do(t, "POST", "/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 22, "optionIds": []int64{221}})
	r := helper.do(t, "POST", "/api/agent/result", tok, map[string]interface{}{"callId": callID, "resultCode": "SUCCESS"})["data"].(map[string]interface{})
	if r["quotaConsume"] != "CONSUMED" {
		t.Fatalf("项目2 配额应独立扣减: %v", r)
	}
}

// ⑦ 项目全生命周期（创建→加题→配额→发布→导样→修订版本化→状态机）
func Test07ProjectLifecycle(t *testing.T) {
	agt := helper.login(t, "agent01")
	sup := helper.login(t, "sup01")
	// 坐席无创建权限
	fb := helper.do(t, "POST", "/api/project", agt, map[string]string{"name": "偷建"})
	if fb["code"] != "4032" {
		t.Fatalf("坐席建项目应 4032: %v", fb)
	}
	pid := int64(helper.do(t, "POST", "/api/project", sup, map[string]string{"name": "生命周期项目"})["data"].(map[string]interface{})["projectId"].(float64))
	// 空问卷不可发布
	empty := helper.do(t, "POST", fmt.Sprintf("/api/project/%d/publish", pid), sup, nil)
	if empty["success"] != false {
		t.Fatalf("无题应拒发布: %v", empty)
	}
	q1 := int64(helper.do(t, "POST", fmt.Sprintf("/api/project/%d/questions", pid), sup,
		map[string]interface{}{"text": "是否满意？", "qType": "single", "options": []string{"满意", "不满意"}})["data"].(map[string]interface{})["questionId"].(float64))
	// DRAFT 不可派样
	dd := helper.do(t, "GET", fmt.Sprintf("/api/agent/dispatch?projectId=%d", pid), agt, nil)
	if dd["code"] != "4031" {
		t.Fatalf("DRAFT 派样应 4031: %v", dd)
	}
	// 配额（选项校验）
	det := helper.do(t, "GET", fmt.Sprintf("/api/project/%d", pid), sup, nil)["data"].(map[string]interface{})
	var optID int64
	for _, o := range det["questions"].([]interface{})[0].(map[string]interface{})["options"].([]interface{}) {
		optID = int64(o.(map[string]interface{})["optionId"].(float64))
		break
	}
	q := helper.do(t, "PUT", fmt.Sprintf("/api/project/%d/quota", pid), sup,
		map[string]interface{}{"name": "配额", "cells": []map[string]interface{}{{"questionId": q1, "inList": []int64{optID}, "target": 3}}})
	if q["success"] != true {
		t.Fatalf("配额失败: %v", q)
	}
	// 发布 + 导样（查重）
	if helper.do(t, "POST", fmt.Sprintf("/api/project/%d/publish", pid), sup, nil)["success"] != true {
		t.Fatal("发布失败")
	}
	imp := helper.do(t, "POST", fmt.Sprintf("/api/project/%d/samples", pid), sup,
		map[string]interface{}{"names": []string{"甲", "乙"}, "phones": []string{"13711112222", "13711112222"}})["data"].(map[string]interface{})
	if imp["count"].(float64) != 1 || imp["skipped"].(float64) != 1 {
		t.Fatalf("查重导入应 1 成功 1 跳过: %v", imp)
	}
	// 发布后加题 → 冻结
	frozen := helper.do(t, "POST", fmt.Sprintf("/api/project/%d/questions", pid), sup, map[string]interface{}{"text": "x", "qType": "text"})
	if frozen["code"] != "4031" {
		t.Fatalf("发布后应冻结: %v", frozen)
	}
	// 派样 + 修订 v1.0→v1.1（旧答卷快照）
	d := helper.do(t, "GET", fmt.Sprintf("/api/agent/dispatch?projectId=%d", pid), agt, nil)["data"].(map[string]interface{})
	callID := int64(d["callId"].(float64))
	helper.do(t, "POST", "/api/agent/answer", agt, map[string]interface{}{"callId": callID, "questionId": q1, "optionIds": []int64{optID}})
	helper.do(t, "POST", "/api/agent/result", agt, map[string]interface{}{"callId": callID, "resultCode": "SUCCESS"})
	rv := helper.do(t, "POST", fmt.Sprintf("/api/project/%d/revise", pid), sup,
		map[string]interface{}{"addQuestions": []map[string]interface{}{{"text": "补充", "qType": "text"}}})
	if rv["success"] != true {
		t.Fatalf("修订失败: %v", rv)
	}
	if rv["data"].(map[string]interface{})["oldVersion"] != "v1.0" || rv["data"].(map[string]interface{})["newVersion"] != "v1.1" {
		t.Fatalf("版本应 v1.0→v1.1: %v", rv["data"])
	}
	sh := helper.do(t, "GET", "/api/sheet", sup, nil)["data"].(map[string]interface{})
	_ = sh
	det2 := helper.do(t, "GET", fmt.Sprintf("/api/project/%d", pid), sup, nil)["data"].(map[string]interface{})
	if det2["qnrVersion"] != "v1.1" {
		t.Fatalf("详情版本应 v1.1: %v", det2["qnrVersion"])
	}
	// 状态机：pause→resume→finish（终态守卫）
	helper.do(t, "POST", fmt.Sprintf("/api/project/%d/status", pid), sup, map[string]string{"action": "pause"})
	paused := helper.do(t, "GET", fmt.Sprintf("/api/agent/dispatch?projectId=%d", pid), agt, nil)
	if paused["code"] != "4031" {
		t.Fatalf("暂停期应拒派样: %v", paused)
	}
	helper.do(t, "POST", fmt.Sprintf("/api/project/%d/status", pid), sup, map[string]string{"action": "resume"})
	helper.do(t, "POST", fmt.Sprintf("/api/project/%d/status", pid), sup, map[string]string{"action": "finish"})
	again := helper.do(t, "POST", fmt.Sprintf("/api/project/%d/status", pid), sup, map[string]string{"action": "pause"})
	if again["code"] != "4031" {
		t.Fatalf("终态应拒变更: %v", again)
	}
}

// ⑧ 工单回访闭环 E2E（P0 行96）
func Test08WorkorderRevisitE2E(t *testing.T) {
	helper.reset(t)
	agt := helper.login(t, "agent01")
	sup := helper.login(t, "sup01")
	// 呼入 → 转人工 → 落单
	rc := helper.do(t, "POST", "/api/ivr/call", agt, map[string]string{"callerNo": "13977778888"})["data"].(map[string]interface{})
	rt := helper.do(t, "POST", fmt.Sprintf("/api/ivr/call/%v/input", rc["sessionId"]), agt, map[string]string{"key": "0"})
	if rt["data"].(map[string]interface{})["outcome"] != "TRANSFER:MANUAL" {
		t.Fatalf("转人工失败: %v", rt)
	}
	wl := helper.do(t, "GET", "/api/workorder?status=PENDING", agt, nil)["data"].(map[string]interface{})
	rows := wl["rows"].([]interface{})
	if len(rows) == 0 {
		t.Fatal("转人工应自动落单")
	}
	tid := int64(rows[0].(map[string]interface{})["id"].(float64))
	// 流转
	if helper.do(t, "POST", fmt.Sprintf("/api/workorder/%d/accept", tid), agt, nil)["success"] != true {
		t.Fatal("受理失败")
	}
	if helper.do(t, "POST", fmt.Sprintf("/api/workorder/%d/resolve", tid), agt, map[string]string{"remark": "需回访"})["success"] != true {
		t.Fatal("办结失败")
	}
	cl := helper.do(t, "POST", fmt.Sprintf("/api/workorder/%d/close", tid), agt, nil)
	if cl["code"] != "4032" {
		t.Fatalf("坐席归档应 4032: %v", cl)
	}
	cl2 := helper.do(t, "POST", fmt.Sprintf("/api/workorder/%d/close", tid), sup, nil)
	if cl2["success"] != true {
		t.Fatalf("归档失败: %v", cl2)
	}
	rid := int64(cl2["data"].(map[string]interface{})["revisitSampleId"].(float64))
	if rid <= 0 {
		t.Fatal("归档应生成回访样本")
	}
	// 回访外呼：派到回访样本（项目1 池内 IDLE，shuffle_key 最大 → 最后派出）
	var hit map[string]interface{}
	for i := 0; i < 8; i++ {
		d := helper.do(t, "GET", "/api/agent/dispatch?projectId=1", agt, nil)
		if d["success"] != true || d["data"] == nil {
			t.Fatalf("派样中断: %v", d)
		}
		dd := d["data"].(map[string]interface{})
		if int64(dd["sampleId"].(float64)) == rid {
			hit = dd
			break
		}
		helper.do(t, "POST", "/api/agent/answer", agt, map[string]interface{}{"callId": dd["callId"], "questionId": 11, "optionIds": []int64{111}})
		helper.do(t, "POST", "/api/agent/answer", agt, map[string]interface{}{"callId": dd["callId"], "questionId": 12, "numericValue": 5.0})
		helper.do(t, "POST", "/api/agent/result", agt, map[string]interface{}{"callId": dd["callId"], "resultCode": "NA"})
	}
	if hit == nil {
		t.Fatal("8 轮内未派出回访样本")
	}
	callID := int64(hit["callId"].(float64))
	helper.do(t, "POST", "/api/agent/answer", agt, map[string]interface{}{"callId": callID, "questionId": 11, "optionIds": []int64{112}})
	helper.do(t, "POST", "/api/agent/answer", agt, map[string]interface{}{"callId": callID, "questionId": 12, "numericValue": 9.0})
	helper.do(t, "POST", "/api/agent/result", agt, map[string]interface{}{"callId": callID, "resultCode": "SUCCESS"})
	// 审核 → AUDITED
	sh := helper.do(t, "GET", "/api/sheet?status=SUBMITTED", sup, nil)["data"].(map[string]interface{})
	var sheetID int64
	for _, r := range sh["rows"].([]interface{}) {
		if int64(r.(map[string]interface{})["sample_id"].(float64)) == rid {
			sheetID = int64(r.(map[string]interface{})["id"].(float64))
		}
	}
	if helper.do(t, "POST", fmt.Sprintf("/api/sheet/%d/audit", sheetID), sup, map[string]string{"action": "PASS"})["success"] != true {
		t.Fatal("回访答卷审核失败")
	}
	// 双链路关联
	det := helper.do(t, "GET", fmt.Sprintf("/api/workorder/%d", tid), sup, nil)["data"].(map[string]interface{})
	rv := det["revisit"].(map[string]interface{})
	if int64(rv["sampleId"].(float64)) != rid {
		t.Fatalf("详情样本不匹配: %v", rv)
	}
	sf := rv["sheet"].(map[string]interface{})
	if sf["status"] != "AUDITED" {
		t.Fatalf("回访答卷应 AUDITED: %v", sf)
	}
}

// ⑨ 监控墙 + IVR 完整走线（调研/留言/无效键）
func Test09MonitorAndIVR(t *testing.T) {
	helper.reset(t)
	tok := helper.login(t, "agent01")
	wall := helper.do(t, "GET", "/api/monitor/wall", tok, nil)["data"].(map[string]interface{})
	found := false
	for _, a := range wall["agents"].([]interface{}) {
		if a.(map[string]interface{})["agentNo"] == "1020" {
			found = true
		}
	}
	if !found {
		t.Fatal("监控墙应有坐席 1020")
	}
	// IVR 调研走线
	rc := helper.do(t, "POST", "/api/ivr/call", tok, nil)["data"].(map[string]interface{})
	if rc["node"].(map[string]interface{})["id"] != "menu" {
		t.Fatal("欢迎音应自动放完到菜单")
	}
	sid := rc["sessionId"].(string)
	if helper.do(t, "POST", "/api/ivr/call/"+sid+"/input", tok, map[string]string{"key": "9"})["data"].(map[string]interface{})["invalidKey"] != true {
		t.Fatal("无效键应重播")
	}
	helper.do(t, "POST", "/api/ivr/call/"+sid+"/input", tok, map[string]string{"key": "1"})
	r2 := helper.do(t, "POST", "/api/ivr/call/"+sid+"/input", tok, map[string]string{"key": "2"})["data"].(map[string]interface{})
	if r2["answers"].(map[string]interface{})["Q11"] != "女" {
		t.Fatalf("IVR 答案错误: %v", r2["answers"])
	}
	r3 := helper.do(t, "POST", "/api/ivr/call/"+sid+"/input", tok, map[string]string{"key": "7"})["data"].(map[string]interface{})
	if r3["outcome"] != "SURVEY" {
		t.Fatalf("结局应 SURVEY: %v", r3["outcome"])
	}
}
