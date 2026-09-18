# NK3C / ITACATI 演示系统（API + 三 Live 控制台）

> 蓝图的可执行验证：把《ITACATI_NK3C_数据库建表.sql》的 15 张核心表 + 《API 接口规格》的关键端点 + 《功能细化与全链路深度分析》的核心算法（派样过滤 / 派样锁 / 配额原子扣减 / 结果码去向 / 幂等）跑起来，并用 **Live 坐席台 + Live 督导台** 做到前后端贯通。
> 单机演示级（FastAPI + SQLite）；生产按蓝图用 Spring(nweb 五层)/MySQL 实现，**表名/字段名/接口路径与生产 DDL/规格一一对应**。

## 运行

```bash
pip install -r requirements.txt
uvicorn app:app --host 0.0.0.0 --port 8000
```

| 入口 | 说明 |
| --- | --- |
| `/` | **控制台首页**：坐席台 / 督导台 / API 文档 三入口 |
| `/agent` | **坐席工作台 Live 版**（真实登录/派样/作答/结果码，页面展示样本去向与配额扣减） |
| `/supervisor` | **督导控制台 Live 版**（监控墙 WS 实时推送 / 话务流水 / 答卷审核 / 实时统计 / 导出中心） |
| `/ivr` | **IVR 流程设计器 Live 版**（可视化编排呼入流程 + 话机模拟器按键真实走线） |
| `/projects` | **项目管理台 Live 版**（创建→加题→配额→发布→导样→修订 v1.x+1→暂停/结束 全生命周期可视化） |
| `/docs` | Swagger UI（OpenAPI 交互式文档） |
| `POST /api/sys/reset` | 一键重置演示数据（需 admin 登录；两个控制台右下角均有同名按钮） |

演示账号（密码均为 `123456`）：

| 登录名 | 角色 | 说明 |
| --- | --- | --- |
| admin | domainAdmin,orgAdmin,groupAdmin,phoneAdmin | 全权限（可重置数据） |
| sup01 | groupAdmin | 督导（监控墙 WS / 审核 / 导出） |
| agent01 | phoneAdmin | 一线坐席（Live 坐席台默认账号） |

## 测试（31 条集成用例，确定性顺序链）

```bash
pytest -v
# ①登录/鉴权 ②首派101+全流程(配额扣减/统计) ③幂等 ④过滤规则排空
# ⑤配额原子不超发(自校验) ⑥审核 ⑦驳回重访 ⑧INVALID自动入黑名单 ⑨监控墙
# ⑩督导台页面 ⑪话务流水 ⑫WebSocket推送(快照+坏token拒绝) ⑬导出CSV/XLSX+权限
# ⑭IVR流程保存校验(坏流程拒/坐席无权) ⑮IVR呼入走线(调研/留言/转人工/放弃)
# （标题行数为 14，实际 16 条：⑩督导台+首页 ⑪话务流水 各为一条）
```

## Live 坐席台（static/agent_live.html）

由后端托管（`GET /agent`，同源免跨域），纯原生实现，无外部依赖：

- **签入** → 调 `/api/auth/login`（角色/权限码来自服务端）→ 自动派样
- **派样** → 调 `/api/agent/dispatch`：样本 101（张一）优先；103 黑名单 / 104 半年原则 / 105 重拨超限 / 102 闭环均被服务端过滤
- **呼叫** → 本地振铃动画（演示 100% 接通）；接通后问卷由服务端下发渲染
- **作答** → 每题实时 `POST /api/agent/answer`（数值题范围校验前后端双重拦截）
- **结果码** → `POST /api/agent/result`：页面直接展示服务端判定的 **样本去向 / 答卷状态 / 配额扣减结果**；SUCCESS 后实时统计立即可查
- **重置** → 右下角按钮：admin 登录后调 `/api/sys/reset`，恢复种子数据并刷新

## Live 督导台（static/supervisor_live.html）

由后端托管（`GET /supervisor`），四个实时功能区 + 导出中心：

