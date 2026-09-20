// Package app 应用装配：路由/中间件/报表辅助/一键重置（演示）
package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"nk3c/internal/agent"
	"nk3c/internal/auth"
	"nk3c/internal/export"
	"nk3c/internal/ivr"
	"nk3c/internal/media"
	"nk3c/internal/monitor"
	"nk3c/internal/project"
	"nk3c/internal/realtime"
	"nk3c/internal/store"
	"nk3c/internal/workorder"
	"nk3c/pkg/rinfo"
)

type App struct {
	DB       *store.DB
	Auth     *auth.Service
	Engine   *gin.Engine
	apiGroup *gin.RouterGroup
}

func Build(db *store.DB) *App {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.Use(gin.Logger(), gin.Recovery(), auditMiddleware(db))

	a := auth.New(db)
	p := project.New(db)
	ag := agent.New(db)
	mo := monitor.New(db)
	wk := workorder.New(db)
	iv := ivr.New(db, wk)

	e.GET("/api/health", func(c *gin.Context) {
		rinfo.GinOK(c, gin.H{"service": "nk3c-go", "driver": db.Driver}, "ok")
	})
	e.POST("/api/auth/login", a.Login)
	e.POST("/api/sys/reset", func(c *gin.Context) {
		u := a.UserOf(c.GetHeader("Authorization"))
		if u == nil || !a.HasRole(u, "domainAdmin") {
			c.JSON(200, rinfo.Fail(rinfo.CodePermission, "重置需要 domainAdmin 权限（演示账号 admin / 123456）"))
			return
		}
		if err := db.Migrate(true); err != nil {
			c.JSON(200, rinfo.Fail(rinfo.CodeInternal, err.Error()))
			return
		}
		a.InvalidateAll()
		c.JSON(200, rinfo.OK(true, "演示数据已重置：样本池/配额/话务恢复种子状态，所有会话已失效"))
	})

	api := e.Group("/api", a.RequireAuth())
	{
		api.POST("/auth/logout", a.Logout)

		prj := api.Group("/project")
		prj.GET("", p.List)
		prj.GET("/:pid", p.Detail)
		prj.POST("", a.RequireRoles("groupAdmin"), p.Create)
		prj.POST("/:pid/questions", a.RequireRoles("groupAdmin"), p.AddQuestion)
		prj.PUT("/:pid/quota", a.RequireRoles("groupAdmin"), p.SetQuota)
		prj.POST("/:pid/publish", a.RequireRoles("groupAdmin"), p.Publish)
		prj.POST("/:pid/revise", a.RequireRoles("groupAdmin"), p.Revise)
		prj.POST("/:pid/status", a.RequireRoles("groupAdmin"), p.Status)
		prj.POST("/:pid/samples", a.RequireRoles("groupAdmin"), p.ImportSamples)

		api.GET("/agent/dispatch", ag.Dispatch)
		api.POST("/agent/answer", ag.Answer)
		api.POST("/agent/result", ag.Result)

		api.GET("/monitor/wall", mo.Wall)
		api.GET("/monitor/calls", mo.Calls)

		api.GET("/sheet", sheetList(db))
		api.POST("/sheet/:sheetId/audit", ag.Audit)
		api.GET("/report/single", reportSingle(db))

		w := api.Group("/workorder")
		w.GET("", wk.List)
		w.GET("/:tid", wk.Detail)
		w.POST("/:tid/accept", wk.Accept)
		w.POST("/:tid/resolve", wk.Resolve)
		w.POST("/:tid/close", wk.Close)

		// 录音回放：按话务 ID 取 WAV（呼入/外呼通用）
		api.GET("/recording/:callId", func(c *gin.Context) {
			callID := c.Param("callId")
			var rec string
			err := db.QueryRow(`SELECT record_file FROM cti_call_record WHERE id=?`, callID).Scan(&rec)
			if err != nil || rec == "" {
				err = db.QueryRow(`SELECT record_file FROM ivr_call_log WHERE id=?`, callID).Scan(&rec)
			}
			if err != nil || rec == "" {
				rinfo.GinFail(c, rinfo.CodeNotFound, "无录音（未接通或未开启录音）")
				return
			}
			if _, err := os.Stat(rec); err != nil {
				rinfo.GinFail(c, rinfo.CodeNotFound, "录音文件已清理")
				return
			}
			c.Header("Content-Disposition", `inline; filename="recording-`+callID+`.wav"`)
			c.File(rec)
		})

		ivg := api.Group("/ivr")
		ivg.GET("/flow", iv.GetFlow)
		ivg.PUT("/flow", a.RequireRoles("groupAdmin"), iv.PutFlow)
		ivg.POST("/call", iv.StartCall)
		ivg.POST("/call/:sid/input", iv.Input)
		ivg.POST("/call/:sid/hangup", iv.Hangup)
		ivg.GET("/logs", iv.Logs)
	}

	app := &App{DB: db, Auth: a, Engine: e, apiGroup: api}
	app.apiGroup = api

	// 质检事件流：坐席动作实时广播督导 + cti_monitor_event 落库（M3）
	qcHub := realtime.NewEventHub()
	ag.SetNotifier(qcHub)
	mo.WireQC(qcHub, a)

	// 监控墙 WebSocket（?token= 鉴权；2s 推送墙面快照）
	hub := realtime.NewHub(mo.BuildWall)
	e.GET("/api/ws/monitor", a.AuthQuery(), func(c *gin.Context) {
		hub.ServeWS(c.Writer, c.Request)
	})
	// 质检 WS（仅 groupAdmin；鉴权后角色校验 403 不升级）
	e.GET("/api/qc/ws", a.AuthQuery(), mo.ServeQCWS)
	// 强签坐席（仅 groupAdmin）
	api.POST("/qc/force-checkout", mo.ForceCheckout)
	// 导出中心（?token= 鉴权；window.open 场景）
	e.GET("/api/export/:pid/:format", a.AuthQuery(), func(c *gin.Context) {
		pid, err := strconv.ParseInt(c.Param("pid"), 10, 64)
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeParam, "项目 ID 非法")
			return
		}
		mx, err := export.BuildMatrix(db, pid)
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
			return
		}
		format := strings.ToLower(c.Param("format"))
		if format == "columns" {
			rinfo.GinOK(c, gin.H{"headers": mx.Headers, "types": mx.Types}, "导出列清单")
			return
		}
		if raw := c.Query("cols"); raw != "" {
			indexes := []int{}
			for _, part := range strings.Split(raw, ",") {
				i, e := strconv.Atoi(strings.TrimSpace(part))
				if e == nil {
					indexes = append(indexes, i)
				}
			}
			if len(indexes) == 0 {
				rinfo.GinFail(c, rinfo.CodeParam, "cols 无有效列")
				return
			}
			mx = mx.SelectColumns(indexes)
		}
		if i := strings.LastIndex(format, "."); i >= 0 { // 兼容 /sheets.csv 与 /csv 两种写法
			format = format[i+1:]
		}
		var body []byte
		var mime, ext string
		switch format {
		case "csv":
			body, err = mx.CSV()
			mime, ext = "text/csv; charset=utf-8", "csv"
		case "xlsx":
			body, err = mx.XLSX()
			mime, ext = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx"
		case "sav":
			body, err = mx.SAV()
			mime, ext = "application/x-spss-sav", "sav"
		default:
			rinfo.GinFail(c, rinfo.CodeParam, "格式仅支持 csv/xlsx/sav")
			return
		}
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"nk3c-project-%d.%s\"", pid, ext))
		c.Data(200, mime, body)
	})
	return app
}

