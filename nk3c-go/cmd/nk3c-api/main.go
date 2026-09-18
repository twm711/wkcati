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

	"github.com/gin-gonic/gin"

	"nk3c/internal/app"
	"nk3c/internal/ivr"
	"nk3c/internal/media"
	"nk3c/internal/workorder"
	"nk3c/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "API 监听地址")
	dsn := flag.String("dsn", "file:nk3c.db?_journal=WAL&_busy_timeout=5000", "数据库 DSN")
	force := flag.Bool("reset", false, "启动时重建数据库（演示种子）")
	sipAddr := flag.String("sip-addr", "0.0.0.0:5060", "话务域 SIP/UDP 监听（空=禁用真实话务域）")
	flag.Parse()

	db, err := store.Open("sqlite", *dsn)
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
