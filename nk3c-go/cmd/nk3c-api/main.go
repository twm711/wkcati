// nk3c-api：Go 版 NK3C 演示/生产服务
// M0–M4 单进程两域：API 域（gin :8080）+ 话务域（diago SIP，--sip-addr，UDP）
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"nk3c/internal/agent"
	"nk3c/internal/app"
	"nk3c/internal/ivr"
	"nk3c/internal/media"
	"nk3c/internal/store"
	"nk3c/internal/workorder"
)

func main() {
	addr := flag.String("addr", ":8080", "API 监听地址")
	driver := flag.String("driver", "sqlite", "数据库驱动：sqlite 或 mysql")
	dsn := flag.String("dsn", "file:nk3c.db?_journal=WAL&_busy_timeout=5000", "数据库 DSN")
	force := flag.Bool("reset", false, "启动时重建数据库（仅 SQLite 演示种子）")
	sipAddr := flag.String("sip-addr", "0.0.0.0:5060", "话务域 SIP/UDP 监听（空=禁用真实话务域）")
	outbound := flag.String("outbound", "", "外呼路由 host:port（被叫模拟器/中继；空=外呼腿不可用）")
	flag.Parse()

	if *driver != "sqlite" && *driver != "mysql" {
		log.Fatalf("不支持的数据库驱动: %s", *driver)
	}
	db, err := store.Open(*driver, *dsn)
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Migrate(*force); err != nil {
		log.Fatal(err)
	}
	a := app.Build(db)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 话务域：真实 SIP 呼入 → IVR 核心引擎 → 工单/话务落库（与网页模拟同一走线）
	if *sipAddr != "" {
		host, portS, _ := net.SplitHostPort(*sipAddr)
		port, _ := strconv.Atoi(portS)
		srv := &media.SIPServer{
			Driver:   ivr.New(db, workorder.New(db)), // 独立业务服务实例（与 API 域同走线引擎，落同一库）
			BindHost: host,
			BindPort: port,
			OnFinish: func(st ivr.NodeState) {
				if strings.HasPrefix(st.Outcome, "TRANSFER") {
					log.Printf("[话务域] 转人工已自动落工单：session=%s caller=%s", st.SessionID, st.CallerNo)
				}
			},
		}
		srv.RecordDir = "recordings"
		if ph, pp, e := net.SplitHostPort(*outbound); e == nil && ph != "" {
			pn, _ := strconv.Atoi(pp)
			go func() { // OutboundCaller 由 Start 装配后补路由
				for srv.Outbound == nil {
					time.Sleep(100 * time.Millisecond)
				}
				srv.Outbound.PeerHost, srv.Outbound.PeerPort = ph, pn
				srv.Outbound.OnRecorded = func(callID int64, path string) {
					_, _ = db.Exec(`UPDATE cti_call_record SET record_file=? WHERE id=?`, path, callID)
				}
				a.RegisterDial(srv.Outbound, agent.New(db))
				log.Printf("外呼腿已挂载：路由 %s:%d（POST /api/agent/calls/:callId/dial）", ph, pn)
			}()
		}
		srv.OnFinish = func(st ivr.NodeState) { // 呼入录音路径回填话务日志
			if st.RecordFile != "" {
				_, _ = db.Exec(`UPDATE ivr_call_log SET record_file=? WHERE id=(SELECT MAX(id) FROM ivr_call_log WHERE caller_no=?)`,
					st.RecordFile, st.CallerNo)
			}
		}
		go func() {
			if err := srv.Start(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[话务域] SIP 服务器退出: %v", err)
			}
		}()
		log.Printf("话务域已启动 SIP/UDP %s（IVR 呼入：1 调研 / 2 留言 / 0 转人工落单）", *sipAddr)
	}

	log.Printf("NK3C Go 服务已启动 %s（driver=%s）", *addr, db.Driver)
	_ = gin.Mode()
	go func() { <-ctx.Done(); os.Exit(0) }()
	if err := a.Engine.Run(*addr); err != nil {
		log.Fatal(err)
	}
}
