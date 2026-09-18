# NK3C / ITACATI 部署手册（生产环境）

> 依据《南康科技_ITACATI_NK3C_开发文档》调研结论编制：硬件配置取自官方方案表【S1】，
> JVM/中间件参数取自 NK3C 运行配置【S8/S9】，坐席软电话部署取自 NKZXAgent 手册【S7】，
> 容量估算模型与《功能细化与全链路深度分析》4.5 算法一致。
> 配套：开发文档（17 章）· 功能细化 · OpenAPI 规格（47 端点）· 三方言建表 SQL · nk3c-demo 演示系统。

---

## 1. 拓扑架构（B/S/S 三层）【S1/S2】

```
┌────────────────────────────────────────────────────────────────┐
│  客户端层                                                        │
│  访员坐席 PC（IE10+/Chrome + NKZXAgent 软电话【S7】+ 耳机话机）     │
│  管理台/督导 PC（浏览器）     外呼手机/固话（PSTN 被叫）             │
└──────────────┬─────────────────────────────────────────────────┘
               │ HTTP / WebSocket
┌──────────────▼─────────────────────────────────────────────────┐
│  接入层    Nginx / F5（负载均衡 · 静态资源 · WSS 终结）             │
└──────────────┬─────────────────────────────────────────────────┘
┌──────────────▼─────────────────────────────────────────────────┐
│  应用层（每程序独立 Tomcat【S8】，可水平扩展）                      │
│  nweb（管理端+坐席 Web）   nservice*（样本池/答卷/统计 SOA 服务）   │
│  nagentstateserver（坐席状态/监控事件，堆 370M 起步）               │
│  消息中心（独立部署）        IVR/录音/报表服务                      │
└───────┬──────────────────────────────┬─────────────────────────┘
        │ JDBC(MyBatis+Druid【S9】)      │ CTI Proxy（平台无关【S2】）
┌───────▼──────────────┐   ┌─────────────▼───────────────────────┐
│  数据层               │   │  语音层                              │
│  MySQL 主从（默认）    │   │  交换机/板卡 ← 语音网关               │
│  可换 Oracle/SQLServer │   │  （预测拨号/外呼策略由 CTI 执行【S3/S4】）│
│  + Redis（会话/队列）  │   └─────────────────────────────────────┘
└──────────────────────┘
```

**关键设计约束**（蓝图结论，部署时必须保持）：
- CTI Proxy 屏蔽交换机差异：换平台不动应用【S2】；
- 样本池、消息中心独立部署：派样高并发与推送解耦【S2】；
- 三库支持：MySQL / Oracle / SQLServer（DDL 三方言已交付，连接参数见 §4）；
- 分权分域（domainAdmin/orgAdmin/groupAdmin/phoneAdmin【S7】）随库初始化。

## 2. 硬件配置（官方方案表【S1】）

**配置表 1：小于 32 坐席（最低配置）**

| 机器 | 操作系统 | 处理器 | 硬盘 | 内存 | 其他 |
| --- | --- | --- | --- | --- | --- |
| DatabaseServer + WebServer（可同机） | Win2003 Server SP1* | 双核 2G | 80G | 2G | SQL Server 2005 或 Oracle 9i |
| CTIServer | Win2003 Server SP1* | P4 2.8G | 160G | 1G | 连接交换机/板卡 |
| 访员客户机 | WinXP Pro* | P4 2G | 60G | 512M | IE6 + 耳机话机 |
| 管理台计算机 | WinXP Pro* | P4 2.8G | 60G | 1G | IE6 |

**配置表 2：100 坐席（最低配置）**

| 机器 | 操作系统 | 处理器 | 硬盘 | 内存 | 其他 |
| --- | --- | --- | --- | --- | --- |
| DatabaseServer | Win2003 Server SP1* | 双核 3G×2 | 80G | 4G | SQL Server 2005 或 Oracle 9i |
| WebServer ×2 | Win2003 Server SP1* | 双核 3G | 80G | 2G | 每台支持 50 坐席 |
| CTIServer ×2 | Win2003 Server SP1* | P4 2.8G | 200G | 2G | 每台支持 50 坐席 |
| 访员客户机 | WinXP Pro* | P4 2G | 60G | 512M | IE6 |
| 管理台计算机 | WinXP Pro* | P4 2.8G | 60G | 1G | IE6 |

\* 表为 2007 年官方原始配置。**2026 年实施建议等比折算**：OS 换 Linux（CentOS/统信 UOS）或 Win2019+，
客户机浏览器按 NK3C 前端规范 IE10+（建议 Chrome/Edge【S6】），磁盘按录音存储估算（§5.3）。

## 3. 软件环境与中间件参数【S6/S8/S9】

| 组件 | 版本/参数 | 说明 |
| --- | --- | --- |
| JDK | 1.8（按 nkframework 基线） | 每程序独立 Tomcat |
| Tomcat | 每程序一个实例 | 便于独立升级与故障隔离【S8】 |
| JVM（通用单程序） | `-Xmx768m -XX:MaxPermSize=160m` | 机器最低内存 2G【S8】 |
| nweb | 堆 250M / PermGen 50M | 管理端+坐席 Web 主应用【S8】 |
| nagentstateserver | 堆 370M（起步） | 坐席状态/监控事件分发【S8】 |
| 数据访问 | MyBatis + Druid 连接池 | SQL Server 驱动 `sqlserver.driver.version=4.0`【S9】 |
| 数据库 | MySQL 5.7+（默认）/ Oracle 12c / SQL Server 2014+ | 三方言 DDL 已交付 |
| 前端 | velocity + requireJS + jQuery2.0 + nkui/vuejs | IE10+【S6】 |
| 坐席软电话 | NKZXAgent（.NET 4.0，reg.bat 注册，零配置 URL:9080/was） | TestAgt.exe 可自测【S7】 |

