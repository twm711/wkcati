# ITACATI NK-3C 呼出中心（CATI）

> 南康科技 ITACATI / NK-3C 电话外呼调查系统的**全链路交付 + 双轨实现**：
> 完整文档与原型 → Python 行为基准 Demo（31 用例全绿）→ Go + React 目标栈重构（骨架已落地）。

![status](https://img.shields.io/badge/里程碑-M0%2BM1%20已落地-green) ![go](https://img.shields.io/badge/Go-1.22-00ADD8) ![react](https://img.shields.io/badge/React-18%20·%20Antd%205-61DAFB) ![tests](https://img.shields.io/badge/tests-demo%2031%20·%20Go%209%20链路-brightgreen)

## 仓库总览

| 轨道 | 内容 | 状态 |
| --- | --- | --- |
| 📚 文档与原型 | 主文档 / Go+React 实现文档 / 部署手册 / OpenAPI 47 端点 / 3 版建表 SQL（60 表）/ 4 交互原型 / 96 条测试用例 / P0 矩阵 | ✅ 交付 |
| 🐍 nk3c-demo | FastAPI+SQLite 行为基准：坐席/督导/IVR/项目管理四控制台 Live、工单回访闭环、SPSS 导出、WS 实时质检 | ✅ 31 集成用例全绿 |
| 🚀 nk3c-go | Go 1.22 + gin 后端（M0+M1）：ResultInfo/RBAC/双方言存储/六大业务域 + 9 链路集成测试（-race 全绿） | ✅ 可运行 |
| ⚛️ nk3c-go/web | React 18 + Antd 5 + Vite 前端骨架：登录/坐席台/监控墙+审核/项目管理/工单/IVR 模拟器五页 | ✅ tsc 全绿 + 代理链 E2E |

## 快速开始

```bash
# ① Python 行为基准（:8000）
cd nk3c-demo && pip install -r requirements.txt
uvicorn app:app --host 0.0.0.0 --port 8000

# ② Go 后端（:8080）
cd nk3c-go && go build ./... && go run ./cmd/nk3c-api --addr :8080 --sip-addr 0.0.0.0:5060 --reset
# 话务域：真实 SIP 话机呼入 :5060（1 调研 / 2 留言 / 0 转人工自动落工单）

# ③ React 前端（:5173，/api 反代 :8080）
cd nk3c-go/web && pnpm install && pnpm dev
```

**演示账号**（密码统一 `123456`）：`admin`（系统管理员）/ `sup01`（督导）/ `agent01`（坐席 1020）。
**一键重置**：`POST /api/sys/reset`（需 admin 会话）。

## 测试

```bash
cd nk3c-demo && python3 -m pytest test_api.py -v      # 31 条链路断言
cd nk3c-go  && go test ./internal/itests/ -race -v    # 9 条业务链路
cd nk3c-go  && go test ./internal/media/ -v            # SIP/RTP 真实呼入 E2E（diago 回环）
cd nk3c-go/web && pnpm build                          # tsc --noEmit 严格类型检查
```

## 目录结构

```
├─ 南康科技_ITACATI_NK3C_开发文档.md        # 主文档（调研→设计→实现→测试）
├─ 南康_ITACATI_NK3C_实现文档_GoReact版.md  # Go+React 重构实现文档（M0–M6）
├─ ITACATI_NK3C_API接口规格.yaml            # OpenAPI 3.0（47 端点）
├─ ITACATI_NK3C_数据库建表*.sql             # MySQL / Oracle / SQL Server 三版（60 表）
├─ ITACATI_问卷设计器_交互原型.html 等 4 个   # 评审版交互原型
├─ ITACATI_NK3C_测试用例集.xlsx / P0矩阵     # 96 用例 + P0 覆盖映射
├─ autotest/                                # pytest 自动化骨架
├─ nk3c-demo/                               # 🐍 行为基准（FastAPI）
└─ nk3c-go/                                 # 🚀 目标栈（Go + React）
   ├─ cmd/nk3c-api/  internal/…  pkg/…
   └─ web/                                  # ⚛️ React 前端
```

## 路线图（M0–M6，详见实现文档）

- ✅ **M0** 可运行骨架（gin+ResultInfo+RBAC+迁移+React 五页）
- ✅ **M1** 业务闭环（派样四过滤/配额原子/结果码去向/审核状态机/工单回访 E2E/IVR 解释器）
- ◐ **M2** 话务域：diago 已接入——真实 SIP 呼入 IVR（DTMF→转人工落工单，SIP E2E 全绿）；外呼腿/B2BUA 待接
- ◻ **M3+** 录音质检/SPSS 导出移植/WS Hub/多机拓扑 → 见《实现文档_GoReact版》

> ⚠️ 已知环境约束：2GB 内存下 `modernc.org/sqlite` 编译与 `vite build` 均会 OOM，生产构建需 ≥4GB（详见 [交接文档](交接文档.md) §7）。

## License

[Apache-2.0](LICENSE)（仓库所有者建仓时选择）。注：项目文档中引用的南康科技 / ITACATI / NK-3C 商标与产品资料权利归原权利人所有，本项目为其复刻实现与教学演示。
