// nk3c-api：Go 版 NK3C 演示/生产服务（M1 核心：业务闭环；话务域 diago 接入见 internal/media 预留）
package main

import (
	"flag"
	"log"

	"github.com/gin-gonic/gin"

	"nk3c/internal/app"
	"nk3c/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	dsn := flag.String("dsn", "file:nk3c.db?_journal=WAL&_busy_timeout=5000", "数据库 DSN")
	force := flag.Bool("reset", false, "启动时重建数据库（演示种子）")
	flag.Parse()

	db, err := store.Open("sqlite", *dsn)
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Migrate(*force); err != nil {
		log.Fatal(err)
	}
	a := app.Build(db)
	log.Printf("NK3C Go 服务已启动 %s（driver=%s）", *addr, db.Driver)
	_ = gin.Mode()
	if err := a.Engine.Run(*addr); err != nil {
		log.Fatal(err)
	}
}