- **项目 / 配额总览** → `GET /api/project`：全部项目并行总览（状态/问卷版本/样本余量/配额进度条），随监控刷新
- **坐席监控墙** → `WS /ws/monitor` 实时推送：坐席一派样（DIALING）、一挂机（READY），督导墙立即变色刷新；断线自动**降级为 3s 轮询**（生产同款兜底策略）
- **话务流水** → `GET /api/monitor/calls`：最近 12 通（样本/坐席/状态/结果码/时间）
- **答卷审核** → `GET /api/sheet` + `POST /api/sheet/{id}/audit`：通过/驳回走真实状态机，驳回填原因→样本回池生成重访
- **录音回放同步质检** → `GET /api/sheet/{id}/answers`（答卷明细+录音地标）+ `GET /api/qc/recording/{callId}`（服务端合成 WAV）：真实波形解码绘制、播放进度与答案**同步高亮**（S2"录音回放同步质检"；音频控件免 Header 用 token 查询参数鉴权，坐席 403）
- **配额与实时统计** → `GET /api/report/single?questionId=11`：配额格进度条 + Q11 频数图（SUBMITTED+AUDITED 口径）
- **导出中心** → `GET /api/export/answers|stats?format=csv|xlsx`：Blob 下载真实文件（CSV 带 UTF-8 BOM，Excel 直接打开不乱码；XLSX 由 openpyxl 内存生成，统计版含"单题统计+配额进度"双 sheet）

## Live IVR 流程设计器（static/ivr_live.html）

由后端托管（`GET /ivr`），左侧设计器 + 右侧话机模拟器：

- **流程编排** → 节点可视化编辑（放音/菜单/问题/留言/转人工/结束 六类），按键分支与答案标识随改随存；`PUT /api/ivr/flow` 先静态校验（entry 存在/id 唯一/跳转完备）再发布，**保存即生效，下一通呼入按新流程走线**（对应 S4"中途可调问卷/流程"）
- **话机模拟** → `POST /api/ivr/call` 模拟呼入（welcome 自动放完到菜单），按键走线：menu 按 1 → 问卷（1 男/2 女 → 0-9 打分，答案按 tag 记录）→ 结束；按 2 → 留言（模拟录音文本）；按 0 → 转人工；无效键重播当前节点；随时挂断按 ABANDONED 落库
- **来电记录** → `GET /api/ivr/logs`：主叫/结局（SURVEY/VOICEMAIL/TRANSFER:MANUAL/ABANDONED）/走线路径/按键答案

## 实时质检（S5 督导接口）

督导台坐席卡内置六个质检按钮，指令经 `POST /api/monitor/qc`（groupAdmin+）下发，**WS 定向推送**到坐席台（`WS /ws/agent`，断线自动重连）：

| 指令 | 服务端动作 | 坐席台表现 |
| --- | --- | --- |
| 🎧 监听 LISTEN | 仅通知 | 顶部黄条"督导正在监听本通话" |
| 💬 插话 INSERT | 仅通知 | 顶部绿条"督导已插话" |
| 🚫 示忙 SETBUSY / ✅ 示闲 SETIDLE | 通知 | 坐席状态切 AUX/READY，示闲后自动补派样 |
| 📴 强挂 FORCEHANGUP | 通知 | 自动执行挂断→话后处理，红条提示提交结果码 |
| ⏏ 强签 FORCECHECKOUT | **踢会话**（生产同步 nagentstateserver） | 强制登出回签入界面，后续请求 401 |

质检动作同步广播到督导墙（`qclog`，仅 monitor 通道，无跨通道回声），坐席台横幅带督导姓名与备注。

## 工单流转（呼入转工单闭环 · P0 行96）

