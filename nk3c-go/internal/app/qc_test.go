// 质检事件流（QC WS）+ 强签 E2E【P0 test_17/18 对应】
package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"nk3c/internal/store"
)

type qcFrame struct {
	Type  string                 `json:"type"`
	Event string                 `json:"event"`
	Data  map[string]interface{} `json:"data"`
}

func login(t *testing.T, tsURL, user, pass string) string {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"loginName": user, "password": pass})
	resp, err := http.Post(tsURL+"/api/auth/login", "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]interface{}{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	return out["data"].(map[string]interface{})["sessionId"].(string)
}

func qcDial(httpURL, tok string) (*websocket.Conn, error) {
	wsURL := "ws" + strings.TrimPrefix(httpURL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/api/qc/ws?token="+tok, nil)
	return conn, err
}

func readFrame(t *testing.T, conn *websocket.Conn) qcFrame {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("读帧失败: %v", err)
	}
	var f qcFrame
	if err := json.Unmarshal(msg, &f); err != nil {
		t.Fatalf("帧解析失败: %v (%s)", err, msg)
	}
	return f
}

func TestQC_WsPush(t *testing.T) {
	ts, db, agentTok := setup(t)
	defer ts.Close()
	adminTok := login(t, ts.URL, "sup01", "123456")

	// 督导订阅质检流
	conn, err := qcDial(ts.URL, adminTok)
	if err != nil {
		t.Fatalf("督导 WS 连接失败: %v", err)
	}
	defer conn.Close()

	// 坐派→作答→提交：三事件应依次到达
	post := func(path string, body map[string]interface{}) map[string]interface{} {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+agentTok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]interface{}{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return out
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/agent/dispatch?projectId=1", nil)
	req.Header.Set("Authorization", "Bearer "+agentTok)
	resp, _ := http.DefaultClient.Do(req)
	d := map[string]interface{}{}
	_ = json.NewDecoder(resp.Body).Decode(&d)
	resp.Body.Close()
	callID := d["data"].(map[string]interface{})["callId"].(float64)

	post("/api/agent/answer", map[string]interface{}{"callId": callID, "questionId": 11, "optionIds": []int64{111}})
	post("/api/agent/result", map[string]interface{}{"callId": callID, "resultCode": "SUCCESS"})

	f1 := readFrame(t, conn)
	if f1.Event != "DIAL" || f1.Data["callId"].(float64) != callID {
		t.Fatalf("首帧应为 DIAL: %+v", f1)
	}
	if f1.Data["agentNo"].(string) != "1020" {
		t.Fatalf("DIAL 帧应带坐席工号: %+v", f1)
	}
	f2 := readFrame(t, conn)
	if f2.Event != "ANSWER" {
		t.Fatalf("第二帧应为 ANSWER: %+v", f2)
	}
	f3 := readFrame(t, conn)
	if f3.Event != "RESULT" || f3.Data["detail"] != "SUCCESS" {
		t.Fatalf("第三帧应为 RESULT/SUCCESS: %+v", f3)
	}
	// 事件落库
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM cti_monitor_event WHERE agent_no='1020'`).Scan(&n)
	if n < 3 {
		t.Fatalf("cti_monitor_event 应 ≥3 行，实际 %d", n)
	}
	// 非督导订阅 → 403 不升级
	r, err := http.Get(ts.URL + "/api/qc/ws?token=" + agentTok)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatalf("坐席订阅质检流应 403，得到 %d", r.StatusCode)
	}
	t.Log("质检 WS 推送 ✓（DIAL→ANSWER→RESULT 三帧 + 落库 + 403 守门）")
}

func TestQC_ForceCheckout(t *testing.T) {
	ts, db, agentTok := setup(t)
	defer ts.Close()
	adminTok := login(t, ts.URL, "sup01", "123456")
	var agentUID int64
	_ = db.QueryRow(`SELECT id FROM sys_user WHERE agent_no='1020'`).Scan(&agentUID)

	// 坐席持有一个 ASSIGNED 样本（DIALING 中）
	post := func(path, tok string, body map[string]interface{}) map[string]interface{} {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(string(b)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]interface{}{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return out
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/agent/dispatch?projectId=1", nil)
	req.Header.Set("Authorization", "Bearer "+agentTok)
	resp, _ := http.DefaultClient.Do(req)
	dd := map[string]interface{}{}
	_ = json.NewDecoder(resp.Body).Decode(&dd)
	resp.Body.Close()

	// 督导订阅后强签
	conn, err := qcDial(ts.URL, adminTok)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	r := post("/api/qc/force-checkout", adminTok, map[string]interface{}{"userId": agentUID})
	data := r["data"].(map[string]interface{})
	if !r["success"].(bool) || data["sessions"].(float64) < 1 {
		t.Fatalf("强签响应异常: %v", r)
	}

	// ① FORCE_LOGOUT 事件帧
	f := readFrame(t, conn)
	if f.Event != "FORCE_LOGOUT" || f.Data["targetAgentNo"].(string) != "1020" {
		t.Fatalf("应推送 FORCE_LOGOUT 帧: %+v", f)
	}
	// ② 坐席旧会话已失效
	req2, _ := http.NewRequest("GET", ts.URL+"/api/monitor/wall", nil)
	req2.Header.Set("Authorization", "Bearer "+agentTok)
	resp2, _ := http.DefaultClient.Do(req2)
	body := map[string]interface{}{}
	_ = json.NewDecoder(resp2.Body).Decode(&body)
	resp2.Body.Close()
	if body["success"] != false {
		t.Fatalf("强签后旧会话应失效: %v", body)
	}
	// ③ 占用样本回池
	var st string
	_ = db.QueryRow(`SELECT status FROM smp_sample WHERE owner_agent_id=? AND status='ASSIGNED'`, agentUID).Scan(&st)
	var idle int
	_ = db.QueryRow(`SELECT COUNT(*) FROM smp_sample WHERE status='IDLE'`).Scan(&idle)
	if idle < 1 {
		t.Fatalf("强签后应有样本回 IDLE 池（last=%s）", st)
	}
	// ④ 非督导调用强签 → 403
	agentTok2 := login(t, ts.URL, "agent01", "123456")
	r403 := post("/api/qc/force-checkout", agentTok2, map[string]interface{}{"userId": agentUID})
	if r403["success"] != false {
		t.Fatalf("坐席强签应被拒: %v", r403)
	}
	t.Log("强签 ✓（会话吊销 + 样本回池 + FORCE_LOGOUT 帧 + 403 守门）")
}

var _ = store.NowISO
