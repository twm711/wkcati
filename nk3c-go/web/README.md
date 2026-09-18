# NK3C React Web（Go 后端配套前端骨架）

技术栈：React 18 + TypeScript（strict）+ Ant Design 5 + Vite 5 + React Router 6（Hash）+ Axios。
对接 `../cmd/nk3c-api`（:8080），开发态 `/api` 由 Vite 代理，生产态走同源反代。

## 运行

```bash
pnpm install
pnpm dev        # http://localhost:5173 （/api → http://127.0.0.1:8080）
pnpm build      # tsc --noEmit + vite 产物构建（需 ≥4GB 内存环境，见下）
```

账号：admin（系统管理员）/ sup01（督导）/ agent01（坐席 1020），密码统一 123456。

## 页面（M0+M1 子集）

| 路由 | 页面 | 能力 |
| --- | --- | --- |
| /login | 登录 | 三角色快捷填充；ResultInfo 信封处理 |
| /agent | 坐席工作台 | 派样 → 动态问卷渲染（单选/多选/数值/文本）→ 逐题实时提交 → 10 结果码提交（去向/配额/幂等展示） |
| /monitor | 监控墙 | 5s 轮询坐席状态 + 统计卡 + 话务流水；督导答卷审核（PASS/REJECT 驳回回池） |
| /projects | 项目管理 | 列表/详情（题目/配额/样本分布/答卷版本分布）、新建/发布/暂停/恢复/结项、修订加题（版本化）、导入样本（查重）、设置配额、单题统计 |
| /workorders | 工单中心 | 状态筛选/详情/受理/办结/归档（自动回访样本 + 回访链路展示） |
| /ivr | IVR 互动 | 流程节点只读视图 + 话机模拟器（呼入/按键/留言/挂断/转人工落单）+ 呼入日志 |

关键约定（承 S13 教训）：后端恒 HTTP 200，`src/api.ts` 拦截器统一判**响应体 `code`**（4010 → 清会话回登录页）。

## 已知环境约束

2GB 内存沙箱中 `vite build`（Rollup 打包 antd 全家，3100 模块）会被 OOM 杀；
本仓库内验证口径为 `tsc --noEmit` 全绿 + dev server 代理链 E2E；生产构建请在 ≥4GB 内存机器/CI 执行。