// RegisterDial 挂载外呼腿路由（话务域启动后由 main 调用）
//   - POST /api/agent/calls/:callId/dial    自动调研外呼
//   - POST /api/agent/calls/:callId/bridge  坐席桥接（body: {agentUri:"host:port"}；阻塞至任一端挂断）
func (a *App) RegisterDial(o *media.OutboundCaller, bd media.BridgeDriver) {
	a.apiGroup.POST("/agent/calls/:callId/dial", func(c *gin.Context) {
		callID, err := strconv.ParseInt(c.Param("callId"), 10, 64)
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeParam, "callId 非法")
			return
		}
		data, msg, err := o.Dial(c.Request.Context(), callID)
		if err != nil {
			var be *agent.BizErr
			if errors.As(err, &be) {
				rinfo.GinFail(c, be.Code, be.Msg)
				return
			}
			rinfo.GinFail(c, rinfo.CodeState, err.Error())
			return
		}
		rinfo.GinOK(c, data, msg)
	})
	a.apiGroup.POST("/agent/calls/:callId/bridge", func(c *gin.Context) {
		callID, err := strconv.ParseInt(c.Param("callId"), 10, 64)
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeParam, "callId 非法")
			return
		}
		var body struct {
			AgentURI string `json:"agentUri" binding:"required"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			rinfo.GinFail(c, rinfo.CodeParam, "agentUri 必填（host:port）")
			return
		}
		host, portS, err := net.SplitHostPort(body.AgentURI)
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeParam, "agentUri 格式应为 host:port")
			return
		}
		port, _ := strconv.Atoi(portS)
		// 阻塞至通话结束（真人通话时长；超 25s 视为演示超时）
		ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
		defer cancel()
		if err := o.Bridge(ctx, callID, bd, host, port); err != nil {
			rinfo.GinFail(c, rinfo.CodeState, err.Error())
			return
		}
		var sampleID int64
		_ = a.DB.QueryRow(`SELECT sample_id FROM cti_call_record WHERE id=?`, callID).Scan(&sampleID)
		rinfo.GinOK(c, gin.H{"callId": callID, "sampleId": sampleID, "bridgeState": "RELEASED"},
			"通话结束（桥已释放）；请提交话务结果码（POST /api/agent/result）")
	})
}

func sheetList(db *store.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		q := `SELECT id,call_id,project_id,sample_id,agent_id,qnr_id,qnr_version,status,audit_remark FROM ans_sheet`
		args := []interface{}{}
		if st := c.Query("status"); st != "" {
			q += ` WHERE status=?`
			args = append(args, st)
		}
		q += ` ORDER BY id DESC`
		rows, err := db.Query(q, args...)
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var id, callID, pid, sid, aid, qnr int64
			var ver, status string
			var remark sql.NullString
			_ = rows.Scan(&id, &callID, &pid, &sid, &aid, &qnr, &ver, &status, &remark)
			out = append(out, map[string]interface{}{"id": id, "call_id": callID, "project_id": pid,
				"sample_id": sid, "agent_id": aid, "qnr_id": qnr, "qnr_version": ver,
				"status": status, "audit_remark": remark.String})
		}
		rinfo.GinOK(c, gin.H{"total": len(out), "rows": out}, "ok")
	}
}

func reportSingle(db *store.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		qidS := c.Query("questionId")
		qid, perr := strconv.ParseInt(qidS, 10, 64)
		if perr != nil || qid <= 0 {
			rinfo.GinFail(c, rinfo.CodeParam, "questionId 必填（整数）")
			return
		}
		var title, qt string
		var qnrID int64
		if err := db.QueryRow(`SELECT title,q_type,qnr_id FROM qnr_question WHERE id=?`, qid).Scan(&title, &qt, &qnrID); err != nil {
			rinfo.GinFail(c, rinfo.CodeNotFound, "题目不存在")
			return
		}
		counts := map[int64]int{}
		rows, _ := db.Query(`SELECT a.option_ids, COUNT(*) FROM ans_answer a
			JOIN ans_sheet s ON s.id=a.sheet_id WHERE a.question_id=? AND s.status IN ('SUBMITTED','AUDITED') GROUP BY a.option_ids`, qid)
		for rows != nil && rows.Next() {
			var opts string
			var n int
			_ = rows.Scan(&opts, &n)
			var ids []int64
			_ = json.Unmarshal([]byte(opts), &ids)
			for _, id := range ids {
				counts[id] += n
			}
		}
		if rows != nil {
			rows.Close()
		}
		items := []map[string]interface{}{}
		total := 0
		orows, _ := db.Query(`SELECT id,opt_text FROM qnr_option WHERE question_id=? ORDER BY opt_no`, qid)
		for orows != nil && orows.Next() {
			var oid int64
			var txt string
			_ = orows.Scan(&oid, &txt)
			n := counts[oid]
			total += n
			items = append(items, map[string]interface{}{"optionId": oid, "label": txt, "count": n})
		}
		if orows != nil {
			orows.Close()
		}
		for _, it := range items {
			if total > 0 {
				it["pct"] = float64(int(it["count"].(int))) * 100.0 / float64(total)
			} else {
				it["pct"] = 0.0
			}
		}
		quota := []map[string]interface{}{}
		qrows, err := db.Query(`SELECT c.id,c.target_count,c.done_count FROM qnr_quota_cell c
			JOIN qnr_quota q ON q.id=c.quota_id WHERE q.qnr_id=?`, qnrID)
		if err == nil {
			for qrows.Next() {
				var cid, tgt, done int64
				_ = qrows.Scan(&cid, &tgt, &done)
				pct := 0.0
				if tgt > 0 {
					pct = float64(done) * 100.0 / float64(tgt)
				}
				quota = append(quota, map[string]interface{}{"cellId": cid, "target": tgt, "done": done, "pct": pct})
			}
			qrows.Close()
		}
		rinfo.GinOK(c, gin.H{"questionId": qid, "title": title, "total": total, "items": items, "quota": quota}, "ok")
	}
}
