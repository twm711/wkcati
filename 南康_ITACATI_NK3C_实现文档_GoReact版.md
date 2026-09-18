# ITACATI / NK3C 实现文档 · Go + React 版

> **版本**：v1.0（2026-09-18）　**定位**：以 Go 后端 + React/Ant Design 前端 + SQLite(开发)/MySQL(生产) + [diago](https://emiago.github.io/diago/)（纯 Go SIP/媒体库）重新实现 NK3C 云联络中心与 ITACATI 电访调研核心。
> **基线沿用**：本文档不推翻既有调研与规格，而是"换引擎"——
> - 接口契约沿用《ITACATI_NK3C_API接口规格.yaml》（47 端点 / ResultInfo 封装 / 18 位 ID 字符串 / 全小写路径 / Bearer）；
> - 数据模型沿用《ITACATI_NK3C_数据库建表.sql》（60 表三方言）派生迁移脚本；
> - 测试基线沿用《ITACATI_NK3C_测试用例集.xlsx》（96 用例 / 41 条 P0）与 P0 覆盖矩阵；
> - 行为基准沿用 `nk3c-demo`（Python）已验证的 31 条闭环断言（派样过滤/配额原子/幂等/审核状态机/结果码去向/回访闭环/SPSS 导出），逐条移植为 Go 集成测试。

---

## 1. 技术栈总览

### 1.1 选型清单

| 层 | 选型 | 说明 |
| --- | --- | --- |
| 后端语言 | **Go 1.22+** | 单二进制部署、goroutine 天然适配高并发话务状态机 |
| Web 框架 | **gin** + go-playground/validator | 路由/参数校验；中间件生态成熟 |
| 数据访问 | **sqlx** + **goose** 迁移 | 轻量 SQL 控制（派样/配额等关键 SQL 手写，便于双方言管理）；避免 ORM 黑盒 |
| 数据库 | **SQLite**（开发/单机演示，WAL 模式）/ **MySQL 8.0+**（生产，主从可选） | 同一套迁移脚本，方言差异见 §5.4 |
| SIP/媒体 | **[diago](https://emiago.github.io/diago/)**（基于 [sipgo](https://github.com/emiago/sipgo)） | 纯 Go：Dialog 全控、alaw/ulaw/opus、WAV 播放/录音、RTP DTMF、B2BUA 桥接、Refer 转接、混音会议、SRTP、IPv6 |
| 实时通道 | **coder/websocket**（或 gorilla/websocket） | 监控墙/坐席条推送（对应原 nagentstateserver 职责） |
| 鉴权 | 自研 session + RBAC 中间件 | 权限码沿用 domainAdmin/orgAdmin/groupAdmin/phoneAdmin【S7】；会话存内存（可插 Redis） |
| 前端 | **React 18 + TypeScript + Vite + Ant Design 5** | 管理端组件全覆盖；react-query + zustand；@ant-design/charts；react-flow（IVR 画布） |
| 前后端联调 | Vite dev proxy → Go :8080；生产 Go embed 前端 dist | 单二进制交付 |
| 坐席话机 | SIP 软电话（MicroSIP/Zoiper/gophone CLI）**或** diagox WebRTC 网关（实验） | 替代原 NKZXAgent（.NET）【S7】；零配置 URL:9080/was 思路保留为"点击签入自动唤起/注册" |
| ID 生成 | 自研 Snowflake（18 位内 int64 → 字符串） | 对应原规范"ID 18 位 Long→String"【S6】 |

### 1.2 新旧映射表（关键决策溯源）

| 原（调研/蓝图） | 新（本文档） | 说明 |
| --- | --- | --- |
| Java Spring(nweb 五层) + Tomcat【S6/S8】 | Go 单体多模块（可拆 call-server） | 无 JVM 调优；内存从"每程序独立 Tomcat 堆 250M/370M"【S8】降为进程内 goroutine |
| MyBatis + Druid【S9】 | sqlx + goose | 关键 SQL（派样 FOR UPDATE SKIP LOCKED / 配额原子 UPDATE）保留手写 |
| CTI Proxy（平台无关层）【S2】 | `internal/media`：diago 适配层 | 保持"换 SIP 栈不动业务"：业务只依赖 `CallController` 接口 |
| nagentstateserver | `internal/realtime` WS Hub | 事件协议沿用 nk3c-demo 已验证的 wall/qc/qclog/reset |
| NKZXAgent 软电话（.NET4.0/reg.bat）【S7】 | 标准 SIP 账号 + 任意软电话；后期 diagox WebRTC | 去掉客户端注册组件 |
| velocity/jQuery/nkui 前端【S6】 | React 18 + Antd 5（IE10 要求废止） | 管理端原型四张 HTML 的交互全部平移 |
| ITACATI 经典版 Win2003 硬件表【S1】 | Linux 容器部署（容量模型沿用《部署手册》§5） | 100 席容量/中继/录音存储算法不变 |
| nk3c-demo（Python，行为基准） | Go 集成测试集（31 条逐条移植） | demo 保留为"可运行参考实现" |

---

## 2. 总体架构

### 2.1 架构图

```
┌────────────────────────────────────────────────────────────────────────┐
│ 客户端层                                                                 │
│  浏览器(React SPA)：坐席台 / 督导台 / 项目台 / IVR设计器 / 报表            │
│  坐席音频端：SIP 软电话(MicroSIP/Zoiper/gophone)  [实验: diagox WebRTC]    │
│  被叫：PSTN 手机/固话                                                     │
└──────┬───────────────────────────┬─────────────────────────────────────┘
       │ HTTPS(REST) / WSS(事件)     │ SIP 5060 / RTP 10000-20000
┌──────▼───────────────────────────▼─────────────────────────────────────┐
│ NK3C Go 服务（单二进制 · 两域 · 可拆分）                                    │
│                                                                          │
│  ┌───────────────────────────┐   ┌────────────────────────────────────┐ │
│  │ API 域 (cmd/nk3c-api)      │   │ 话务域 (cmd/nk3c-call)              │ │
│  │  gin REST（47 端点）        │   │  diago/sipgo：SIP UA + RTP 会话     │ │
│  │  Session+RBAC 中间件        │   │  InboundServer（呼入→IVR 引擎）      │ │
│  │  15 个业务模块 handler       │   │  DialerLoop（预览/自动/预测外呼）    │ │
│  │  realtime WS Hub            │◄──┤  Bridge 桥接（坐席↔客户）/混音(插话) │ │
│  │  （wall/qc/call 事件推送）   │   │  Recorder（双轨 WAV + 地标）         │ │
│  └──────────────┬────────────┘   └───────────────┬────────────────────┘ │
│                 │      internal/bus（进程内事件总线，可换 NATS）          │
│  ┌──────────────▼─────────────────────────────────▼──────────────────┐ │
│  │ 业务模块 internal/{auth,org,project,questionnaire,sample,strategy, │ │
│  │   agent,monitor,answer,report,export,qc,workorder,ivr,sys}         │ │
│  │   · 派样引擎（半年原则/黑名单/重拨上限/派样锁）                        │ │
│  │   · 配额原子扣减 · 结果码去向 · 审核状态机 · 统计 · 导出(CSV/XLSX/SPSS)│ │
│  └──────────────────────────────┬────────────────────────────────────┘ │
│  ┌──────────────────────────────▼────────────────────────────────────┐ │
│  │ 数据访问 internal/store：sqlx + goose 迁移                          │ │
│  │   dev: SQLite(WAL)   prod: MySQL 8.0                               │ │
│  └───────────────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────────────┘
```

**拆分原则**：M0–M4 单进程两域（API 域 + 话务域在同一二进制，goroutine 隔离）；100 席以上或需多机横向扩展时，话务域独立部署为 `nk3c-call`，API↔Call 经事件总线（NATS/Redis Stream）通信。对应原蓝图"独立样本池/消息中心独立部署"【S2】的思想。

### 2.2 进程内事件总线（解耦两域）

```go
// internal/bus/bus.go —— 业务域与话务域的唯一耦合点
type Event struct {
    Type string          // call.dispatched / call.connected / call.finished / agent.state / qc.command / ticket.created ...
    Data json.RawMessage
    Ts   time.Time
}
type Bus interface {
    Publish(ctx context.Context, e Event)
    Subscribe(topics ...string) <-chan Event
}
```

- 话务域发事件（呼入/挂断/DTMF/录音就绪），API 域消费落库并驱动 WS Hub；
- 业务域发指令（强制挂断/监听/插话/外呼任务），话务域消费执行——对应 S5 质检六指令与 M06 策略。

---

## 3. 仓库结构

```
nk3c-go/
├── cmd/
│   ├── nk3c-api/main.go          # API 域入口（gin + WS Hub + embed 前端）
│   ├── nk3c-call/main.go         # 话务域入口（diago SIP/RTP；可与 api 同进程跑 all-in-one）
│   └── nk3c-migrate/main.go      # goose 迁移执行器
├── internal/
│   ├── auth/                     # 登录/会话/RBAC（权限码四件套）
│   ├── org/                      # 域/组织/组/员工（四级结构、工号段）
│   ├── project/                  # 项目 CRUD/状态机/发布/修订/样本导入
│   ├── questionnaire/            # 问卷/题型/选项/配额/版本化/校验(可达性 BFS)
│   ├── sample/                   # 样本/号码/黑名单/状态码/批次导入
│   ├── strategy/                 # 外呼策略（预览/自动/预测/混合、时段、重拨规则）
│   ├── agent/                    # 坐席签入签出/状态机/派样接口
│   ├── monitor/                  # 监控墙聚合/话务流水/质检指令下发
│   ├── answer/                   # 话务作答/答卷/逐题 upsert/审核
│   ├── report/                   # 单题频数/项目报表/配额进度
│   ├── export/                   # CSV/XLSX/SPSS(.sav) 导出中心
│   ├── qc/                       # 抽检任务/评分表/缺陷否决
│   ├── workorder/                # 工单落单/流转/回访闭环
│   ├── ivr/                      # IVR 流程引擎(flow_json)/留言/来电记录
│   ├── sys/                      # 参数/日志/健康/一键重置(演示)
│   ├── media/                    # ★ diago 适配层：CallController 接口 + 实现
│   │   ├── controller.go         #   业务侧接口（CallController，见 §6.1）
│   │   ├── diago_impl.go         #   diago 实现（Dialog 会话/桥接/播放/DTMF/录音）
│   │   ├── dialer.go             #   预测外呼循环（ratio 算法）
│   │   └── recorder.go           #   双轨录音 + aud_start/aud_end 地标
│   ├── realtime/                 # WS Hub（monitor/agent 双通道，沿用 demo 协议）
│   ├── store/                    # sqlx 封装/方言差异/事务助手
│   └── bus/                      # 进程内事件总线
├── migrations/                   # goose：00001_init.sql ...（SQLite/MySQL 双目录或标签）
├── web/                          # React 前端（Vite）
│   ├── src/pages/{login,agent,supervisor,projects,ivr-designer,reports,workorders}
│   ├── src/components/{SoftPhoneBar,QuestionnaireRenderer,MonitorWall,QuotaBoard,CallFlowCanvas,RecordPlayer}
│   ├── src/api/                  # openapi-typescript 生成的 client + axios 拦截器
│   └── src/stores/               # zustand（session/agentState）
├── config.yaml                   # 配置（见 §9）
├── Makefile                      # dev / migrate / build / test / e2e
└── Dockerfile / docker-compose.yml
```

---

## 4. 接口层设计（沿用 OpenAPI 规格）

### 4.1 统一响应与错误码（与规格逐字一致）

```go
// pkg/resultinfo/resultinfo.go
type ResultInfo struct {
    Success bool        `json:"success"`
    Code    string      `json:"code"`    // "0" 成功；其余见错误码表
    Message string      `json:"message"`
    Data    interface{} `json:"data"`
}
func OK(data any, msg string) ResultInfo       { ... }
func Fail(code, msg string) ResultInfo         { ... }   // 4001参数/4010未登录/4031状态/4032权限/4041不存在/4091冲突/4092重复/5000内部
```

- 路由全小写、Bearer sessionId 中间件、`pageNum/pageSize → {total, rows}`；
- **ID 一律 int64 Snowflake，JSON 序列化为字符串**（`type ID int64` + 自定义 MarshalJSON）——兼容原"18 位 Long→String"【S6】与 JS 精度。

### 4.2 端点 → Go handler 映射（47 端点按 15 tag 归口）

| tag（模块包） | 关键端点（与 OpenAPI 规格同名） |
| --- | --- |
| auth | POST /api/auth/login、POST /api/auth/logout、GET /api/auth/me |
| org | GET/POST /api/org/domain|org|group|user、PUT user/{id}/status（停用→强制签出） |
| project | GET/POST /api/project、GET /api/project/{pid}、POST .../questions、PUT .../quota、POST .../publish、POST .../revise、POST .../status、POST .../samples |
| questionnaire | GET /api/questionnaire/{id}（结构）、POST .../validate（BFS 可达性+死题检测）、GET .../versions |
| sample | GET /api/sample（分页/筛选）、POST /api/sample/import（查重）、GET /api/sample/blacklist |
| strategy | GET/PUT /api/strategy/{projectId}（模式/时段/重拨/呼损阈值） |
| agent | GET /api/agent/dispatch?projectId=、POST /api/agent/answer、POST /api/agent/result、POST /api/agent/aux、POST /api/agent/checkout |
| monitor | GET /api/monitor/wall、WS /ws/monitor、WS /ws/agent、GET /api/monitor/calls、POST /api/monitor/qc |
| answer | GET /api/sheet、GET /api/sheet/{id}/answers、POST /api/sheet/{id}/audit |
| report | GET /api/report/single?questionId=、GET /api/report/project/{pid} |
| export | GET /api/export/answers?format=csv|xlsx、GET /api/export/stats、GET /api/export/spss、GET /api/qc/recording/{callId}（WAV） |
| qc | POST /api/qc/task（抽检）、POST /api/qc/score |
| workorder | GET /api/workorder、GET /api/workorder/{id}、POST .../accept|resolve|close |
| ivr | GET/PUT /api/ivr/flow、POST /api/ivr/call（模拟）/真实呼入由话务域、POST .../input、POST .../hangup、GET /api/ivr/logs |
| sys | GET /api/health、GET /api/sys/param、POST /api/sys/reset（演示） |

> 鉴权矩阵沿用规格：坐席接口 phoneAdmin、审核/导出/质检/工单归档 groupAdmin+、域管理 domainAdmin、重置 domainAdmin。

---

## 5. 数据层设计

### 5.1 迁移策略

- `migrations/` 用 **goose**，从既有《数据库建表.sql》60 表派生；M1 只迁移 33 张核心表（nk3c-demo 同名同字段超集），M3 补 CTI 扩展列（`ans_answer.aud_start/aud_end`、`cti_call_record.record_file`、`wko_ticket.revisit_sample_id` 等）；
> **✅ M0+M1 已落地（2026-09-18，nk3c-go/）**：`go build ./...`、`go vet`、`go test ./internal/itests/ -race` 全绿（9 条链路集成测试，断言对齐 nk3c-demo 行为基线：登录鉴权/派样四过滤/作答 upsert/结果码去向+幂等+配额/审核状态机/项目生命周期+修订版本化/导入查重/工单回访闭环 E2E/IVR 走线+监控墙）。落地差异：SQLite 驱动采用 **mattn/go-sqlite3（CGO）**（modernc.org/sqlite 为 CGO-free 备选，但其 sqlite/lib 单包编译需 ≥4GB 内存，2GB 约束环境编译 OOM）；迁移暂用内嵌 SQL + schema_migrations 表（goose 语义子集），M3 换 goose CLI；单写者串行化以 `SetMaxOpenConns(1)` 实现，errAbort 哨兵语义=「业务分支已应答，事务提交」。
- 每个迁移文件含 `-- +goose StatementBegin` 包裹的**双方言段**（goose 支持按方言标签拆分，或用 `migrations/sqlite/` 与 `migrations/mysql/` 两目录，构建标签选择）。

### 5.2 核心表（M1 范围，33 张）

沿用原 DDL 的同名表：sys_user / sys_param / sys_op_log / org_domain / org_org / org_group / prj_project / qnr_questionnaire / qnr_question / qnr_option / qnr_quota / qnr_quota_cell / smp_sample / smp_phone / smp_blacklist / smp_status_code / smp_import_batch / strat_outbound / cti_agent_state / cti_agent_state_log / cti_call_record / cti_monitor_event / ans_sheet / ans_answer / rpt_export_log / qc_task / qc_score / wko_ticket / wko_template / ivr_flow / ivr_call_log / msg_notice / seq_snowflake。

### 5.3 ID 与时间

- 主键 `BIGINT`，Snowflake（nodeId 取自配置；单机 0）；API 输出转字符串；
- 时间统一 `DATETIME(3)`（MySQL）/ `TEXT` ISO（SQLite），UTC 存储，前端本地化。

### 5.4 SQLite ↔ MySQL 方言差异清单（store 层集中处理）

| 差异点 | SQLite（dev） | MySQL 8.0（prod） | store 层策略 |
| --- | --- | --- | --- |
| 驱动 DSN | `file:nk3c.db?_journal=WAL&busy_timeout=5000` | `go-sql-driver/mysql` + 连接池(_maxOpen=2×坐席组) | Config.Driver 切换 |
| 派样锁 | `BEGIN IMMEDIATE` + `UPDATE ... WHERE status='IDLE'` 条件更新防竞争 | `SELECT ... FOR UPDATE SKIP LOCKED LIMIT n` | `store.SampleDAO.DispatchLock()` 方言分支 |
| 逐题 upsert | `INSERT ... ON CONFLICT(sheet_id,question_id) DO UPDATE` | `INSERT ... ON DUPLICATE KEY UPDATE` | `store.AnswerDAO.Upsert()` |
| 布尔/枚举 | INTEGER/TEXT | TINYINT/VARCHAR | DAO 统一 Go 枚举 |
| 自增/主键 | INTEGER PRIMARY KEY（演示种子可用固定小 ID） | BIGINT + Snowflake 应用层生成 | 种子数据仅 dev 使用小 ID |
| 并发写 | 单写者：`store` 内单写 goroutine 串行化事务 | InnoDB 行锁 | 接口不变 |

> 关键算法 SQL 与蓝图/MySQL 附录一致：派样过滤（IDLE+非黑名单+attempts<max+半年原则 ORDER BY shuffle_key）、配额原子 `UPDATE qnr_quota_cell SET done=done+1 WHERE id=? AND done<target`（0 行=满格，走 overflow 策略）。

---

## 6. 话务层设计（diago 实战映射）

### 6.1 CallController 接口（业务只依赖此接口，SIP 栈可替换）

```go
// internal/media/controller.go
type CallController interface {
    // 外呼：strategy 决定被叫与时机；返回 callID 由业务登记
    Invite(ctx context.Context, req InviteReq) (CallHandle, error)
    // 呼入回调：话务域注册（diago Serve）
    OnInbound(fn func(ctx context.Context, call InboundCall))
    // 媒体操作（坐席/客户任一会话）
    Play(call CallHandle, wav io.Reader) error          // PlaybackCreate().Play
    StopPlay(call CallHandle) error
    ReadDTMF(call CallHandle) <-chan DTMF               // RTP DTMF
    Record(call CallHandle, w io.Writer) error          // 双轨 WAV
    Bridge(agent CallHandle, customer CallHandle) error // B2BUA 桥接
    MixTo(agent CallHandle, mixer MixerHandle) error    // 插话=加入混音
    Transfer(call CallHandle, target string, attended bool) error // Refer 盲转/咨转
    Hangup(call CallHandle, cause string) error
}
```

> 实现基于 diago 已验证能力（官方文档核对）：`sipgo.NewUA()` → `diago.NewDiago(ua)` → `dg.Serve(ctx, func(inDialog *diago.DialogServerSession))`；会话 `Trying()/Progress()/Answer()/Bye()`；`PlaybackCreate().Play(file, "audio/wav")`；RTP DTMF 通道；B2BUA Bridging；Refer 盲转/咨转；Mixer 混音；WAV 立体声录音；alaw/ulaw/opus 编解码。**版本锁定**：go.mod 固定 diago/sipgo commit，升级走灰度分支。

### 6.2 概念映射表（CTI 事件 ↔ diago 调用）

| 蓝图概念（4.2 状态机） | 话务域实现 | 落库/推送 |
| --- | --- | --- |
| 外呼起呼 | `Invite(req)`（DialogClientSession）→ `Progress/Ringing` 事件 | cti_call_record(status=DIALING) → WS wall |
| 客户应答 | `Answer/WAIT_ANSWER` 成功 | connect_time、INCALL、WS wall |
| 坐席接听 | 坐席 SIP 注册在线 → `Bridge(agent, customer)` | agent.state=TALKING |
| 逐题作答 | 坐席端 Web 提交（与音频并行） | ans_answer upsert + aud_start/aud_end |
| 播报/问卷读题 | `Play(wav)`（TTS 离线合成 wav 库） | ivr_call_log.path_json |
| 按键收号 | `ReadDTMF` 超时重播（3 次→挂断） | IVR 答案按 tag 归集 |
| 转人工 | `Transfer(refer→队列URI)`；无空闲→排队+播报 | wko_ticket 落单（呼入转工单） |
| 插话/监听 | 监听=订阅录音流；插话=`MixTo` 三方混音 | cti_monitor_event(LISTEN/INSERT) |
| 强挂 | `Hangup(cause=FORCE)` | 结果码按 FORCE 映射 |
| 通话结束 | `Bye/对话框销毁` | end_time、录音文件、结果码待提交、WS wall |
| 录音 | 双轨 WAV（8k/16k ulaw→PCM 封装）；文件名 `{callID}.wav` | cti_call_record.record_file；回放沿用"答案地标同步高亮" |

### 6.3 外呼策略（M06 四模式）

| 模式 | 实现 |
| --- | --- |
| 预览 PREVIEW | 坐席台弹样本卡 → 坐席点呼叫 → `Invite`；无并发调节 |
| 自动 AUTO | DialerLoop：空闲坐席队列 → 派样 → `Invite` → 接通桥接给该坐席 |
| 预测 PREDICTIVE | DialerLoop + 算法（沿用 4.5.1/autotest 参数）：`OCC=ATT/(ATT+HT)`、`N=ceil(坐席×ratio×CR)`；呼损>上限→`ratio×0.8 且 N×0.8`；连续≥3 周期达标且 occ<0.9→`ratio×1.1`（cap 3.0）；ticker 100ms，ratio 用 atomic.Value |
| 混合 HYBRID | 预测池 + 预览队列按权重出队 |
| 通用规则 | 时段窗外停拨（strategy.time_window）；一人多号（smp_phone sort_no）；重拨规则（attempts<max、间隔、按 RESULT_CODES 映射去 REDIAL_POOL/APPOINT_QUEUE/BANNED/CLOSED_x） |

### 6.4 呼入 IVR 引擎（flow_json 解释器）

- 节点协议与 nk3c-demo 完全一致：`play / menu / question / voicemail / transfer / end`（branches/options/tag/next）；
- 呼入：`OnInbound` → `Answer` → 解释器循环：`Play(wav)` → `ReadDTMF`(超时 5s 重播×3) → 菜单分流 / 记答案 / 留言（Record 截断存档）/ 转人工（Refer 入队列，排队播报）；
- 校验沿用 nk3c-demo `_ivr_validate`（entry 存在/id 唯一/跳转完备）+ 增强：**BFS 可达性检测死题**（P0 行 23）；
- 模拟通道保留：`POST /api/ivr/call|input|hangup`（无音频直接走解释器），供自动化测试与前端设计器调试——同一引擎两条入口（真实 SIP/模拟按键）。

---

## 7. 实时通道（realtime）

- 端点：`WS /ws/monitor`（督导）、`WS /ws/agent`（坐席条）；token 查询参数鉴权（音频/WS 控件无法带 Header），坏 token 4401；
- 协议（沿用 nk3c-demo 已验证格式）：`{"type":"wall|qc|qclog|reset|call","data":…,"ts":…}`；
- Hub：`map[*conn]{uid, channel}`；`BroadcastChannel(monitor, …)` / `SendToUID(uid, …)`（质检定向）；业务变更点（login/dispatch/result/audit/qc/ticket/reset）发总线 → Hub fan-out；
- 断线：客户端 3s 重连 + 降级 3s 轮询 `GET /api/monitor/wall`（保留 demo 兜底策略）。

---

## 8. 前端设计（React + Ant Design 5）

### 8.1 工程与基建

- Vite + TS 严格模式；`openapi-typescript` 从规格生成类型与 client；axios 拦截器解 ResultInfo（code!==0 → message.error + 4010 跳登录）；
- 状态：`zustand`（session/agentState 轻状态）+ `react-query`（服务端数据缓存/失效）；
- 实时：`useLiveChannel(channel)` hook（WS + 降级轮询）；
- 布局：Antd ProLayout；菜单按权限码过滤（phoneAdmin 仅见坐席工作台/本人答卷——对应 P0 行 3 菜单显隐）。

### 8.2 页面清单（对应四张原型 + demo 四控制台的交互平移）

| 路由 | 页面 | 关键 Antd 构件 / 交互 |
| --- | --- | --- |
| /login | 登录 | Form + 权限码提示 |
| /agent | 坐席工作台 | 软电话条（签入/示忙/挂断/话后）；样本卡（多号切换）；**QuestionnaireRenderer** 按题型渲染（单选 Radio/数值 InputNumber min-max/文本 TextArea/多选 Checkbox…对应 20 题型渐进覆盖）；结果码 Popconfirm 提交后展示去向/配额/幂等；`/ws/agent` 收质检横幅（notification） |
| /supervisor | 督导控制台 | 监控墙（卡片墙 + Statistic 汇总 + 状态 Tag 色系）；话务流水 Table；答卷审核（Table + 录音回放抽屉：**WaveSurfer 波形 + 答案同步高亮**，P0 行 58）；质检六按钮（监听/插话/示忙/示闲/强挂/强签）；配额进度 Progress；项目总览；工单流转；导出中心（CSV/XLSX/**SPSS .sav** 下载） |
| /projects | 项目管理台 | 卡片列表 + **Steps 生命周期**（DRAFT→RUNNING→PAUSED/FINISHED）；加题表单/配额编辑器/发布确认/修订（版本 v1.x+1 提示快照策略）/样本导入（上传 xlsx + 查重报告） |
| /ivr-designer | IVR 流程设计器 | **react-flow** 节点画布（六类节点着色）+ 属性面板 + 保存校验 + 内嵌话机模拟器（DTMF 按键走线调试） |
| /reports | 统计报表 | @ant-design/charts 频数/配额；导出按钮组 |
| /workorders | 工单中心 | Table + 状态机按钮（受理/办结/归档）+ 回访链路关联展示 |

### 8.3 组件设计要点

- `SoftPhoneBar`：签入后注册状态轮询（SIP 账号由后端 `GET /api/agent/me` 下发：`sip:1020@host:5060`），点击"呼叫"仅作业务触发（真正起呼由话务域 `Invite`）；
- `QuestionnaireRenderer`：数据源=派样响应中的问卷（与规格一致）；逐题提交乐观更新；断点续答高亮已答题；
- `RecordPlayer`：`GET /api/qc/recording/{callId}` 流式播放（token 查询参数）；地标 `cues` 来自 `GET /api/sheet/{id}/answers`；
- 路由守卫：roles 注入，无权 403 页。

---

## 9. 配置与部署

### 9.1 config.yaml

```yaml
server: { addr: ":8080", wsPathPrefix: "/ws" }
db:
  driver: sqlite            # sqlite | mysql
  dsn: "file:data/nk3c.db?_journal=WAL&busy_timeout=5000"
  # dsn: "user:pass@tcp(127.0.0.1:3306)/nk3c?parseTime=true&loc=Local"
auth: { sessionTTL: 12h, redis: "" }          # redis 非空则启用外部会话
sip:
  publicIP: "203.0.113.10"
  listenUDP: ":5060"
  rtpStart: 10000
  rtpEnd: 20000
  codecs: [pcmu, pcma, opus]
  trunk: { host: "sip-trunk.operator.com", user: "8012", pass: "***" }  # 运营商中继
media: { recordDir: "data/record", ttsDir: "data/tts" }
dialer: { tickMs: 100, abandonMaxPct: 3.0, defaultRatio: 1.2 }
log: { level: info, loki: "" }
```

### 9.2 构建/运行

```bash
# 开发
make dev            # 终端1: go run ./cmd/nk3c-api -config config.yaml（含话务域）
                    # 终端2: cd web && pnpm i && pnpm dev（Vite 代理 /api,/ws → :8080）
make migrate-dev    # goose up（SQLite）
make test           # go test ./... -race（集成测试用临时 SQLite）

# 生产
make build          # web: pnpm build → dist；Go embed → 单二进制 nk3c
docker compose up -d   # nk3c + mysql:8（volume: data/, record/）；env: DB_DRIVER=mysql
```

**Docker Compose 服务**：`nk3c`（镜像多阶段：node:20 build web → golang:1.22 build embed）、`mysql:8.0`（utf8mb4、innodb 默认参数）、可选 `redis`（会话/限流）、可选中继网关。生产 MySQL 连接池 `SetMaxOpenConns(2×坐席组数)`、慢查询日志接入。

---

## 10. 测试与验收

### 10.1 三层测试（沿用既有体系）

| 层 | 内容 | 工具 |
| --- | --- | --- |
| 单元 | 配额算法/ratio 算法/IVR 校验/版本号 bump/ResultInfo | testify 表驱动 |
| 集成 | **nk3c-demo 31 条断言逐条移植**（登录→派样101→作答→结果码→配额→审核→回访闭环→SPSS 回读），httptest + 临时 SQLite | go test -race |
| 话务 E2E | 真实 SIP：**gophone**（diago 生态 CLI 软电话，支持自动化脚本）模拟被叫 + DTMF 走 IVR；MicroSIP 手工验坐席音频；录音文件抽查 + 地标对齐 | gophone / sipp（压力） |
| 前端 | vitest 组件冒烟 + Playwright（◻ 原型层 4 条 P0 的 UI 自动化落点） | Playwright |

### 10.2 P0 对齐（41 条沿用《P0覆盖矩阵》）

- ✅ 已自动化 20 条 → Go 移植清单直接映射（附录 B）；
- ◻ 原型层 4 条 → Playwright；
- ▢ 10 条随 M5/M6 功能落地补齐（管理端 CRUD/质检抽检/对账任务等）。

---

## 11. 里程碑计划

| 里程碑 | 内容 | 交付/验收 | 估算 |
| --- | --- | --- | --- |
| **M0 骨架**（3 天） | 仓库/CI、gin+ResultInfo+RBAC 中间件、goose 双方言迁移、React 骨架（ProLayout+登录+openapi client）、WS Hub | Swagger 47 路由占位全通鉴权冒烟；登录/菜单权限 2 条 P0 | 3 人日 |
> **✅ 前端骨架已落地（2026-09-18，nk3c-go/web/）**：React 18 + TS strict + Antd 5 + Vite 5 + React Router 6（Hash）。与规划的差异：以 antd Layout 手搭替代 ProLayout（减依赖/避 React 版本地雷）、HashRouter 适配任意反代、axios 拦截器实现 ResultInfo 信封直通（S13 教训）。五页：登录/坐席工作台（派样→动态问卷→逐题落库→结果码去向）/监控墙（5s 轮询+督导审核）/项目管理（全生命周期+修订+配额+单题统计）/工单（回访链路）/IVR 话机模拟器。**注意：2GB 内存环境 `vite build` 会 OOM（3100 模块），生产构建需 ≥4GB；沙箱验证口径 = tsc --noEmit 全绿 + dev 代理链 E2E（登录→派样→作答→结果码→监控/工单/IVR 全 200）。**
| **M1 业务闭环**（6 天） | project/questionnaire/sample/agent/answer/report 模块；派样引擎+配额原子+结果码去向+审核状态机；**31 条 Go 集成测试全绿** | `go test ./... -race` 绿；与 nk3c-demo 行为逐条对齐 | 6 人日 |
| **M2 管理与导出**（5 天） | 项目台/问卷设计器简化版（表单级）/答卷审核/录音合成回放（先无 SIP：复用 demo 合成音轨思路，接口留 Record 真录音）/导出 CSV/XLSX/SPSS | P0 行 11/18~24/69 相关条目绿；React 四控制台可演示 | 5 人日 |
| **M3 SIP 话务**（8 天） | diago 接入：呼入应答/外呼 Invite/桥接/DTMF/双轨录音；坐席 SIP 注册与桥接；质检指令落 cti_monitor_event | gophone 脚本化 E2E：外呼→桥接→挂断→录音落盘；MicroSIP 人工通话验收 | 8 人日 |
| **M4 呼入 IVR + 工单**（5 天） | flow_json 引擎接真实音频（Play/DTMF/留言 Record/Refer 转队列）；转人工落工单 + 回访闭环；IVR 设计器（react-flow） | gophone DTMF 走通种子流程；工单回访 E2E（test_30 对应） | 5 人日 |
> **◐ M3/M4 呼入部分提前落地（2026-09-18，nk3c-go/internal/media/）**：diago **v0.32.2** + sipgo v1.6（需 **Go ≥1.23 工具链**，go.mod 已=1.23.0）；opus 为可选 build tag `with_opus_c`（默认纯 Go 回退，无 CGO）。单进程两域兑现 M0–M4 拆分原则：`cmd/nk3c-api --sip-addr 0.0.0.0:5060` 同进程起 API+话务域。接口隔离：`media.IvrDriver`（StartSIP/KeySIP/HangupSIP）驱动抽取出的 **ivr 核心引擎**（internal/ivr/core.go）——网页模拟按键与真实话机 DTMF 同一走线；提示音运行时合成（8kHz/16bit WAV 多频音，无音频资产依赖）；DTMF 走 RTP telephone-event。**SIP E2E 全绿**（internal/media/sip_e2e_test.go：diago 双端同进程回环：INVITE/200/ACK→提示音→RTP DTMF「0」→TRANSFER:MANUAL→工单 PENDING）。待增量：外呼 Invite/桥接（M3 主体）、gophone 真机演练、录音落盘（M4）、flow 热更 SIP 验证。
| **M5 预测外呼 + 监控强化**（6 天） | DialerLoop 四模式/时段/呼损降速 ratio 算法；监控墙 WS 实时（事件总线）；抽检 qc_task | sipp 压测 64 路并发呼损率达标；ratio 算法单测（autotest 参数复刻） | 6 人日 |
| **M6 生产化**（4 天） | MySQL 切换验证（SKIP LOCKED 派样压测）、Docker Compose、备份/监控（metrics+日志）、上线检查单（沿用《部署手册》§7） | 部署手册 Go 版增补完成；预生产 5 席灰度 | 4 人日 |

> 合计约 **37 人日**（单人 2 个月；双人并行约 5 周）。每里程碑结束跑 P0 矩阵同步更新。

---

## 12. 风险与对策

| # | 风险 | 对策 |
| --- | --- | --- |
| R1 | diago 录音能力：核心库为"简单 WAV 立体声录音"，高级录音在其私有模块 | M3 自实现：挂接 AudioReader 流 → 双轨 PCM → WAV 封装；不依赖私有模块；接口 CallController 隔离 |
| R2 | WebRTC 坐席（浏览器直讲）为实验性（diagox） | M3–M5 坐席音频用标准 SIP 软电话（MicroSIP/gophone）；WebRTC 作为 M6+ 试点 |
| R3 | SQLite 并发写（单写者）限制 | WAL + busy_timeout + store 层单写事务队列；并发压测在 MySQL 上做（SKIP LOCKED 需 8.0+） |
| R4 | 运营商中继对接（NAT/编解码/号码格式） | sipgo/diago 支持 symmetric RTP/SRTP；先 Lab 用 gophone 互测，再接真实中继；保留 CTI Proxy 接口以便回退 Asterisk 桥接方案 |
| R5 | 预测外呼实时性 | DialerLoop 100ms ticker + atomic ratio + 事件驱动重算；呼损统计滑动窗口 |
| R6 | TTS 质量/成本 | 离线批量合成 WAV（按问卷/IVR 文案预生成）；运行时只 Play 文件 |
| R7 | OpenAPI 规格与 Go 实现漂移 | oapi-codegen/gin 绑定生成 server 接口，编译期对齐；CI 跑规格校验 |
| R8 | 96 用例中"万级导入/对账任务"等大功能 | 排期 M6+；导入先实现 5000 行/批 + 查重报告 |

---

## 附录 A：事件协议（WS）

```jsonc
// 服务端 → 客户端
{"type":"wall",  "data":{"agents":[{...}],"summary":{...}}, "ts":"..."}   // 监控墙快照
{"type":"qc",    "data":{"action":"LISTEN","note":"…","by":"督导"}, "ts":"…"} // 定向坐席
{"type":"qclog", "data":{"agentNo":"1020","action":"INSERT"}, "ts":"…"}     // 仅督导通道
{"type":"call",  "data":{"callId":"…","state":"TALKING","recordFile":"…"}, "ts":"…"}
{"type":"reset", "data":{"message":"…"}, "ts":"…"}
```

## 附录 B：nk3c-demo 31 条断言 → Go 测试映射（抽样）

| demo 测试 | Go 集成测试（internal/…/ _test.go） |
| --- | --- |
| test_02 首派101全流程 | `TestAgentDispatch_FullFlow`（answer 模块） |
| test_03 结果码幂等 | `TestSubmitResult_Idempotent` |
| test_04 过滤排空(103/104/105/102) | `TestDispatch_FilterRules`（sample 模块） |
| test_05/DB 配额并发 | `TestQuota_ConcurrentAtomic`（-race，MySQL 下 SKIP LOCKED 分支） |
| test_06/07 审核/驳回重访 | `TestAudit_StateMachine` |
| test_17/18 质检推送/强签 | `TestQC_WS_Push`、`TestQC_ForceCheckout`（realtime 模块） |
| test_19 录音地标/WAV | `TestRecording_Landmarks`（media 模块） |
| test_21/22 多项目/生命周期 | `TestProject_Lifecycle`（project 模块） |
| test_27 版本化/28 状态机/29 查重 | `TestQuestionnaire_Versioning` 等 |
| test_30 回访闭环 E2E | `TestWorkorder_Revisit_E2E`（workorder 模块） |
| test_31 SPSS 回读对账 | `TestExport_SPSS_Roundtrip`（export 模块） |

## 附录 C：与既有交付物关系

| 既有交付物 | 在 Go 版中的角色 |
| --- | --- |
| OpenAPI 规格（47 端点） | 接口契约（oapi-codegen 绑定） |
| 三方言 DDL | goose 迁移母本 |
| 测试用例集（96 用例/41 P0/覆盖矩阵） | 验收基线（编号沿用） |
| nk3c-demo（Python） | 行为基准 + 差距对照（README《与生产差距》逐条被本计划吸收） |
| 部署手册 | §9 生产化输入（容量模型/检查单沿用，技术栈章节按本文档增补 Go 版） |
| 主开发文档（17 章） | 业务需求源（模块划分一一对应 M02–M15） |
