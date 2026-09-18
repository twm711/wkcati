# -*- coding: utf-8 -*-
"""
核心算法单元测试（纯 Python，无外部依赖，可直接运行）
对应《ITACATI_功能细化与全链路深度分析》4.5 核心算法 与测试用例集"并发一致性"分组。

用例映射：
  test_predictive_dial_*  -> 4.5.1 预测拨号量 / 用例 4.4#7
  test_quota_atomic_*     -> 4.5.3 配额原子扣减 / 用例 4.4#2
  test_sample_filter_*    -> 4.5.2 派样过滤（半年原则/黑名单/重拨上限）/ 用例 M05 组
  test_result_dest_*      -> 4.2.3 结果码与样本去向 / 用例 M05.7 组
"""
import threading
import time
from datetime import datetime, timedelta

import pytest

# ----------------------------------------------------------------------------
# 4.5.1 预测拨号量算法（与前端原型/文档公式一致）
# ----------------------------------------------------------------------------
def predictive_dial_count(a_ready, att, ht, connect_rate, dial_ratio,
                          abandon_rate, abandon_max,
                          speedup_streak=0, occ_threshold=0.9, in_window=True,
                          ratio_upper=3.0):
    """返回 (拨号量N, 新拨号倍率)。每 T=5s 决策一次。
    OCC = A×ATT/(ATT+HT) 服务占用模型（Erlang 近似）
    N = ceil(A × ratio × CR)；呼损超限降速（×0.8）；持续无呼损且 OCC<0.9 提速（×1.1）。
    """
    if not in_window or a_ready <= 0:
        return 0, dial_ratio
    occ = att / (att + ht) if (att + ht) > 0 else 0   # 占用率分数（0~1）：通话时长占比
    ratio = dial_ratio
    if abandon_rate > abandon_max:
        ratio = max(0.5, ratio * 0.8)
        n = int(a_ready * ratio * connect_rate * 0.8)  # 降速双保险
    elif speedup_streak >= 3 and occ < occ_threshold:
        ratio = min(ratio_upper, ratio * 1.1)
        n = int(-(-a_ready * ratio * connect_rate // 1))  # ceil
    else:
        n = int(-(-a_ready * ratio * connect_rate // 1))
    return max(0, n), ratio


class TestPredictiveDial:
    def test_normal_dial_count(self):
        """P0：常规场景 A=10,ATT=120s,HT=30s,CR=0.6,ratio=1.5 → N=9"""
        n, ratio = predictive_dial_count(10, 120, 30, 0.6, 1.5, 1.0, 3.0)
        assert n == 9 and ratio == 1.5

    def test_abandon_over_limit_reduces(self):
        """P0：呼损率 4% > 上限 3% → 倍率降为 1.2，N 显著下降"""
        n, ratio = predictive_dial_count(10, 120, 30, 0.6, 1.5, 4.0, 3.0)
        assert ratio == pytest.approx(1.2)
        assert n < 9

    def test_speedup_capped(self):
        """P1：连续无呼损且占用低 → 倍率提速但不超过上限 3.0"""
        n, ratio = predictive_dial_count(10, 120, 30, 0.9, 2.8, 0.0, 3.0, speedup_streak=5)
        assert ratio == 3.0
        assert n == 27  # ceil(10×3.0×0.9)

    def test_window_closed_zero(self):
        """P0：时段窗外/排班外 → 拨号量 0（策略闸门）"""
        n, _ = predictive_dial_count(10, 120, 30, 0.6, 1.5, 1.0, 3.0, in_window=False)
        assert n == 0

    def test_no_agents_zero(self):
        n, _ = predictive_dial_count(0, 120, 30, 0.6, 1.5, 1.0, 3.0)
        assert n == 0


# ----------------------------------------------------------------------------
# 4.5.3 配额原子扣减（模拟 UPDATE ... SET done=done+1 WHERE done<target 的原子性）
# ----------------------------------------------------------------------------
class QuotaCell:
    """以 DB 原子语义模拟：扣减成功返回 True，满格返回 False。"""
    def __init__(self, target):
        self.target = target
        self.done = 0
        self._lock = threading.Lock()   # 模拟 InnoDB 行锁

    def try_consume(self):
        with self._lock:
            if self.done < self.target:
                self.done += 1
                return True
            return False


class TestQuotaAtomic:
    def test_concurrent_exactly_target(self):
        """P0（4.4#2）：目标 100，1000 并发提交 → 恰好 100 成功，900 被拒，done==100"""
        cell = QuotaCell(100)
        ok, reject = [], []
        lk_ok, lk_rj = threading.Lock(), threading.Lock()

        def worker():
            r = cell.try_consume()
            (ok if r else reject).append(1)

        threads = [threading.Thread(target=worker) for _ in range(1000)]
        for t in threads: t.start()
        for t in threads: t.join()
        assert cell.done == 100
        assert len(ok) == 100
        assert len(reject) == 900

    def test_overflow_allowed_cell(self):
        """P1：overflow 放行策略 —— 满格后按项目配置决定是否继续"""
        cell = QuotaCell(3)
        for _ in range(3):
            assert cell.try_consume() is True
        assert cell.try_consume() is False  # 默认不放行
        # overflow_flag=1 的格由应用层直接 done+=1，不占 target（此处断言语义占位）


# ----------------------------------------------------------------------------
# 4.5.2 派样过滤（等价 SQL 附录1：IDLE + 半年原则 + 黑名单 + 重拨上限 + 打散）
# ----------------------------------------------------------------------------
HALFYEAR_DAYS = 180
REDIAL_MAX = 3
NOW = datetime(2026, 9, 17, 10, 0, 0)

def pick_samples(samples, blacklist, n=1):
    """按派样 SQL 语义返回可用样本（已按 shuffle_key 排序）。
    sample: dict(id, status, last_connected_at, attempts, shuffle_key)
    """
    picked = []
    for s in sorted(samples, key=lambda x: x["shuffle_key"]):
        if len(picked) >= n:
            break
        if s["status"] != "IDLE":
            continue
        if s["attempts"] >= REDIAL_MAX:
            continue
        if s["id"] in blacklist:
            continue
        lc = s.get("last_connected_at")
        if lc is not None and (NOW - lc).days < HALFYEAR_DAYS:
            continue  # 半年原则：180 天内接触过（以"接通"刷新）
        picked.append(s["id"])
    return picked


class TestSampleFilter:
    def _pool(self):
        return [
            dict(id="A", status="IDLE", last_connected_at=None, attempts=0, shuffle_key=5),
            dict(id="B", status="SUCCESS", last_connected_at=NOW - timedelta(days=30), attempts=1, shuffle_key=1),  # 半年原则内（被接通过）
            dict(id="C", status="IDLE", last_connected_at=None, attempts=1, shuffle_key=2),                          # 呼过但未接通：不刷新半年原则→可派
            dict(id="D", status="IDLE", last_connected_at=None, attempts=3, shuffle_key=3),                          # 重拨超限
            dict(id="E", status="IDLE", last_connected_at=None, attempts=0, shuffle_key=4),                           # 黑名单
            dict(id="F", status="IDLE", last_connected_at=NOW - timedelta(days=200), attempts=2, shuffle_key=0),     # 超半年可再访
        ]

    def test_halfyear_rule_blocks_recent_connected(self):
        """P0：B 在 180 天内被接通过 → 不派；C 仅未接通 → 可派（接通才刷新原则）"""
        got = pick_samples(self._pool(), blacklist=set(), n=10)
        assert "B" not in got and "C" in got

    def test_blacklist_excluded(self):
        """P0：黑名单 E 不派"""
        got = pick_samples(self._pool(), blacklist={"E"}, n=10)
        assert "E" not in got

    def test_redial_max_excluded(self):
        """P1：重拨已达上限 D 不派"""
        got = pick_samples(self._pool(), blacklist=set(), n=10)
        assert "D" not in got

    def test_shuffle_order_and_limit(self):
        """P1：按打散键排序取数（F shuffle=0 最先），且限量生效"""
        got = pick_samples(self._pool(), blacklist={"E"}, n=2)
        assert got[0] == "F" and len(got) == 2

    def test_pool_exhausted(self):
        """P2：全过滤后返回空，派样引擎进入'样本池见底'分支（触发 M08.3 报警）"""
        pool = [dict(id="X", status="SUCCESS", last_connected_at=NOW, attempts=0, shuffle_key=1)]
        assert pick_samples(pool, set(), n=1) == []


# ----------------------------------------------------------------------------
# 4.2.3 结果码 → 样本去向（与 smp_status_code 初始化码表一致）
# ----------------------------------------------------------------------------
RESULT_CODES = {  # code: (closed, allow_redial, hit_black)
    "SUCCESS":  (True,  False, False),
    "PARTIAL":  (True,  True,  False),
    "QUFAIL":   (True,  False, False),
    "REFUSE":   (True,  False, True),
    "BREAKOFF": (True,  True,  False),
    "APPOINT":  (False, False, False),
    "NA":       (False, True,  False),
    "BUSY":     (False, True,  False),
    "INVALID":  (True,  False, True),
    "FAX":      (True,  False, False),
}

def sample_destination(code, overflow=False):
    closed, redial, black = RESULT_CODES[code]
    if black:
        return "BANNED"
    if code == "APPOINT":
        return "APPOINT_QUEUE"
    if code == "SUCCESS":
        return "CLOSED_SUCCESS"
    if closed:
        return "CLOSED_" + code
    if redial:
        return "REDIAL_POOL"
    return "REVIEW"


class TestResultDestination:
    @pytest.mark.parametrize("code,dest", [
        ("SUCCESS", "CLOSED_SUCCESS"),
        ("NA", "REDIAL_POOL"),
        ("BUSY", "REDIAL_POOL"),
        ("REFUSE", "BANNED"),
        ("INVALID", "BANNED"),
        ("APPOINT", "APPOINT_QUEUE"),
        ("QUFAIL", "CLOSED_QUFAIL"),
        ("BREAKOFF", "CLOSED_BREAKOFF"),
    ])
    def test_destinations(self, code, dest):
        assert sample_destination(code) == dest

    def test_quota_full_not_success(self):
        """配额满格被拒的答卷不得计入成功样本（回 QUFAIL/审阅分支）"""
        assert sample_destination("QUFAIL") != "CLOSED_SUCCESS"


# ----------------------------------------------------------------------------
# 半年原则参数热生效（M15.1 / F1501）
# ----------------------------------------------------------------------------
class TestParamHotReload:
    def test_param_change_applies(self):
        """P1：halfyear.days 由 180 改 90 后，120 天前接触的样本应可派"""
        global HALFYEAR_DAYS
        pool = [dict(id="Y", status="IDLE", last_connected_at=NOW - timedelta(days=120), attempts=0, shuffle_key=1)]
        assert pick_samples(pool, set()) == []          # 180 天下禁派
        HALFYEAR_DAYS = 90                               # 模拟参数热更新
        try:
            assert pick_samples(pool, set()) == ["Y"]   # 90 天下可派
        finally:
            HALFYEAR_DAYS = 180
