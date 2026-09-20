// 外呼腿 E2E：派样 → diago Invite 真实外呼 → 被叫 RTP DTMF 答题 → answerCore/resultCore 闭环 → 录音落盘
package media_test

import (
	"bytes"
	"context"
	"log/slog"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestOutboundAutoSurveyE2E(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	dir := t.TempDir()
	recDir := filepath.Join(dir, "recordings")
	db, err := store.Open("sqlite", filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}

	// 被叫模拟器（"客户" 13800000101）：应答后 2.0s 发 '2'（Q11→女/112）、3.5s 发 '8'（Q12→8 分）
	uaC, _ := sipgo.NewUA()
	dgC := diago.NewDiago(uaC, diago.WithTransport(diago.Transport{Transport: "udp", BindHost: "127.0.0.1", BindPort: 25073}))
	ctx, cancel := contextWithCancel()
	defer cancel()
	go func() {
		_ = dgC.Serve(ctx, func(in *diago.DialogServerSession) {
			_ = in.Trying()
			_ = in.Ringing()
			if err := in.Answer(); err != nil {
				return
			}
			go func() {
				w, err := in.AudioWriterDTMF()
				if err != nil {
					return
				}
				time.Sleep(2 * time.Second)
				_ = w.WriteDTMF('2')
				time.Sleep(1500 * time.Millisecond)
				_ = w.WriteDTMF('8')
			}()
			time.Sleep(9 * time.Second) // 等服务端走完问卷后 BYE（或兜底超时）
			_ = in.Hangup(ctx)
		})
	}()
	time.Sleep(400 * time.Millisecond)

	// 话务域（外呼腿）
	ivSvc := ivr.New(db, workorder.New(db))
	srv := &media.SIPServer{Driver: ivSvc, BindHost: "127.0.0.1", BindPort: 25072, DtmfWait: 4 * time.Second, RecordDir: recDir}
	go func() { _ = srv.Start(ctx) }()
	for srv.Outbound == nil {
		time.Sleep(100 * time.Millisecond)
	}
	srv.Outbound.Driver = agent.New(db)
	srv.Outbound.PeerHost, srv.Outbound.PeerPort = "127.0.0.1", 25073
	srv.Outbound.OnRecorded = func(callID int64, path string) {
		_, _ = db.Exec(`UPDATE cti_call_record SET record_file=? WHERE id=?`, path, callID)
	}

	// HTTP 栈（注册外呼路由）
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

	// ① 登录 + 派样（样本 101；派样为 GET 路由）
	lr := post("/api/auth/login", "", map[string]string{"loginName": "agent01", "password": "123456"})
	tok := lr["data"].(map[string]interface{})["sessionId"].(string)
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
	dd := get("/api/agent/dispatch?projectId=1", tok)["data"].(map[string]interface{})
	if dd == nil || int64(dd["sampleId"].(float64)) != 101 {
		t.Fatalf("派样应 101: %v", dd)
	}
	callID := int64(dd["callId"].(float64))

	// ② 真实外呼自动调研（同步返回 = 通话已完成）
	res := post(fmt.Sprintf("/api/agent/calls/%d/dial", callID), tok, nil)
	if res["success"] != true {
		t.Fatalf("外呼应成功: %v", res)
	}
	rd := res["data"].(map[string]interface{})
	if rd["destination"] != "CLOSED_SUCCESS" || rd["quotaConsume"] != "CONSUMED" {
		t.Fatalf("结果码去向/配额错误: %v", rd)
	}

	// ③ 断言：两题答案正确入库
	var opt string
	var num float64
	poll(t, 4*time.Second, func() bool {
		e1 := db.QueryRow(`SELECT option_ids FROM ans_answer WHERE question_id=11 AND numeric_value IS NULL`).Scan(&opt)
		e2 := db.QueryRow(`SELECT numeric_value FROM ans_answer WHERE question_id=12`).Scan(&num)
		return e1 == nil && e2 == nil
	})
	if opt != "[112]" || num != 8 {
		t.Fatalf("答案错误: Q11=%s（应 [112]）Q12=%v（应 8）", opt, num)
	}
	// ④ 断言：话务闭环 + 录音落盘
	var rc, rec string
	poll(t, 4*time.Second, func() bool {
		return db.QueryRow(`SELECT result_code,COALESCE(record_file,'') FROM cti_call_record WHERE id=?`, callID).Scan(&rc, &rec) == nil
	})
	if rc != "SUCCESS" {
		t.Fatalf("结果码应 SUCCESS: %s", rc)
	}
	if rec == "" {
		t.Fatalf("record_file 未回填")
	}
	st, err := os.Stat(rec)
	if err != nil || st.Size() <= 44 {
		t.Fatalf("录音文件异常: %v %v", rec, err)
	}
	// ⑤ 断言：HTTP 录音回放端点可用
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/recording/%d", ts.URL, callID), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != 200 || (ct != "audio/wav" && ct != "audio/vnd.wave" && ct != "audio/x-wav") {
		t.Fatalf("录音端点异常: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	t.Logf("外呼 E2E 全链路 ✓（样本 101 SUCCESS，录音 %d bytes）", st.Size())
}

func TestInboundRecordingPersisted(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelWarn)
	dir := t.TempDir()
	recDir := filepath.Join(dir, "recordings")
	db, err := store.Open("sqlite", filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}
	ivSvc := ivr.New(db, workorder.New(db))
	srv := &media.SIPServer{Driver: ivSvc, BindHost: "127.0.0.1", BindPort: 25074, DtmfWait: 3 * time.Second, RecordDir: recDir}
	srv.OnFinish = func(st ivr.NodeState) {
		if st.RecordFile != "" {
			_, _ = db.Exec(`UPDATE ivr_call_log SET record_file=? WHERE id=(SELECT MAX(id) FROM ivr_call_log WHERE caller_no=?)`,
				st.RecordFile, st.CallerNo)
		}
	}
	ctx, cancel := contextWithCancel()
	defer cancel()
	go func() { _ = srv.Start(ctx) }()
	time.Sleep(400 * time.Millisecond)

	ua2, _ := sipgo.NewUA()
	dg2 := diago.NewDiago(ua2, diago.WithTransport(diago.Transport{Transport: "udp", BindHost: "127.0.0.1", BindPort: 25075}))
	invOpts := diago.InviteOptions{Transport: "udp"}
	invOpts.Headers = append(invOpts.Headers, &sip.FromHeader{
		DisplayName: "测试话机", Address: sip.Uri{User: "13800000199", Host: "127.0.0.1"}, Params: sip.NewParams(),
	})
	c, err := dg2.Invite(ctx, sip.Uri{User: "13800000199", Host: "127.0.0.1", Port: 25074}, invOpts)
	if err != nil {
		t.Fatalf("呼入失败: %v", err)
	}
	time.Sleep(2 * time.Second)
	_ = c.Hangup(ctx)
	_ = c.Close()

	var rec string
	poll(t, 6*time.Second, func() bool {
		return db.QueryRow(`SELECT COALESCE(record_file,'') FROM ivr_call_log WHERE caller_no='13800000199' ORDER BY id DESC LIMIT 1`).Scan(&rec) == nil && rec != ""
	})
	st, err := os.Stat(rec)
	if err != nil || st.Size() <= 44 {
		t.Fatalf("呼入录音异常: %v %v", rec, err)
	}
	t.Logf("呼入录音落库 ✓（%d bytes）", st.Size())
}

func contextWithCancel() (ctx context.Context, cancel context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// poll 轮询直到 cond 为真或超时（异步落库容错）
func poll(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}
