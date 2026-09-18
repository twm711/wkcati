# ITACATI / NK3C 自动化测试骨架

对应 `ITACATI_NK3C_测试用例集.xlsx`（96 条用例），实现"三层测试"骨架：
纯逻辑单测（立即可跑）→ UI 冒烟（Playwright 驱动 4 份 HTML 原型）→ 数据库并发（真实 MySQL）。

## 目录结构

```
autotest/
├── README.md                  本说明
├── pytest.ini                 pytest 配置（标记/输出）
├── conftest.py                公共 fixture（原型路径等）
├── test_logic_algorithms.py   ① 纯逻辑：预测拨号/配额原子/派样过滤/结果码去向/参数热生效
├── test_prototype_smoke.py    ② UI 冒烟：Playwright 驱动 4 份交互原型
└── test_db_dispatch.py        ③ DB 并发：派样锁 SKIP LOCKED / 配额原子扣减（需 MySQL）
```

## 快速开始

```bash
# 1) 逻辑用例（零依赖，应当全部通过）
pip install pytest
pytest test_logic_algorithms.py -v

# 2) UI 冒烟（需 Playwright + Chromium）
pip install pytest playwright
playwright install chromium
pytest test_prototype_smoke.py -v

# 3) 数据库并发（需 MySQL 8.0，先执行工作区根目录的建表脚本）
docker run -d --name nk3c-mysql -p 3306:3306 -e MYSQL_ROOT_PASSWORD=nk3c mysql:8.0
mysql -h127.0.0.1 -uroot -pnk3c < ../ITACATI_NK3C_数据库建表.sql
export TEST_MYSQL_HOST=127.0.0.1 TEST_MYSQL_PASS=nk3c
pytest test_db_dispatch.py -v
```

## 用例与《测试用例集》映射

| 测试文件 | 用例集分组 | 关键用例 |
| --- | --- | --- |
| test_logic_algorithms.py | 并发一致性 4.4#2/#7、M05 组 | 配额 1000 并发恰好 100 成功；呼损超限降速；半年原则/黑名单/重拨上限过滤 |
| test_prototype_smoke.py | M04/M07/M08/M14 组 | 设计器死题检测；必答拦截；坐席签入派样；监听/强制示忙留痕；IVR 按键分流 |
| test_db_dispatch.py | 并发一致性 4.4#1/#2 | 双连接派样结果无交集；UPDATE WHERE done<target 并发不超发 |

## 扩展指引

1. **补充用例**：每条 Excel 用例写成一个 `test_*` 函数，docstring 首行注明用例编号（如 `"""P0（TC-M09-002）：审核状态流转"""`）。
2. **接入真实系统**：把 `test_prototype_smoke.py` 的 `file://` 地址换成环境变量 `APP_BASE_URL` 即可测真实部署（原型选择器与真实实现保持同 id 约定可平滑迁移）。
3. **CI 建议**：逻辑层每次提交必跑；UI 层夜间跑；DB 层发布前跑。P0 用例全集（41 条）作为冒烟门禁。
