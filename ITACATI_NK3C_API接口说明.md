# NK3C / ITACATI 接口说明（配套 OpenAPI 规格使用）

| 项 | 内容 |
| --- | --- |
| 规格文件 | `ITACATI_NK3C_API接口规格.yaml`（OpenAPI 3.0，已通过 openapi-spec-validator 校验） |
| 端点规模 | 47 个路径 · 15 个业务分组 · 12 个核心 Schema |
| 设计依据 | NK3C 公开工程规范【S6】【S7】+ 行业 REST 惯例 |
| 对应前端 | 四份交互原型中的全部交互均可映射到本文端点 |

---

## 1. 全局约定（与 NK3C 公开规范对齐）

### 1.1 响应封装 ResultInfo【S6】

```json
{ "success": true, "code": "0", "message": "ok", "data": { } }
```

- 前端**必须处理 `success=false` 分支**并展示 `message` 详情（历史教训：只提示"提交失败"会大幅增加联调成本【S13】）；
- `code`：`0` 成功；`4xxx` 业务拒绝（如 `4001 配额已满`、`4031 项目状态非法迁移`）；`5xxx` 系统错误；
- 框架不支持的场景用 `setHttpResponse` 返回异常，前端对 `responseText` 特殊处理【S6】。

### 1.2 ID 规范【S6】

- 后端 Long，**18 位精度**；序列化为 String 传前端（JS Number 仅 16 位精度）；
- 本规格所有 `id` 字段均为 String；示例：`"id": "9007199254740993"`。

### 1.3 路径与命名【S6】

- URL **全小写**（UNIX 路径大小写敏感）：`/api/sample/random-generate`；
- 控制器名与资源名一致；参数一律 `@RequestBody` JSON（分页/筛选可走 query）；
- ajax GET 加时间戳参数兼容 IE 缓存【S6】。

### 1.4 鉴权（Shiro）

- 登录 `/api/auth/login` 获得 `sessionId`；后续请求头 `Authorization: Bearer <sessionId>`；
- 权限四码【S7】：`domainAdmin / orgAdmin / groupAdmin / phoneAdmin`，登录响应返回 `perms`（资源 Code 列表），前端按 Code 控按钮显隐，**后端控制器二次校验**（防越权）。

### 1.5 幂等

写操作中"结果码提交 `/api/agent/result`"与"工单动作 `/api/workorder/{id}/action`"支持 `Idempotency-Key` 头；重复请求返回首写结果（对应全链路分析 4.4#6）。

### 1.6 分页

`?pageNum=1&pageSize=20` → `data: { total, rows }`。

---

## 2. 端点总表（47 个路径 → 按菜单分组）

| 分组 | 端点（方法 路径 — 说明） | 关联菜单/功能点 |
| --- | --- | --- |
| auth | POST /api/auth/login · POST /api/auth/logout | 登录日志 F0206 |
| org | GET/POST /api/org/domain · GET/POST /api/org/group · GET/POST /api/org/user · POST /api/org/user/{id}/disable · PUT /api/org/role/{roleId}/resources | M02.1~M02.5 |
| project | GET/POST /api/project · GET/PUT /api/project/{id} · POST /api/project/{id}/status · POST /api/project/{id}/distribute · PUT /api/project/{id}/schedule | M03.1~M03.6（F0301~F0306） |
| questionnaire | GET/POST /api/questionnaire · GET/PUT /api/questionnaire/{id} · POST …/validate · POST …/publish | M04.1~M04.8（F0401~F0408） |
| sample | POST /api/sample/import · POST /api/sample/random-generate · POST /api/sample/assign · POST /api/sample/{id}/blacklist | M05.2/5.3/5.4/5.6 |
| strategy | GET/POST /api/strategy | M06.1（F0601~F0605） |
| agent | POST /api/agent/login · POST /api/agent/state · POST /api/agent/dial · GET /api/agent/dispatch · POST /api/agent/answer · POST /api/agent/result | M07 坐席面（F0701~F0705） |
| monitor | GET /api/monitor/wall · POST /api/monitor/listen · POST /api/monitor/force · POST /api/monitor/message | M08.1~M08.3（F0801~F0804） |
| answer | GET /api/sheet · POST /api/sheet/{id}/audit · PUT /api/sheet/{id}/codes | M09.1~M09.3 |
| report | GET /api/report/single · GET /api/report/cross · GET /api/report/traffic | M10.1/10.3/10.4 |
| export | POST /api/export/task · GET /api/export/task/{id}/download | M11.1~M11.4（含审批） |
| qc | POST /api/qc/task · POST /api/qc/result | M12.1/12.2（致命项一票否决） |
| workorder | GET/POST /api/workorder · POST /api/workorder/{id}/action | M13.1/13.4/13.6 |
| ivr | GET/POST /api/ivr/flow · POST /api/ivr/flow/{id}/publish · POST /api/ivr/message/{id}/to-workorder | M14.1/14.2/14.4 |
| sys | GET/PUT /api/sys/param | M15.1（热生效 ≤5min） |