- **自动落单** → IVR 转人工通话结束时，服务端自动生成 HIGH 优先级工单（主叫/通话轨迹入 detail）
- **状态机** → `PENDING → 受理 ACCEPTED（认领人+时间）→ 办结 RESOLVED（备注）→ 归档 CLOSED（督导）`
- **守卫** → 非法跳转 4031（PENDING 不能直办结、重复受理拒绝）、坐席归档 4032
- **督导台面板** → 工单流转列表（状态着色/筛选/受理-办结-归档按钮），随监控 WS 事件自动刷新
- **回访闭环（P0 行96 全链）** → 归档工单自动生成回访样本（`回访-工单#N`，主叫号码）进项目1池 → 坐席外呼回访 → 答卷 AUDITED → `GET /api/workorder/{id}` 详情断言 **工单-样本-答卷双链路关联**
- SPSS 导出 → `GET /api/export/spss?projectId=1`：**.sav 真实格式**（$FL2 魔数，pyreadstat 生成），变量标签=题干、值标签=选项文本、行数=已审答卷数（P0 行69）
- 41 条 P0 用例的自动化覆盖见《南康_ITACATI_P0用例覆盖矩阵.md》与用例集 Excel「03 P0自动化覆盖」sheet（✅20 / ◐7 / ◻4 / ▢10）

## Live 项目管理台（static/projects_live.html）

由后端托管（`GET /projects`），第四个 Live 控制台，把"多项目管理"从 API 变成可视化操作：

- **项目列表**：全部项目卡片（状态徽章/问卷版本/样本可派/配额进度条），点选进详情
- **DRAFT**：加题（单选/数值/文本表单）→ 设置配额（选题→勾选项→目标份数，草稿整组保存）→ 发布
- **RUNNING**：**修订问卷**（版本 v1.x+1，已答答卷保留旧快照）→ 导入样本（同项目号码查重自动跳过）→ 暂停/结束
- **PAUSED**：恢复/结束；**FINISHED**：终态只读
- 操作即调 API，与 pytest 断言同一套端点（创建→发布→导样→修订→派样 可全程在页面完成）

> 注意：原型预览沙箱无网络，Live 版必须在**服务预览**（或本地 `http://localhost:8000/`）中打开。

## 端点与蓝图对照

| 端点 | 蓝图依据 |
| --- | --- |
| POST /api/auth/login | 规格认证组；登录写日志（demo 略） |
| GET /api/agent/dispatch（?projectId=） | 4.5.2 派样算法 = MySQL 附录样例1；多项目：校验 RUNNING+PUBLISHED 后按项目取样/下发对应问卷 |
| GET/POST /api/project · GET /api/project/{pid} · POST .../questions · PUT .../quota · POST .../publish · POST .../samples | project/questionnaire/sample 三组端点：DRAFT→(加题/配额)→发布 v1.0 RUNNING→导样（同项目号码查重跳过）；详情端点聚合题目/配额/样本/答卷版本分布 |
| POST /api/project/{pid}/revise | S4 中途可调 + P0 行24：RUNNING 修订问卷 → 版本 v1.x+1，已答答卷保留旧版本快照，新话务按新版本执行 |
| POST /api/project/{pid}/status | 项目状态机 pause/resume/finish（PAUSED 派样挂起；FINISHED 终态；非法跳转 4031） |
| POST /api/agent/answer | 4.4#4 逐题实时 upsert（断点续答）；首题=接通（connect_time/INCALL） |
| POST /api/agent/result | 4.2.3 结果码去向（闭环/回池/预约/黑名单）+ 4.4#6 幂等 + 4.5.3 配额原子扣减 |
| GET /api/monitor/wall | M08 监控墙快照（轮询兜底通道） |
| WS /ws/monitor | M08 实时推送（生产为 nagentstateserver → WS/STOMP 增量推送）；督导权限校验，坏 token 4401 |
| GET /api/monitor/calls | M10 话务统计的实时流水版 |
| GET /api/sheet · POST /api/sheet/{id}/audit | M09 审核状态机；REJECT→样本回池（重访） |
| GET /api/report/single | 附录样例3 单题频数（SUBMITTED+AUDITED 口径）+ 配额进度 |
| GET /api/export/answers · /api/export/stats | S3 导出中心（Excel/Txt/Quantum/SPSS → demo 实现 CSV/XLSX；督导权限 4032） |
| GET/PUT /api/ivr/flow | S4 任意级跳转/中途可调；静态校验后即时生效（groupAdmin+） |
| POST /api/ivr/call · /input · /hangup · GET /api/ivr/logs | S4/S5 菜单分流/收号/留言/转人工（demo 按键模拟，生产对接语音网关/CTI） |
| POST /api/monitor/qc · WS /ws/agent | S5 实时质检接口（监听/插话/强制示忙/示闲/挂断/签出）；强签服务端踢会话 |
| GET /api/sheet/{id}/answers · GET /api/qc/recording/{callId} | S2 答卷审核+录音回放同步（demo 合成音轨，生产对接录音存储按 aud_start 对齐） |
| GET /api/workorder · GET /api/workorder/{id} · POST /api/workorder/{id}/accept·resolve·close | M13 工单管理：IVR 转人工自动落单 + 受理/办结/归档状态机；归档自动生成回访样本，详情端点聚合双链路（P0 行96 全闭环） |
| GET /api/export/spss | M11/P0 行69：SPSS .sav 真实格式（pyreadstat），变量标签=题干、值标签=选项文本 |
| POST /api/sys/reset | 演示辅助：种子数据恢复（domainAdmin；WS 广播 reset 事件，督导台自动刷新） |

