# NK3C Go 后端骨架（M0 可运行 + M1 业务闭环）

技术栈：Go 1.23 + gin + database/sql（SQLite 开发 / MySQL 生产，驱动可切换）。
与 `nk3c-demo`（Python 行为基准）接口语义逐条对齐，`internal/itests` 为移植断言。

## 运行

```bash
go build ./...
go run ./cmd/nk3c-api --driver sqlite --addr :8080 --sip-addr 0.0.0.0:5060 --reset
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

## 坐席督导消息（M3-18）

- 新增 `/api/agent/ws?token=` 定向 WebSocket 消息通道。
- 督导通过 `POST /api/monitor/control` 的 `MESSAGE` 动作向指定坐席推送消息。
- 坐席工作台显示督导消息提醒；消息不广播给其它坐席，断线后 5 秒自动重连。
- 能力矩阵中的 `MESSAGE` 已接通。

## 坐席状态控制（M3-17）

- 新增 `cti_agent_state` SQLite/MySQL 迁移；坐席可通过 `POST /api/agent/state` 设置 `READY/BUSY/PAUSE`。
- 督导可通过监控控制面执行 `FORCE_BUSY/FORCE_READY`；非 READY 坐席不能继续派样。
- 监控墙读取持久化坐席状态；控制能力矩阵同步标记强制状态切换为可用。

## CTI 督导控制面第一步（M3-12）

- 新增 `POST /api/monitor/control`，督导及以上可对活动 SIP 外呼/桥接发送 `HANGUP`，并通过 `BARGE + supervisorUri=host:port` 加入第三方督导 SIP leg；`TestBridgeAgentCallE2E` 已增加督导第三方 BARGE E2E。
- 话务域从两方 `Bridge` 改为可扩展的 `BridgeMix`，活动 call-leg 和督导 leg 会在通话结束时自动清理。
- `GET /api/monitor/control/capabilities` 返回真实能力矩阵；`LISTEN` 已实现为督导上行静音的三方监听，并可通过 BARGE 动态解除静音；`MESSAGE`、`FORCE_BUSY`、`FORCE_READY` 已接通。

## IVR 主叫号码多项目路由（M3-10）

- 新增 `ivr_route` 表与 `PUT /api/ivr/routes`（督导权限），按最长主叫号码前缀选择项目。
- IVR 流程增加 `project_id` 隔离；`GET/PUT /api/ivr/flow` 支持 `projectId`。
- 真实 SIP 呼入按主叫号码路由到项目流程；无匹配时兼容回退项目 1。

## IVR 多项目归属修正（M3 第九增量）

- IVR HTTP 模拟会话支持传入 `projectId`，并将项目归属保存到会话上下文。
- IVR 转人工生成工单时使用会话项目 ID，不再固定写入项目 1；缺省仍兼容演示项目 1。
- 真实 SIP 入口暂以号码路由映射项目，当前默认项目 1，号码绑定表仍待实现。

## 操作审计基础（M3 第七增量）

- `GET /api/audit/logs` 提供督导及以上审计查询，默认最近 100 条，最多 500 条。

- 新增 `sys_op_log` SQLite/MySQL 迁移；统一中间件记录业务写操作及导出/录音访问。
- 审计只保存用户、方法、路径、状态码和时间，不保存请求体，避免复制客户电话/答案等 PII。
- 审计写失败不覆盖原业务响应；生产仍需补审批、查询权限、留存和归档策略。

## 导出中心与 MySQL 方言演练（M3 第四增量）

- web 新增 `/exports` 导出中心：项目/格式选择、列选择、CSV/XLSX/SAV 下载，浏览器本地保留最近 20 条导出历史。
- `GET /api/export/:pid/columns` 返回可选列；导出支持 `?cols=0,1,...`。XLSX 结果码分布在裁剪列后仍正确。
- 新增 `migrations/mysql/001_init.sql`、`002_qc.sql`、`003_cti_extensions.sql`、`004_seed.sql`、`008_production_integrity.sql`（MySQL 8.0、InnoDB、utf8mb4）；001/008 已开始补齐 AUTO_INCREMENT、文本字段和结果码基础数据。`--driver mysql` 已按 driver 选择迁移目录，答题 upsert 对 SQLite 使用 `ON CONFLICT`，MySQL 使用 `ON DUPLICATE KEY UPDATE`。真实 MySQL 连接、迁移、接口集成和并发验证仍未完成。

## CTI 扩展列迁移（M3 第三增量）

- `003_cti_extensions.sql` 将 `cti_call_record.record_file`、`ivr_call_log.record_file`、`ans_answer.aud_start/aud_end` 从核心表迁移中拆出，支持旧 SQLite 库增量升级。
- 迁移文件采用 Goose 兼容的 `-- +goose Up/Down` 标记；内置迁移器按版本顺序执行，旧库已存在字段时仅忽略重复列错误。

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

## 坐席状态前端控制（M3-20）

- Agent 工作台新增 READY/BUSY/PAUSE 状态控制。
- 话务进行中禁止前端直接切换状态，服务端仍会拦截有进行中样本的强制示闲。
