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

## 前端 web/（React 18 + Antd 5 + Vite 5）

```bash
cd web && pnpm install && pnpm dev   # :5173，/api 反代 :8080
```

五页骨架（登录/坐席工作台/监控墙+审核/项目管理/工单/IVR 模拟器）详见 `web/README.md`。
2GB 沙箱内 `vite build` 会 OOM（antd 3100 模块），验证口径 = `tsc --noEmit` + dev 代理链 E2E；生产构建需 ≥4GB 环境。
