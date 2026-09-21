# NK3C / ITACATI 项目真实进度与 CATI 能力审查

审查日期：2026-09-21（Asia/Taipei）
审查范围：README、web/README、Go 路由与业务实现、SQLite/MySQL 迁移、React 页面、Go 测试与 SIP/媒体实现。

## 1. 结论摘要

当前项目不是“生产级 CATI 平台”，而是一个**可运行的演示型/集成验证型 CATI 垂直切片**：

- M0/M1 样本派发、问卷作答、结果码、审核、工单、项目生命周期：基本可运行，SQLite 端有业务链路测试。
- M2 SIP/IVR、外呼自动调研、B2BUA 坐席桥接、录音、督导 LISTEN/BARGE/HANGUP：有真实 diago 接线和 E2E，但仍是单进程、单中继/UDP、演练级实现。
- M3 导出、监控墙、QC WS、强签、审计、坐席状态和督导消息：部分已有测试，最新状态控制 UI/消息重连已提交；能力矩阵和 README 本轮已修正 LISTEN/已接通控制项口径。
- MySQL 目前只是迁移代码与方言演练，**不能宣称生产可用**；新鲜 MySQL 初始化、ID 生成、种子数据、字段长度和真实接口集成仍是上线阻断项。
- 前端 TypeScript 仍未验证：当前 `web/node_modules/.bin/tsc` 不存在，因此不能宣称 `tsc --noEmit` 通过。

本轮验证：`go test ./...` 全部通过（缓存/本地 SQLite 与模拟/E2E 测试口径）；不是 MySQL 验证，也不是生产负载验证。

## 2. 文档与代码逐项对比

