# NK3C Go 后端骨架（M0 可运行 + M1 业务闭环）

技术栈：Go 1.22 + gin + database/sql（SQLite 开发 / MySQL 生产，驱动可切换）。
与 `nk3c-demo`（Python 行为基准）接口语义逐条对齐，`internal/itests` 为移植断言。

## 运行

```bash
go build ./...
go run ./cmd/nk3c-api --addr :8080 --sip-addr 0.0.0.0:5060 --reset
# API: http://localhost:8080/api/health
# 话务域: SIP/UDP :5060 呼入 → IVR（1 调研 / 2 留言 / 0 转人工自动落工单）
#（--sip-addr "" 禁用话务域；diago 需 Go ≥1.23 工具链）
```

账号：admin（domainAdmin）/ sup01（groupAdmin）/ agent01（坐席 1020），密码均 123456。

## 测试

```bash
go test ./internal/itests/ -v   # 9 条业务链路集成测试
go test ./internal/media/ -v    # SIP/RTP E2E ×5：呼入转人工 / 外呼自动调研 / 呼入录音 / B2BUA 坐席桥接 / flow 热更
bash ../autotest/sip_drill.sh   # gophone 真机演练 6 断言（需 gophone 二进制 + 双服务在跑）
```

## 结构

- `pkg/rinfo` ResultInfo 统一响应（恒 HTTP 200 + 业务码 0/4001/4010/4031/4032/4041/4091/4092/5000）
- `pkg/ids` 雪花 ID（JSON 序列化为字符串，规避前端 Long 精度丢失）
- `internal/store` 双方言数据层 + 嵌入式迁移（18 表 + 种子）
- `internal/auth|project|agent|monitor|workorder|ivr` 六大业务域
- `internal/media` 话务域（M2）：diago SIP 服务器（呼入 IVR + 外呼自动调研 + B2BUA 桥接）+ IvrDriver/AgentDriver 接口 + 合成提示音 + 录音（audio_tap 单消费者：tap 直通层叠 RTP 事件检测）
- `internal/app` 装配与路由
- SQLite 驱动：mattn/go-sqlite3（CGO）；如需 CGO-free 可换 modernc.org/sqlite（编译需 ≥4GB 内存）

> 话务域 M2 收官：呼入 IVR、外呼自动调研、B2BUA 坐席桥接（`POST /api/agent/calls/:id/bridge`，body `{agentUri}`）、录音落盘/回放、flow 热更均已落地（diago v0.32.2，Go ≥1.23）。外呼需 `--outbound host:port` 指向被叫/中继。

## 导出中心与实时推送（M3 第一增量）

- `GET /api/export/:pid/:format`（format=csv|xlsx|sav，亦接受 `/sheets.csv` 段式）：CSV 带 UTF-8 BOM；XLSX 双工作表（答卷明细+结果码分布）；**SAV**（SPSS 系统文件：数值题落数值变量、其余宽 32 字符串、record7 subtype13 长名 + subtype20 UTF-8 声明）。鉴权双通道：`?token=` 或 Authorization 头；`Content-Disposition: attachment; nk3c-project-<pid>.<ext>`。
- `GET /api/ws/monitor?token=`：WebSocket 监控墙推送，帧 `{"type":"wall","data":<监控墙>,"ts":ms}`，周期 2s；web 端优先 WS，失败自动降级 5s 轮询（Monitor 页状态标签）。
- web：项目详情抽屉新增「导出 CSV/XLSX/SPSS」直链下载按钮（window.open + ?token=）。
- E2E：`internal/app/export_ws_test.go` ×4（CSV 可解析、XLSX 双表、SAV 逐记录解析（$FL2/变量记录/rec7/999 终止/数值 9.0）、WS 首帧 wall+无 token 401）。

## 质检事件流与强签（M3 第二增量）

- `GET /api/qc/ws?token=`（**仅 groupAdmin**，升级前 403）：督导订阅坐席动作事件，帧 `{"type":"qc","event":"DIAL|ANSWER|RESULT|FORCE_LOGOUT","data":{agentNo,userId,callId,sampleId,...},"ts":ms}`，事件驱动即推（无订阅者零开销）。
- `POST /api/qc/force-checkout` body `{"userId":n}`（仅 groupAdmin）：强签=注销目标全部会话 + 释放占用样本（ASSIGNED/INCALL→IDLE 回池）+ FORCE_LOGOUT 广播。
- 事件同步落 `cti_monitor_event`（迁移 002，迁移器已泛化为多版本顺序应用）。
- web：监控墙坐席卡「强签」按钮（督导可见，Popconfirm 二次确认）；新增质检事件实时表格，断线 5 秒重连并保留最近 50 条事件。
- E2E：`internal/app/qc_test.go` ×2（DIAL→ANSWER→RESULT 三帧+落库+403 守门；强签=会话吊销+样本回池+FORCE_LOGOUT 帧+非督导 403）。【对应 P0 test_17/18】

## 前端 web/（React 18 + Antd 5 + Vite 5）

```bash
cd web && pnpm install && pnpm dev   # :5173，/api 反代 :8080
```

五页骨架（登录/坐席工作台/监控墙+审核/项目管理/工单/IVR 模拟器）详见 `web/README.md`。
2GB 沙箱内 `vite build` 会 OOM（antd 3100 模块），验证口径 = `tsc --noEmit` + dev 代理链 E2E；生产构建需 ≥4GB 环境。
