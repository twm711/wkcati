// SIP E2E：diago 双端同进程回环 —— 真实 SIP 信令 + RTP DTMF 驱动 IVR 核心引擎
// 验证：呼入→应答→提示音→按 0 →转人工→ABANDONED 之外的正常落库（ivr_call_log + 工单 PENDING）
package media

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	sip "github.com/emiago/sipgo/sip"

	"nk3c/internal/ivr"
	"nk3c/internal/store"
	"nk3c/internal/workorder"
)

func TestSIPIvrTransferE2E(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelWarn)
	dir := t.TempDir()
	db, err := store.Open("sqlite", filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}
	driver := ivr.New(db, workorder.New(db))
	srv := &SIPServer{Driver: driver, BindHost: "127.0.0.1", BindPort: 25060, DtmfWait: 4 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Start(ctx) }()
	time.Sleep(500 * time.Millisecond) // 等 SIP 就绪

	// 客户端（模拟话机 13977778888）
	ua2, _ := sipgo.NewUA()
	dg2 := diago.NewDiago(ua2, diago.WithTransport(diago.Transport{Transport: "udp", BindHost: "127.0.0.1", BindPort: 25061}))
	defer ua2.Close()

	invOpts := diago.InviteOptions{Transport: "udp"}
	invOpts.Headers = append(invOpts.Headers, &sip.FromHeader{
		DisplayName: "测试话机",
		Address:     sip.Uri{User: "13977778888", Host: "127.0.0.1"},
		Params:      sip.NewParams(),
	})
	var client *diago.DialogClientSession
	for i := 0; i < 3; i++ { // 重试容错
		c, err := dg2.Invite(ctx, sipURIAbs("13977778888", "127.0.0.1", 25060), invOpts)
		if err == nil {
			client = c
			break
		}
		if i == 2 {
			t.Fatalf("SIP 呼叫失败: %v", err)
		}
		time.Sleep(800 * time.Millisecond)
	}
	t.Log("SIP 已应答（200/ACK），等待提示音…")
	time.Sleep(2 * time.Second) // 欢迎音自动流转 + 菜单提示音 + 进入 DTMF 监听

	// 按 0 → 转人工
	w, err := client.AudioWriterDTMF()
	if err != nil {
		t.Fatalf("客户端 DTMF 通道失败: %v", err)
	}
	if err := w.WriteDTMF('0'); err != nil {
		t.Fatalf("发送 DTMF 失败: %v", err)
	}
	t.Log("已发送 DTMF '0'（转人工）")
	time.Sleep(2500 * time.Millisecond) // 转接音 + 结束音 + BYE + 落库
	_ = client.Close()

	// 断言 ①：转人工自动落工单
	var tickets int
	poll(t, 8*time.Second, func() bool {
		_ = db.QueryRow(`SELECT COUNT(*) FROM wko_ticket WHERE caller_no='13977778888'`).Scan(&tickets)
		return tickets >= 1
	})
	if tickets < 1 {
		t.Fatal("转人工应自动落工单（wko_ticket PENDING）")
	}
	// 断言 ②：呼入话务日志 + 结局
	var outcome string
	poll(t, 5*time.Second, func() bool {
		return db.QueryRow(`SELECT outcome FROM ivr_call_log WHERE caller_no='13977778888' ORDER BY id DESC LIMIT 1`).Scan(&outcome) == nil
	})
	if outcome != "TRANSFER:MANUAL" {
		t.Fatalf("结局应为 TRANSFER:MANUAL，得到 %s", outcome)
	}
	// 断言 ③：工单可经业务 API 查询（与网页链路同库同走线）
	var status string
	if err := db.QueryRow(`SELECT status FROM wko_ticket WHERE caller_no='13977778888' LIMIT 1`).Scan(&status); err != nil || status != "PENDING" {
		t.Fatalf("工单状态应 PENDING: %v %s", err, status)
	}
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

func sipURIAbs(user, host string, port int) sip.Uri { return sip.Uri{User: user, Host: host, Port: port} }