> 完整参数/响应结构以 YAML 为准；本表用于评审走查与前后端任务拆分。

---

| GET | `/api/export/:pid/:format` | 导出答卷（csv/xlsx/sav；?token= 或 Bearer） |
| GET | `/api/ws/monitor?token=` | 监控墙 WebSocket 推送（2s 周期 wall 帧） |
| GET | `/api/qc/ws?token=` | 质检事件流（仅督导；DIAL/ANSWER/RESULT/FORCE_LOGOUT 帧；Monitor 页实时表格订阅） |
| POST | `/api/qc/force-checkout` | 强签坐席（仅督导；吊销会话+样本回池） |

## 3. 关键交互 → 接口映射（原型对照）

### 3.1 坐席外呼一圈（对应坐席端原型全流程）

```
签入        POST /api/agent/login                    ── 软电话 NKZXAgent 联动【S7】
示闲        POST /api/agent/state {state:READY}
取样        GET  /api/agent/dispatch                 ── 派样锁样本（SKIP LOCKED）
预览呼出     POST /api/agent/dial {sampleId, phoneIndex}
逐题作答     POST /api/agent/answer ×N                ── 每题实时 upsert（断线续答）
提交结果码   POST /api/agent/result {callId, resultCode} ── 幂等；触发样本去向
```

### 3.2 督导监控（对应监控墙原型）

```
墙数据      GET  /api/monitor/wall                   ── 快照；生产建议 WS 增量推送
监听/插话   POST /api/monitor/listen {callId, action:LISTEN|BARGE|STOP}
强制操作    POST /api/monitor/force {agentId, action:FORCEBUSY…}
发消息      POST /api/monitor/message {agentId, text}
```

### 3.3 问卷全生命周期（对应设计器原型）

```
保存        PUT  /api/questionnaire/{id}              ── QuestionnaireDoc JSON
校验        POST /api/questionnaire/{id}/validate     ── err/warn/ok 报告（含死题 BFS）
发布        POST /api/questionnaire/{id}/publish      ── 版本+0.1，支持执行中调整【S3】
```

### 3.4 呼入链路（对应 IVR 设计器原型）

```
流程保存    POST /api/ivr/flow                        ── IvrFlowDoc
发布        POST /api/ivr/flow/{id}/publish
留言转工单   POST /api/ivr/message/{id}/to-workorder   ── 生成 WorkorderCreate
工单流转    POST /api/workorder/{id}/action           ── ACCEPT/TRANSFER/…/CLOSE
```

---

## 4. 错误码建议（code 语义）

| code | 场景 | message 示例 |
| --- | --- | --- |
| 0 | 成功 | ok |
| 4001 | 配额满格 | 配额【男/18-30】已满，答卷按 overflow 策略处理 |
| 4010 | 问卷未发布 | 启动失败：项目绑定的问卷未发布 |
| 4031 | 状态机非法迁移 | 项目当前 DRAFT，不允许执行 FINISH |
| 4032 | 无权限（权限码校验失败） | 需要 orgAdmin 权限 |
| 4041 | 样本池为空 | 派样失败：过滤后无可用样本 |
| 4091 | 幂等冲突 | 重复的结果码提交，已返回首写结果 |
| 4092 | 乐观锁冲突 | 数据已被他人修改，请刷新后重试 |
| 5000 | 系统异常 | 详细信息已记录，请联系管理员（日志号 xxx） |

---

## 5. 联调与实施建议

1. **Mock 优先**：以本 YAML 生成 Mock（Apifox/Yarn mock/openapi-mock），前后端并行；
2. **契约测试**：CI 中用 dredd/schemathesis 对实现做契约校验，防字段漂移；
3. **推送通道**：`/api/monitor/wall`、坐席消息、坐席状态建议生产环境以 WebSocket（STOMP）增量推送，HTTP 仅作降级轮询；
4. **审计**：全部写操作落 `sys_op_log`（模块/动作/前后值），导出类必须走审批端点；
5. **兼容注意**：IE10+ 目标（若沿用 NK3C 前端规范【S6】）——响应避免使用 IE 不支持的 ES6+ 语法直接输出。

> 数据库迁移：M3 的 `003_cti_extensions.sql`（Goose 兼容 Up/Down 标记）负责录音文件索引与答题质检时间字段的旧库增量升级。
| GET | `/api/export/:pid/columns` | 导出列清单（headers/types） |

导出文件接口支持 `cols=0,1,...` 列裁剪；web `/exports` 页面提供列选择与本地导出历史。

> 审计：认证后的写操作及导出/录音访问由 `sys_op_log` 中间件记录方法、路径、用户和状态码；请求体不入审计表。

| GET | `/api/audit/logs` | 督导及以上查询最近审计记录（支持 method/limit，最多 500） |

> `POST /api/ivr/call` 支持可选 `projectId`；转人工工单沿用该项目归属。真实 SIP 号码到项目的绑定路由仍待实现。

| PUT | `/api/ivr/routes` | 督导配置主叫号码前缀→项目路由 |