## 种子数据（覆盖全部过滤规则）

| 样本 | 状态 | 预期 |
| --- | --- | --- |
| 101 张一 | IDLE，无接触 | **第 1 个被派** |
| 102 李二 | SUCCESS | 永不派（闭环） |
| 103 王三 | 号码在黑名单 | 永不派 |
| 104 赵四 | 30 天前接通过 | 半年原则拦截（改 `halfyear.days` 参数可放行） |
| 105 孙五 | 已呼 3 次 | 重拨上限拦截 |
| 106 周六 | 呼过未接通 | **第 2 个被派**（接通才刷新原则） |

问卷：Q11 性别（男/女，挂配额 target=2×2）+ Q12 满意度（0-10）。

**项目 2：产品偏好调查_2026**（RUNNING / 问卷 v1.0 PUBLISHED）——多项目并行演示：

| 样本 | 状态 | 预期 |
| --- | --- | --- |
| 201 钱七 | IDLE，无接触 | **第 1 个被派**（?projectId=2） |
| 202 吴八 | SUCCESS | 永不派（闭环） |
| 203 郑九 | 号码在黑名单 | 永不派 |
| 205 冯十 | 已呼 3 次 | 重拨上限拦截 |
| 206 陈一 | 呼过未接通 | **第 2 个被派** |

问卷：Q21 年龄段（18-30/31-45/46+，配额 2/2/1）+ Q22 推荐意愿。跨项目作答被服务端拒绝（题目必须属于话务项目的问卷）。

## 快速体验（curl）

```bash
T=$(curl -s -X POST localhost:8000/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"loginName":"agent01","password":"123456"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['sessionId'])")

curl -s localhost:8000/api/agent/dispatch -H "Authorization: Bearer $T"     # 派样
curl -s -X POST localhost:8000/api/agent/answer -H "Authorization: Bearer $T" \
  -H 'Content-Type: application/json' \
  -d '{"callId":1,"questionId":11,"optionIds":[111]}'                       # 作答
curl -s -X POST localhost:8000/api/agent/result -H "Authorization: Bearer $T" \
  -H 'Content-Type: application/json' \
  -d '{"callId":1,"resultCode":"SUCCESS"}'                                  # 结果码
curl -s "localhost:8000/api/report/single?questionId=11" -H "Authorization: Bearer $T"  # 实时统计
```

## 与生产实现的差距（按蓝图补齐）

SQLite 单机 → MySQL 主从 + 独立样本池服务；`BEGIN IMMEDIATE` → `FOR UPDATE SKIP LOCKED`；
内存会话 → Shiro/Redis；无录音/CTI 事件 → 接语音网关（CTI Proxy）；
demo WS 为监控快照全量推送 → 生产 nagentstateserver 做增量/STOMP；导出 CSV/XLSX → 补 Quantum/SPSS/Txt 与异步大任务；IVR 按键模拟 → 对接语音网关（放音/收号/ASR）与转接队列；质检强挂为通知式 → 生产由 CTI 网关真实拆线；
Live 控制台 → nweb（springMVC+velocity/nkui）。表结构迁移脚本已备（三方言 DDL）。
