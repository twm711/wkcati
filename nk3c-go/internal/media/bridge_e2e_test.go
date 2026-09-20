// B2BUA 桥接 E2E：派样 → 客户腿+坐席腿 真实 SIP 双人通话 → 接通联动 → 挂断 → 人工结果码闭环
// flow 热更 E2E：PUT /api/ivr/flow 即时生效 → 真实 SIP 呼入按新流程走线
package media_test

import (
	"bytes"
	"context"
	"log/slog"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	sip "github.com/emiago/sipgo/sip"

	"nk3c/internal/agent"
	"nk3c/internal/app"
	"nk3c/internal/ivr"
	"nk3c/internal/media"
	"nk3c/internal/store"
	"nk3c/internal/workorder"
)

// TestBridgeAgentCallE2E：真人坐席外呼全动线（派样→桥接→通话→挂断→人工结果码）
func TestBridgeAgentCallE2E(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelWarn)
	dir := t.TempDir()
	db, err := store.Open("sqlite", filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}

	// 客户端自动应答器（同时扮演"客户"与"坐席"两个 UAS；客户 3.5s 后挂断）
	ctx, cancel := contextWithCancel()
	defer cancel()
	mkAnswer := func(name string, port int, hangupAfter time.Duration) {
		ua, _ := sipgo.NewUA()
		dg := diago.NewDiago(ua, diago.WithTransport(diago.Transport{Transport: "udp", BindHost: "127.0.0.1", BindPort: port}))
		go func() {
			_ = dg.Serve(ctx, func(in *diago.DialogServerSession) {
				_ = in.Trying()
				_ = in.Ringing()
				if err := in.Answer(); err != nil {
					return
				}
				t.Logf("[%s] 已接通", name)
				time.Sleep(hangupAfter)
				_ = in.Hangup(ctx)
			})
		}()
	}
	mkAnswer("客户", 25080, 3500*time.Millisecond)
	mkAnswer("坐席", 25081, 30*time.Second)
	time.Sleep(400 * time.Millisecond)

	// 话务域 + HTTP 栈（bridge 路由）
	ivSvc := ivr.New(db, workorder.New(db))
	srv := &media.SIPServer{Driver: ivSvc, BindHost: "127.0.0.1", BindPort: 25079, DtmfWait: 4 * time.Second}
	go func() { _ = srv.Start(ctx) }()
	for srv.Outbound == nil {
		time.Sleep(100 * time.Millisecond)
	}
	srv.Outbound.Driver = agent.New(db)
	srv.Outbound.PeerHost, srv.Outbound.PeerPort = "127.0.0.1", 25080
	a := app.Build(db)
	a.RegisterDial(srv.Outbound, agent.New(db))
	ts := httptest.NewServer(a.Engine)
	defer ts.Close()

	post := func(path, tok string, body interface{}) map[string]interface{} {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+path, bytes.NewReader(b))
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
	get := func(path, tok string) map[string]interface{} {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
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

	tok := post("/api/auth/login", "", map[string]string{"loginName": "agent01", "password": "123456"})["data"].(map[string]interface{})["sessionId"].(string)
	dd := get("/api/agent/dispatch?projectId=1", tok)["data"].(map[string]interface{})
	if dd == nil {
		t.Fatal("派样失败")
	}
	callID := int64(dd["callId"].(float64))
	sampleID := int64(dd["sampleId"].(float64))
	t.Logf("派样 #%d 样本 %d，发起桥接外呼…", callID, sampleID)

	// ① 桥接（阻塞至客户 3.5s 后挂断）；通话中坐席实时录入答案（真人动线）
	ansDone := make(chan bool, 1)
	go func() {
		defer func() { ansDone <- true }()
		time.Sleep(1500 * time.Millisecond) // 桥接已建立、connect 已标记
		ar := post("/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 11, "optionIds": []int64{112}})
		if ar["success"] != true {
			t.Logf("[warn] 通话中作答被拒: %v", ar)
		}
	}()
	br := post(fmt.Sprintf("/api/agent/calls/%d/bridge", callID), tok, map[string]string{"agentUri": "127.0.0.1:25081"})
	if br["success"] != true {
		t.Fatalf("桥接失败: %v", br)
	}
	<-ansDone
	t.Log("桥接已释放（客户挂断）")

	// ② 接通联动断言：connect_time + 样本 INCALL
	var connect *string
	var sampleStatus string
	poll(t, 4*time.Second, func() bool {
		_ = db.QueryRow(`SELECT connect_time FROM cti_call_record WHERE id=?`, callID).Scan(&connect)
		_ = db.QueryRow(`SELECT status FROM smp_sample WHERE id=?`, sampleID).Scan(&sampleStatus)
		return connect != nil
	})
	if connect == nil || *connect == "" {
		t.Fatal("connect_time 未标记")
	}
	if sampleStatus != "INCALL" {
		t.Fatalf("样本应 INCALL，得到 %s", sampleStatus)
	}
	// ③ 话务保持 OPEN（真人动线：结果码人工提交）
	var rc *string
	_ = db.QueryRow(`SELECT result_code FROM cti_call_record WHERE id=?`, callID).Scan(&rc)
	if rc != nil && *rc != "" {
		t.Fatalf("桥接结束不应自动写结果码（人工动线），得到 %s", *rc)
	}
	// ④ 坐席人工提交结果码 → 闭环
	res := post("/api/agent/result", tok, map[string]interface{}{"callId": callID, "resultCode": "SUCCESS"})
	if res["success"] != true {
		t.Fatalf("人工结果码失败: %v", res)
	}
	rd := res["data"].(map[string]interface{})
	if rd["destination"] != "CLOSED_SUCCESS" || rd["quotaConsume"] != "CONSUMED" {
		t.Fatalf("去向/配额错误: %v", rd)
	}
	t.Log("真人坐席外呼全动线 ✓（桥接→接通联动→人工结果码→CLOSED_SUCCESS+CONSUMED）")
}

// TestFlowHotSwapSIP：PUT /api/ivr/flow 即时生效，真实 SIP 呼入按新流程走线
func TestFlowHotSwapSIP(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelWarn)
	dir := t.TempDir()
	db, err := store.Open("sqlite", filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}
	ivSvc := ivr.New(db, workorder.New(db))
	srv := &media.SIPServer{Driver: ivSvc, BindHost: "127.0.0.1", BindPort: 25082, DtmfWait: 4 * time.Second}
	go func() { _ = srv.Start(context.Background()) }()
	time.Sleep(400 * time.Millisecond)

	a := app.Build(db)
	ts := httptest.NewServer(a.Engine)
	defer ts.Close()
	req2 := func(method, path, tok string, body interface{}) map[string]interface{} {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(b))
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
	adm := req2("POST", "/api/auth/login", "", map[string]string{"loginName": "admin", "password": "123456"})["data"].(map[string]interface{})["sessionId"].(string)

	// ① 热更新流程：w2(欢迎) → m2(9键结束) → b2
	newFlow := map[string]interface{}{
		"name": "热更流程",
		"flow": map[string]interface{}{
			"entry": "w2",
			"nodes": []map[string]interface{}{
				{"id": "w2", "type": "play", "text": "热更欢迎语", "next": "m2"},
				{"id": "m2", "type": "menu", "text": "9 结束", "branches": map[string]string{"9": "b2"}},
				{"id": "b2", "type": "end", "text": "再见"},
			},
		},
	}
	pr := req2("PUT", "/api/ivr/flow", adm, newFlow)
	if pr["success"] != true {
		t.Fatalf("流程热更失败: %v", pr)
	}

	// ② 真实 SIP 呼入 → 2s 后按 9 → 应走新流程
	ua2, _ := sipgo.NewUA()
	dg2 := diago.NewDiago(ua2, diago.WithTransport(diago.Transport{Transport: "udp", BindHost: "127.0.0.1", BindPort: 25083}))
	invOpts := diago.InviteOptions{Transport: "udp"}
	invOpts.Headers = append(invOpts.Headers, &sip.FromHeader{
		DisplayName: "热更测试", Address: sip.Uri{User: "13800000666", Host: "127.0.0.1"}, Params: sip.NewParams(),
	})
	cctx, ccancel := context.WithCancel(context.Background())
	defer ccancel()
	c, err := dg2.Invite(cctx, sip.Uri{User: "13800000666", Host: "127.0.0.1", Port: 25082}, invOpts)
	if err != nil {
		t.Fatalf("呼入失败: %v", err)
	}
	time.Sleep(2500 * time.Millisecond) // w2 播完自动到 m2
	if w, err := c.AudioWriterDTMF(); err == nil {
		_ = w.WriteDTMF('9')
	}
	time.Sleep(2 * time.Second)
	_ = c.Close()

	// ③ 断言：新流程路径落库
	var path string
	poll(t, 5*time.Second, func() bool {
		return db.QueryRow(`SELECT path_json FROM ivr_call_log WHERE caller_no='13800000666' ORDER BY id DESC LIMIT 1`).Scan(&path) == nil
	})
	if !bytes.Contains([]byte(path), []byte("w2")) || !bytes.Contains([]byte(path), []byte("m2")) || !bytes.Contains([]byte(path), []byte("b2")) {
		t.Fatalf("应走热更后新流程 w2→m2→b2，实际 %s", path)
	}
	t.Logf("flow 热更 SIP 验证 ✓（新流程路径 %s）", path)
}