## 4. 端口规划

| 端口 | 服务 | 协议 |
| --- | --- | --- |
| 80/443 | Nginx 接入（HTTP/HTTPS） | TCP |
| 9080 | nweb（含软电话零配置入口 `/was`）【S7】 | HTTP |
| 8088 | nagentstateserver（坐席状态/WS 推送） | HTTP/WS |
| 3306 / 1521 / 1433 | MySQL / Oracle / SQLServer | TCP |
| 6379 | Redis | TCP |
| 5060 | 语音网关 SIP 信令 | UDP/TCP |
| 网关 RTP 段 | 语音媒体流 | UDP |

> 防火墙仅需放通：客户端→接入层 80/443；接入层→各 Tomcat；应用→DB/Redis/CTI；CTI→语音网关。

## 5. 容量估算模型（与 4.5 算法一致）

### 5.1 话务并发

- 坐席占用率 **OCC = ATT / (ATT + 话后处理 HT)**（0~1）；
- 系统并发话务 **N = ceil(坐席数 × 外呼比例 ratio × 接通率 CR)**；
- 例：100 席、ratio=1.4、CR=0.6 → 并发 ≈ 84 路；OCC 按 0.55 目标配置 HT 定额（autotest 逻辑层同参数）。

### 5.2 中继线（Erlang B，P.01）

话务量 A(尔朗) = BHCA × AHT / 3600；中继线 ≈ ErlangB(A, 0.01)。
示例：100 席每小时呼出 3600 通、AHT=120s → A=120 尔朗 → 约 133 线（含 10% 余量建议 145 线）。

### 5.3 存储

| 对象 | 单位大小 | 估算（100 席 × 8h × 240 通/席/日） |
| --- | --- | --- |
| 话务记录行 | ~0.5 KB | 19.2 万行/日 ≈ 90 MB/月 |
| 答卷+答案行 | ~2 KB | ≈ 360 MB/月 |
| 录音（G.711 64kbps） | 0.5 MB/分钟 | AHT 2min → **≈ 45 GB/日**，建议录音 ≥90 天并转存冷备 |

### 5.4 应用内存

每 50 坐席一组 WebServer/nagentstateserver（官方表）；JVM 参数见 §3；Redis 会话按坐席数×2KB 估算（可忽略）。

## 6. 部署步骤（标准序列）

1. **数据库**：装库 → 执行三方言 DDL（按目标库选 `ITACATI_NK3C_数据库建表.{mysql,oracle,sqlserver}.sql`）→ 初始化参数/菜单/权限码种子；
2. **应用**：部署各 Tomcat（nweb → nservice* → nagentstateserver → 消息中心）→ 连接串/Druid 池按 §3；
3. **接入**：Nginx 上游挂 nweb 集群，WSS 反向代理 nagentstateserver；
4. **语音**：CTIServer 连接交换机/网关 → 配外呼策略（预测/预览/自动/混合【S3】）→ 中继联调；
5. **坐席**：客户机装 .NET 4.0 → `reg.bat` 注册 NKZXAgent → TestAgt.exe 自测 → 浏览器登录零配置 URL【S7】；
6. **验证**：跑《测试用例集》P0 用例（登录/派样/作答/结果码/配额/审核 30 条）→ 灰度 5 席 → 全量。

## 7. 上线检查单

- [ ] 三库 DDL 执行无报错，`smp_status_code`/`sys_param`/菜单（101–115）/权限码种子齐全
- [ ] 每程序独立 Tomcat，JVM 参数按 §3；`DisallowedRunLevel` 无端口冲突
- [ ] CTI Proxy 连通：测试呼叫可起呼/拆线/取录音路径
- [ ] 监控墙 WS 推送正常（签入/通话状态秒级刷新）；质检六指令可用（监听/插话/示忙/示闲/强挂/强签）
- [ ] 派样过滤四规则生效（黑名单/半年原则/重拨上限/派样锁并发验证）
- [ ] 配额并发压测：8 线程×20 次派样不超发（对照 autotest DB 层用例）
- [ ] 答卷审核 + 录音回放同步定位正常；导出中心 CSV/Excel/SPSS 抽检通过
- [ ] 备份策略：DB 每日全备 + binlog；录音存储监控水位 ≥80% 告警

## 8. nk3c-demo 演示系统 → 生产对照

| 演示（nk3c-demo） | 生产（NK3C） |
| --- | --- |
| FastAPI + SQLite 单机 | Spring(nweb 五层【S6】) + MySQL 主从 |
| `BEGIN IMMEDIATE` 派样锁 | `FOR UPDATE SKIP LOCKED`（附录样例1） |
| 内存 TOKENS 会话 | Shiro 会话 / Redis |
| WS 快照全量推送 | nagentstateserver 增量/STOMP |
| 服务端合成 WAV 音轨 | 录音存储（CTI 落盘）+ aud_start/aud_end 对齐 |
| 按键模拟 IVR | 语音网关（放音/收号/ASR）+ 转接队列 |
| 质检强挂为通知式 | CTI 网关真实拆线 |
| 导出 CSV/XLSX | 补 Quantum/SPSS/Txt 与异步导出任务 |

> 演示系统的表名/字段名/接口路径与生产 DDL/规格一一对应，可直接作为开发联调基准。