| 领域 | 文档声明 | 实际证据 | 真实评级 |
|---|---|---|---|
| 登录/RBAC | 三角色登录、角色权限 | `internal/auth`、RequireAuth/RequireRoles、Go 测试 | 原型可运行；密码明文、内存 session，生产未完成 |
| 项目生命周期 | 创建、发布、暂停、恢复、结项、修订 | `project.go` 与 chain_test | SQLite 演示闭环；ID 用 MAX+1，缺并发安全/审批/版本快照完整性 |
| 样本导入 | 查重、样本池 | `ImportSamples` 只按项目内手机号查重 | 部分完成；无批量文件导入、字段校验、全局黑名单导入、导入任务审计 |
| 半年规则 | 180 天过滤 | `Dispatch` 用 `last_connected_at < cutoff` | 已实现基础规则；规则仅按全局 sys_param，未做项目/客户/号码维度可配置和时区政策 |
| 黑名单 | 全局黑名单/结果码自动命中 | `smp_blacklist`、REFUSE/INVALID | 基础完成；无范围继承、导入审批、号码规范化、黑名单历史/解禁 |
| 重拨 | redial.max、NA/BUSY 等回池 | 结果码更新 attempts，派样过滤 | 基础完成；无预约时间、拨号策略、号码轮换/失败原因历史 |
| 预约 | APPOINT 预约队列 | `APPOINT_QUEUE` 字符串去向 | 未完成：没有预约表、时间窗、调度器、到期处理 |
| 预测/渐进/预览拨号 | 文档/种子参数提及 | 无拨号器队列、速率控制、呼损算法 | 未实现 |
| 坐席签入 | 坐席工作台/状态 | 登录即视为可操作，状态 READY/BUSY/PAUSE 已有 API/UI | 部分完成：没有真实 sign-in/out、设备绑定、心跳、技能/队列 |
| 状态机 | READY/BUSY/PAUSE、强制状态 | `cti_agent_state`、前端按钮、督导控制 | 部分完成；无事件历史、原因字典、状态时长、断线策略 |
| 派样锁 | BEGIN IMMEDIATE/并发安全描述 | SQLite 单连接串行 + UPDATE status 条件；MySQL 无 FOR UPDATE/SKIP LOCKED | SQLite 演示可用；MySQL 生产并发未完成 |
| 断线续答 | 逐题 upsert 可断点续答 | `ans_sheet`/`ans_answer` upsert | 部分完成：没有恢复接口/租约/坐席重连认领，状态可能长期 INCALL/DOING |
| SIP 呼入 | diago UDP SIP、IVR、DTMF | `internal/media/server.go` + sip_e2e | 真实演练完成；无 TLS/SRTP、注册、鉴权、NAT/多节点 |
| IVR | flow 热更、项目/主叫前缀路由、转人工 | `ivr` 核心 + route migration | 原型到演示完成；内存 sessions 无锁/无过期，转人工 call_id 取 MAX(id) 不可靠 |
| 外呼自动调研 | 自动拨号+DTMF答题+结果码 | `outbound.go` + E2E | 单通演示完成；无批量调度、并发、速率、重试、运营商中继策略 |
| B2BUA/桥接 | 客户腿+坐席腿+督导第三方腿 | `BridgeMix`、bridge/outbound E2E | 真实媒体切片完成；桥接 API 阻塞、25 秒演示超时，不是生产呼叫编排 |
| DTMF | RTP DTMF 读取 | audio tap / diago | 已验证基础路径；无 SIP INFO/多编码兼容和异常统计 |
| 录音 | 呼入/外呼 WAV 落盘与回放 | recordings、record_file、recording API | 演示完成；桥接录音仍明确交后续，文件存本地，无对象存储/加密/留存/分段 |
| LISTEN/BARGE/HANGUP | 督导控制 | `monitor/control.go`、媒体活动腿、E2E | 基础真实接线；Origin 全放行、无控制审计详情/多督导/权限数据域 |
| QC 事件 | DIAL/ANSWER/RESULT/FORCE_LOGOUT WS+落库 | EventHub、`cti_monitor_event`、qc_test | 部分完成：内存订阅、断线丢事件、无查询 API/事件幂等/持久化重放 |
| MESSAGE | 督导定向消息 | MessageHub、Agent WS、5 秒重连 | 部分完成：离线丢失、无落库/已读/送达、背压丢弃 |
| 问卷 | 单选/多选/数值/文本、必答、跳转、版本 | 实际渲染和 answerCore | 部分完成；后端未完整校验 required、option 类型/多选合法性、跳转未执行，结束可不答必答题 |
| 配额 | quota cell 原子扣减 | `tryConsumeQuota` | 基础完成；只覆盖条件 JSON 的 in，配额溢出处理不改变答卷/项目状态，复杂交叉配额不足 |
| 审核 | PASS/REJECT/VOID | `agent.Audit` | 基础完成；无审核队列/二审/质检规则/审计事件细粒度 |
| 工单回访 | 转人工落单→受理→办结→归档生成回访样本 | `workorder` + E2E | 演示闭环；回访样本固定项目1，缺通知/预约/关联呼叫策略 |
| 统计报表 | 监控墙、单题报告、CSV/XLSX/SAV | REST/WS/export、E2E | 基础完成；指标定义不完整，MySQL/大数据量性能未证 |
| 审计 | 写操作、导出、录音访问 | middleware + sys_op_log | 基础完成；写失败静默、无审批/留存/归档/租户域/完整前后值 |
| 多租户/组织/数据域 | 角色名包含 domain/org/group | 角色字符串存在 | 未实现：表无 tenant/org/group 关键域，接口多为全局查询，属于高危越权风险 |
| MySQL | 迁移目录、方言、Goose兼容 | mysql/*.sql、store.Migrate | 仅演练，生产未完成，见第4节阻断问题 |
| 可观测性/容灾 | 未形成完整声明 | slog/少量日志 | 未完成：无 metrics、trace、告警、健康依赖检查、备份恢复、HA |

## 3. CATI 行业能力审查

### 3.1 样本与触达策略

已有：样本状态 IDLE/ASSIGNED/INCALL/SUCCESS/CLOSED/BANNED、号码表、黑名单、半年过滤、重拨次数、结果码去向、shuffle_key 基础派样。

缺口：

1. 没有 appointment 表和时间窗调度，APPOINT 只是字符串去向。
2. 没有号码尝试历史；多号码样本虽然返回 phones，但实际派样/外呼主要使用首个有效号码。
3. 没有拨号失败原因、SIP cause、人工重拨规则、号码轮换和优先级策略。
4. 没有冷却期/时段策略/节假日/当地时区/夜间禁呼控制。
5. 没有真正的样本池租约、租约过期回收和死信处理。

### 3.2 预测/渐进/预览拨号

当前只有按 API 触发的单通 `Dial` 和单次桥接。没有：

- 预测拨号 pacing、坐席可用率、呼损率实时控制；
- 渐进/预览模式的任务队列和坐席确认；
- 并发上限、运营商限流、重试退避、号码池分配；
- 呼损上限计算。种子 `predict.abandon.max` 没有业务消费者。

该项属于 CATI 核心能力，评级为未实现，不能以“外呼 E2E”替代。

### 3.3 坐席、并发与断线

已有 READY/BUSY/PAUSE 和服务端派样前检查；SQLite `SetMaxOpenConns(1)` 使演示环境串行。问题是：

- 坐席不是通过签入建立设备/工作租约；
- 状态与真实 SIP leg 未绑定；
- `FORCE_LOGOUT` 释放 DB 样本，但没有可靠地挂断活动 SIP leg；
- 坐席断线后无 lease、resume token、恢复当前题目接口；
- MySQL 路径没有数据库级行锁/跳过已锁行策略；
- `MAX(id)+1` 在项目、问题、样本、工单、黑名单、配额等多处存在竞态。

### 3.4 SIP/CTI 媒体

真实实现证据：diago 0.32.2、SIP UDP 呼入、RTP DTMF、Playback、录音 tap、Outbound Invite、BridgeMix、第三方督导腿、HANGUP/LISTEN/BARGE；媒体 E2E 覆盖 5 个场景。

仍不能称为生产级 CTI：单进程内存活动呼叫表、单机本地 WAV、仅 UDP、无 TLS/SRTP、无 REGISTER/鉴权、无 NAT/媒体中继、无 SIP cause 映射和可靠崩溃恢复。桥接 API 的 25 秒 context timeout 也属于演示约束。

### 3.5 问卷/质检/统计

问卷动态渲染和逐题落库是真实功能，但 required、题型、选项合法性、跳转规则、必答完成条件尚未形成服务端强约束；前端不能代替后端问卷规则引擎。配额仅是简单 JSON 条件匹配，复杂交叉配额、配额锁定/回滚、配额版本化未完成。

## 4. 上线阻断项（P0）

### P0-1：MySQL 新鲜库缺业务种子与完整可执行验证

`mysql/004_seed.sql` 只插入用户和参数，SQLite 的项目、问卷、样本、状态码、IVR flow 等演示数据没有对应 MySQL seed。新鲜 MySQL 即使迁移成功也没有可运行项目和状态码。

### P0-2：MySQL 主键/ID 生成不闭环（已开始修复，未完成验证）

原 MySQL 表的多数 `id` 没有 AUTO_INCREMENT，代码却大量省略 id 并调用 `LastInsertId()`；例如派样插入 `cti_call_record`、答卷 `ans_sheet`、QC 事件 `cti_monitor_event`。这会导致新库插入失败或得到 0 ID。

本轮已将新鲜安装的 `001_init.sql` 主键改为 `AUTO_INCREMENT`，并新增 `008_production_integrity.sql` 尝试修复旧库和补齐状态码；但真实 MySQL 实例尚未执行，不能标记为完成。应用层 Snowflake 仍没有在这些写入路径接入。

### P0-3：MySQL 字段长度不适合真实数据

`flow_json`、`path_json`、`answers_json`、`detail`、`conditions_json` 等使用 `VARCHAR(255)`，真实问卷/IVR/审计/轨迹很容易超过 255；应使用 JSON/TEXT，并验证 MySQL strict mode。

### P0-4：多租户/数据域隔离（已开始修复，仍未完成）

本轮新增 SQLite 007 / MySQL 009：`sys_user.tenant_id`、`prj_project.tenant_id`，登录用户携带 TenantID，项目列表、详情、创建和项目变更接口已增加首层租户边界；domainAdmin 保留跨租户能力。进一步将项目租户校验传播到派样、项目导出、录音回放、工单 REST、监控墙 REST 和话务流水（非 domainAdmin 不再通过项目/话务 ID 访问其他租户）。

本轮又将监控墙 WS 改为按连接绑定 tenantID，墙面快照通过 `BuildWallFor(tenantID)` 生成，不再向普通租户推送全局坐席和话务统计；IVR flow、主叫路由、模拟呼入和 IVR 日志已增加项目租户校验，`ivr_call_log` 新增 project_id；新增跨租户集成测试，验证项目列表、导出和派样不能越租户。

本轮新增 `sys_org`、`sys_group`，并为用户/项目增加 `org_id`、`group_id`；项目列表、项目访问和派样开始按坐席组过滤；新增组织/坐席组管理、坐席归组、项目归组 API 和基础测试。进一步新增技能、坐席技能和项目技能要求模型，派样会拒绝不满足项目最低技能等级的坐席；新增队列、项目队列绑定、坐席队列归属和队列管理 API，项目绑定队列后派样会检查坐席是否已加入该队列；新增 `cti_sample_task` 任务租约记录，派样创建 LEASED 任务，结果码完成任务，强签将未完成任务置为 EXPIRED；新增 30 秒后台租约回收器，过期任务回池并将仍在 DIALING 的话务标记为 NA；新增坐席任务续租接口，并用条件更新防止多实例重复回收同一租约；新增续租、过期续租拒绝、租约回收、启动陈旧话务恢复和状态联动自动化测试；新增数据库 worker lock，对启动恢复和周期回收加 25 秒互斥租约，避免多实例重复执行；新增任务 retry_count/max_retries，租约多次失败后将样本置为 DEAD，避免无限重拨；新增任务尝试历史、死信任务查询和督导重新入池接口；新增 React 死信任务页面，支持查询、刷新和督导重新入池；新增死信任务尝试历史查询、失败码/失败说明字段，并记录租约超时和服务重启回收原因；结果码提交链路也会将真实业务结果（如 NA、BUSY、REFUSE、INVALID、BREAKOFF）写入尝试历史；diago 外呼 Invite 捕获最终 SIP 响应并映射 486/600→BUSY、404/484→INVALID、603/607→REFUSE、408/480/487→NA；同时保留 SIP Reason 头（包括 Q.850 cause）写入失败说明；新增 q850_cause/q850_text 结构化字段并在死信页面展示；新增样本 next_attempt_at 和按结果码配置的递增退避（BUSY 60 秒、NA 120 秒、REFUSE 600 秒、BREAKOFF/PARTIAL 300 秒），派样查询会跳过尚未到可重试时间的样本；通过 redial.backoff.enabled 控制启用，默认关闭以保持现有验收行为；同时修正新增样本和工单回访样本的显式列插入以兼容新增字段；死信恢复操作继续由统一操作审计记录。

同时修复迁移器缺陷：原实现会执行 Goose 文件的 Down 区段，导致新建的 `sys_org/sys_group` 等表立即被删除；现在统一只执行 Up 区段。当前仍属于基础模型，尚无技能组/队列调度和完整历史数据迁移。

仍未完成：历史 IVR 日志归属校验、全链路跨租户自动化测试、技能组数据域。当前仍不能上线多租户生产。

### P0-5：认证与敏感数据安全不足

密码明文；token 为进程内存会话且无过期/刷新/设备撤销；录音和导出使用 query token；WebSocket CheckOrigin 全放行；录音路径直接从数据库读取并 `c.File`；无速率限制、MFA、密钥管理、PII 脱敏和加密留存。

### P0-6：活动呼叫与样本状态不具备崩溃恢复

活动 leg、IVR session、MessageHub、EventHub 都在内存；进程重启后 DB 可能保留 ASSIGNED/INCALL/DOING，而媒体腿已不存在，造成样本泄漏和状态失真。

## 5. P1 后续计划

1. **MySQL生产化**：重做 ID 策略（Snowflake/UUID 或 AUTO_INCREMENT），补齐所有迁移、完整 seed/初始化分离、JSON/TEXT、FK/索引、strict mode；用真实 MySQL 执行迁移和 `go test`/接口集成。
2. **并发派样与租约**：样本领取表/lease、SELECT FOR UPDATE SKIP LOCKED、租约超时回收、唯一约束、事件历史；所有 MAX+1 改为安全 ID 服务。
3. **拨号调度器**：任务队列、preview/progressive/predictive 三模式、pacing、呼损率、并发和时段/节假日策略、SIP cause 与重拨计划。
4. **断线续答和签入**：agent session/device/heartbeat、SIP leg 关联、强签挂断、恢复接口、ASSIGNED/INCALL/DOING 修复作业。
5. **消息/QC持久化**：message 表、离线补发、送达/已读、序列号、幂等和权限审计；QC 事件持久化游标和重放。
6. **问卷引擎**：服务端 required/题型/选项/跳转/终止规则、问卷版本快照、复杂配额与审计。
7. **生产媒体**：SIP/TLS/SRTP、REGISTER/鉴权、NAT/媒体策略、录音对象存储、加密、留存、回放授权、桥接录音混音。
8. **安全与可观测性**：密码哈希、Redis/session TTL、Origin 白名单、限流、metrics/tracing/告警、审计留存、备份恢复演练。

## 6. P2 改进项

- 坐席技能/队列/组织层级、项目级号码绑定和 DID 管理；
- 多语言 TTS/提示音管理、DTMF/SIP INFO 兼容；
- 报表维度、导出异步任务、分页和大数据量索引；
- 录音质检标注、静音/打分/实时规则；
- 容灾演练、灰度迁移、配置中心、自动化压力与长稳测试；
- 前端状态从 `/api/agent/state` 初始化读取，避免刷新后 UI 默认 READY 与服务端状态不一致。

## 7. 文档修订建议

1. README 不应再使用“话务域 M2 收官”或“可生产”的语气，应改为“演示级真实媒体切片已完成”。
2. 已修正的 CTI 能力口径：LISTEN、MESSAGE、FORCE_BUSY、FORCE_READY 已有代码接线；当前能力矩阵 `LISTEN` 已修正为由 `s.cti != nil` 决定。
3. 明确所有 E2E 是 SQLite/本地 SIP 演练证据，不等价于真实 MySQL、运营商中继、并发和容灾验证。
4. 增加“未完成能力”章节，显式列出预约、预测/渐进/预览拨号、数据域隔离、MySQL生产化、断线续答、消息持久化和安全项。

## 8. 本轮动作与验证

- 修正 `internal/monitor/capabilities.go`：`LISTEN` 不再错误返回 false。
- 修正 README 对 `MESSAGE/FORCE_BUSY/FORCE_READY` 的过期描述。
- `go test ./...`：全部通过。
- 新增 `TestHalfOpenLineAllowsSingleProbe`，使用两个并发服务实例验证 OPEN 到期时仅一个请求能成功取得 HALF_OPEN 探测线路；SQLite 重复运行 5 次通过。
- 派样实际读取 `cti_agent_queue.capacity`，统计该坐席在目标队列的 `LEASED` 任务占用，达到容量时拒绝继续派样；
- 未传 `projectId` 时，派样会在当前租户/坐席组可用项目中按队列 priority、项目队列 priority、项目 ID 自动选择最高优先级项目；显式传入 `projectId` 仍保持兼容；
- 新增 `cti_waiting_task` 等待队列表；坐席队列容量已满时不再直接丢弃请求，而是幂等进入 WAITING；容量释放后后台每 30 秒自动为等待任务选择样本、创建话务和 LEASED 任务，并标记 ASSIGNED；新增等待任务查询和取消接口及 React 等待队列页面；`go test ./...`、`npx tsc --noEmit` 全部通过。
- 新增 `/api/monitor/line-health`：按租户汇总主叫线路总呼叫、接通、失败、失败率和最近话务时间；样本量至少 10 且失败率不低于 50% 时标记 `degraded`。
- 新增 `cti_outbound_line` 线路模型及 `/api/monitor/lines` 查询、`POST /api/monitor/lines` 配置接口，支持线路启停、优先级、容量和 active_calls 状态；媒体外呼会优先按租户、启用状态、优先级和剩余容量原子占用线路，呼叫结束释放 active_calls，无可用数据库线路时兼容启动参数路由；新增线路熔断字段，连续 5 次失败进入 OPEN、5 分钟后允许再次尝试，线路列表展示 circuitState/failureStreak/openedUntil；线路选择增加单探测竞争：OPEN 且熔断窗口到期时，只有成功将状态原子改为 HALF_OPEN 的请求可占用探测容量，其余请求重试选择；新增 `cti_line_circuit_event` 记录线路 CLOSED/OPEN/HALF_OPEN 状态变化、关联话务和结果码；新增 `/api/monitor/line-events` 按租户查询线路熔断状态时间线；线路事件使用数据库自增 ID，避免多实例 MAX(id)+1 冲突；新增 React 外呼线路页面，展示线路容量、熔断状态和事件时间线，并支持新增线路；新增 `cti_outbound_line_lease`，线路占用绑定 call_id 和 90 秒 lease_until，正常呼叫按 call_id 释放；媒体外呼每 30 秒续期线路租约，服务启动和 30 秒后台任务清理过期线路租约并修复 active_calls。
- 已临时安装前端依赖并执行 `npx tsc --noEmit`，检查通过；`node_modules` 为生成目录，不纳入提交。
