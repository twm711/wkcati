# NK3C Go 后端骨架（M0 可运行 + M1 业务闭环）

技术栈：Go 1.22 + gin + database/sql（SQLite 开发 / MySQL 生产，驱动可切换）。
与 `nk3c-demo`（Python 行为基准）接口语义逐条对齐，`internal/itests` 为移植断言。

## 运行

```bash
go build ./...
go run ./cmd/nk3c-api --addr :8080 --reset   # 首次/演示：重建种子数据
# 打开 http://localhost:8080/api/health
```

账号：admin（domainAdmin）/ sup01（groupAdmin）/ agent01（坐席 1020），密码均 123456。

## 测试

```bash
go test ./internal/itests/ -v   # 9 条链路集成测试（登录鉴权/派样过滤/作答/结果码/审核/生命周期/工单回访/IVR）
```

## 结构

- `pkg/rinfo` ResultInfo 统一响应（恒 HTTP 200 + 业务码 0/4001/4010/4031/4032/4041/4091/4092/5000）
- `pkg/ids` 雪花 ID（JSON 序列化为字符串，规避前端 Long 精度丢失）
- `internal/store` 双方言数据层 + 嵌入式迁移（18 表 + 种子）
- `internal/auth|project|agent|monitor|workorder|ivr` 六大业务域
- `internal/app` 装配与路由
- SQLite 驱动：mattn/go-sqlite3（CGO）；如需 CGO-free 可换 modernc.org/sqlite（编译需 ≥4GB 内存）

> 话务域（SIP/录音/IVR 实时流）按《实现文档_GoReact版》M2+ 接入 diago，本骨架先落业务闭环与数据链。

## 前端 web/（React 18 + Antd 5 + Vite 5）

```bash
cd web && pnpm install && pnpm dev   # :5173，/api 反代 :8080
```

五页骨架（登录/坐席工作台/监控墙+审核/项目管理/工单/IVR 模拟器）详见 `web/README.md`。
2GB 沙箱内 `vite build` 会 OOM（antd 3100 模块），验证口径 = `tsc --noEmit` + dev 代理链 E2E；生产构建需 ≥4GB 环境。
