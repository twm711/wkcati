# -*- coding: utf-8 -*-
"""NK3C Demo 集成测试：完整走通 派样→作答→结果码→配额→审核→统计 闭环

注意：用例共享同一 DB（session 级），按文件顺序组成一条确定性链：
  ①登录/鉴权 → ②首派101+全流程(配额扣减/统计) → ③幂等 → ④过滤规则排空 →
  ⑤注入新样本验证配额不超发 → ⑥审核 → ⑦驳回重访 → ⑧INVALID自动入黑名单 → ⑨监控墙
"""
import json
import os
import sqlite3

import pytest

os.environ.setdefault("NK3C_DEMO_TEST", "1")

from fastapi.testclient import TestClient  # noqa: E402

import app as appmod  # noqa: E402


def _db():
    conn = sqlite3.connect(appmod.DB_PATH)
    return conn


def _insert_sample(conn, sid, name, gender, shuffle_key, attempts=0, phone=None):
    conn.execute(
        "INSERT INTO smp_sample(id, project_id, cust_name, gender, status, attempts, shuffle_key)"
        " VALUES(?,?,?,?,?,?,?)",
        (sid, 1, name, gender, "IDLE", attempts, shuffle_key))
    conn.execute("INSERT INTO smp_phone(id, sample_id, phone_no, sort_no, valid_flag) VALUES(?,?,?,?,1)",
                 (sid, sid, phone or f"138{sid:08d}", 1))
    conn.commit()


def _full_call(client, auth, expect_code="SUCCESS", gender_opt=111):
    """一条完整话务：派样 → 答2题 → 提交结果码。返回 (dispatch_data, result_data)"""
    d = client.get("/api/agent/dispatch", headers=auth).json()
    assert d["success"] and d["data"], f"派样失败: {d}"
    client.post("/api/agent/answer", headers=auth,
                json={"callId": d["data"]["callId"], "questionId": 11, "optionIds": [gender_opt]})
    client.post("/api/agent/answer", headers=auth,
                json={"callId": d["data"]["callId"], "questionId": 12, "numericValue": 8})
    res = client.post("/api/agent/result", headers=auth,
                      json={"callId": d["data"]["callId"], "resultCode": expect_code}).json()
    assert res["success"], res
    return d["data"], res["data"]


@pytest.fixture(scope="session")
def client():
    appmod.init_db(force=True)  # 每次测试会话重置为种子数据
    with TestClient(appmod.app) as c:
        yield c


@pytest.fixture(scope="session")
def auth(client):
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    assert r["success"] and r["data"]["roles"] == ["phoneAdmin"]
    return {"Authorization": "Bearer " + r["data"]["sessionId"]}


@pytest.fixture(scope="session")
def sup_auth(client):
    r = client.post("/api/auth/login", json={"loginName": "sup01", "password": "123456"}).json()
    assert r["success"]
    return {"Authorization": "Bearer " + r["data"]["sessionId"]}


# ── ① 登录与鉴权 ──
def test_01_login_bad_password(client):
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "wrong"}).json()
    assert r["success"] is False and r["code"] == "4003"


def test_02_dispatch_requires_auth(client):
    assert client.get("/api/agent/dispatch").status_code == 401


# ── ② 首派 + 全流程（E2E 4.6 子集） ──
def test_03_full_success_flow_first_dispatch(client, auth):
    """P0：首派 101（shuffle 顺序）；SUCCESS → 样本闭环+答卷SUBMITTED+配额扣减+单题统计"""
    d = client.get("/api/agent/dispatch", headers=auth).json()["data"]
    assert d["sampleId"] == 101
    assert d["questionnaire"]["version"] == "v1.0" and len(d["questionnaire"]["questions"]) == 2
    r1 = client.post("/api/agent/answer", headers=auth,
                     json={"callId": d["callId"], "questionId": 11, "optionIds": [111]}).json()
    assert r1["success"] and r1["data"]["sheetId"] > 0
    client.post("/api/agent/answer", headers=auth,
                json={"callId": d["callId"], "questionId": 12, "numericValue": 8})
    res = client.post("/api/agent/result", headers=auth,
                      json={"callId": d["callId"], "resultCode": "SUCCESS"}).json()
    assert res["data"]["destination"] == "CLOSED_SUCCESS"
    assert res["data"]["sheetStatus"] == "SUBMITTED"
    assert res["data"]["quotaConsume"] == "CONSUMED"
    conn = _db()
    st = conn.execute("SELECT status FROM smp_sample WHERE id=101").fetchone()[0]
    cell_done = conn.execute("SELECT done_count FROM qnr_quota_cell WHERE id=11").fetchone()[0]
    conn.close()
    assert st == "SUCCESS" and cell_done == 1
    rep = client.get("/api/report/single", params={"questionId": 11}, headers=auth).json()
    assert rep["data"]["total"] == 1
    men = next(i for i in rep["data"]["items"] if i["optionId"] == 111)
    assert men["count"] == 1 and men["pct"] == 100.0


# ── ③ 幂等 ──
def test_04_result_idempotent(client, auth):
    """P0（4.4#6）：重复提交结果码 → 返回首写结果，统计不重复计数"""
    d, _ = _full_call(client, auth, gender_opt=112)  # 106 为女
    second = client.post("/api/agent/result", headers=auth,
                         json={"callId": d["callId"], "resultCode": "SUCCESS"}).json()
    assert second["success"] and second["data"]["duplicate"] is True
    assert second["data"]["firstResultCode"] == "SUCCESS"
    rep = client.get("/api/report/single", params={"questionId": 11}, headers=auth).json()
    women = next(i for i in rep["data"]["items"] if i["optionId"] == 112)
    assert women["count"] == 1


# ── ④ 过滤规则排空 ──
def test_05_pool_exhausted_by_rules(client, auth):
    """P0：派样循环到空 → 仅 101/106 出现（102闭环/103黑名单/104半年/105重拨上限被过滤）"""
    got = {101, 106}
    while True:
        r = client.get("/api/agent/dispatch", headers=auth).json()
        if not r["data"]:
            assert "无可用样本" in r["message"]
            break
        assert r["data"]["sampleId"] not in {102, 103, 104, 105}, "过滤规则失效"
        # 直接闭环，避免悬挂样本
        client.post("/api/agent/result", headers=auth,
                    json={"callId": r["data"]["callId"], "resultCode": "SUCCESS"})
        got.add(r["data"]["sampleId"])


# ── ⑤ 配额原子不超发（4.4#2） ──
def test_06_quota_atomic_not_exceed(client, auth):
    """P0：注入（余量+1）个男样本 → 恰好余量个 CONSUMED、最后 1 个 QUOTA_FULL、done 永不超 target"""
    conn = _db()
    target = conn.execute("SELECT target_count FROM qnr_quota_cell WHERE id=11").fetchone()[0]
    done_before = conn.execute("SELECT done_count FROM qnr_quota_cell WHERE id=11").fetchone()[0]
    remaining = target - done_before
    for i in range(remaining + 1):
        _insert_sample(conn, 400 + i, f"压测男{i}", "男", 40 + i, phone=f"13800000{400 + i}")
    conn.close()
    consumed, full_seen = 0, False
    for _ in range(remaining + 1):
        _, res = _full_call(client, auth, gender_opt=111)
        qc = res.get("quotaConsume") or ""
        if qc == "CONSUMED":
            consumed += 1
        elif qc.startswith("QUOTA_FULL"):
            full_seen = True
    conn = _db()
    done_after = conn.execute("SELECT done_count FROM qnr_quota_cell WHERE id=11").fetchone()[0]
    conn.close()
    assert done_after == target, f"配额超发：done={done_after} > target={target}"
    assert consumed == remaining and full_seen


# ── ⑥ 审核 ──
def test_07_audit_flow(client, sup_auth):
    """P1：SUBMITTED → PASS=AUDITED，审核后统计仍计入"""
    sheets = client.get("/api/sheet", params={"status": "SUBMITTED"}, headers=sup_auth).json()["data"]["rows"]
    assert sheets
    sid = sheets[0]["id"]
    r = client.post(f"/api/sheet/{sid}/audit", headers=sup_auth,
                    json={"action": "PASS", "remark": "录音比对无误"}).json()
    assert r["success"] and r["data"]["status"] == "AUDITED"
    rep = client.get("/api/report/single", params={"questionId": 11}, headers=sup_auth).json()
    assert rep["data"]["total"] >= 1


# ── ⑦ 驳回重访 ──
def test_08_reject_creates_revisit(client, sup_auth, auth):
    """P1：驳回 → 答卷 REJECTED 且样本回 IDLE（可重访）"""
    conn = _db()
    _insert_sample(conn, 301, "驳回测试", "男", 20, phone="13800000301")
    conn.close()
    d, _ = _full_call(client, auth, gender_opt=111)
    sample_id = d["sampleId"]
    sheets = client.get("/api/sheet", params={"status": "SUBMITTED"}, headers=sup_auth).json()["data"]["rows"]
    sid = next(s["id"] for s in sheets if s["sample_id"] == sample_id)
    r = client.post(f"/api/sheet/{sid}/audit", headers=sup_auth,
                    json={"action": "REJECT", "remark": "关键题漏问"}).json()
    assert r["success"] and r["data"]["status"] == "REJECTED"
    conn = _db()
    st = conn.execute("SELECT status FROM smp_sample WHERE id=?", (sample_id,)).fetchone()[0]
    conn.close()
    assert st == "IDLE"


# ── ⑧ INVALID 自动入黑名单 ──
def test_09_invalid_auto_blacklist(client, auth):
    """P1：INVALID 结果码 → 号码自动写入 smp_blacklist（hit_black_flag 生效）"""
    conn = _db()
    _insert_sample(conn, 302, "空号测试", "男", 21, phone="13800000302")
    conn.close()
    d = client.get("/api/agent/dispatch", headers=auth).json()["data"]
    phone = d["currentPhone"]
    r = client.post("/api/agent/result", headers=auth,
                    json={"callId": d["callId"], "resultCode": "INVALID"}).json()
    assert r["success"] and r["data"]["destination"] == "BANNED"
    conn = _db()
    hit = conn.execute("SELECT 1 FROM smp_blacklist WHERE phone_no=?", (phone,)).fetchone()
    conn.close()
    assert hit


# ── ⑨ 监控墙 ──
def test_10_monitor_wall_summary(client, sup_auth):
    """P1：监控墙返回坐席与汇总，成功数 ≥1 且 拨打数 ≥ 成功数"""
    r = client.get("/api/monitor/wall", headers=sup_auth).json()
    assert r["success"]
    assert any(a["agentNo"] == "1020" for a in r["data"]["agents"])
    s = r["data"]["summary"]
    assert s["successCount"] >= 1 and s["dialCount"] >= s["successCount"]


# ── ⑩ 督导台页面与话务流水 ──
def test_11_supervisor_page_served(client):
    """P1：GET /supervisor 返回 Live 页面（含 API 调用），GET / 返回控制台首页"""
    r = client.get("/supervisor")
    assert r.status_code == 200 and "api/auth/login" in r.text
    r2 = client.get("/")
    assert r2.status_code == 200 and "督导控制台" in r2.text and "坐席工作台" in r2.text


def test_12_monitor_calls(client, sup_auth):
    """P1：话务流水返回列表，且包含此前用例产生的 SUCCESS 话务"""
    r = client.get("/api/monitor/calls?limit=20", headers=sup_auth).json()
    assert r["success"] and isinstance(r["data"], list) and len(r["data"]) >= 1
    assert any(c["result_code"] == "SUCCESS" for c in r["data"])
    assert all(set(c) >= {"id", "sample_id", "agent_no", "status", "result_code"} for c in r["data"])


# ── ⑪ WebSocket 实时推送 ──
def test_13_ws_monitor(client):
    """P1：督导 WS 连接即收监控墙快照（含 1020）；坏 token 被拒（4401）"""
    r = client.post("/api/auth/login", json={"loginName": "sup01", "password": "123456"}).json()
    assert r["success"]
    with client.websocket_connect("/ws/monitor?token=" + r["data"]["sessionId"]) as ws:
        msg = ws.receive_json()
        assert msg["type"] == "wall"
        assert any(a["agentNo"] == "1020" for a in msg["data"]["agents"])
        assert msg["data"]["summary"]["successCount"] >= 1
    rejected = False
    try:
        with client.websocket_connect("/ws/monitor?token=badtoken") as ws:
            ws.receive_json()
    except Exception:
        rejected = True
    assert rejected


# ── ⑫ 导出中心 ──
def test_14_export(client, sup_auth, auth):
    """P1：答卷明细 CSV/XLSX、统计报表 CSV 可下载且含数据；坐席无导出权限（4032）"""
    r = client.get("/api/export/answers?format=csv", headers=sup_auth)
    assert r.status_code == 200 and "text/csv" in r.headers["content-type"]
    assert "答卷ID" in r.text and "AUDITED" in r.text and "v1.0" in r.text  # test_06 已审答卷
    rx = client.get("/api/export/answers?format=xlsx", headers=sup_auth)
    assert rx.status_code == 200 and rx.content[:2] == b"PK"  # xlsx 魔数
    rs = client.get("/api/export/stats?format=csv", headers=sup_auth)
    assert rs.status_code == 200 and "选项/指标" in rs.text and "平均分" in rs.text
    forbidden = client.get("/api/export/answers", headers=auth).json()
    assert not forbidden["success"] and forbidden["code"] == "4032"


# ── ⑬ IVR 流程（设计器保存校验）──
def test_15_ivr_flow(client, sup_auth, auth):
    """P1：GET 流程含种子节点；坏流程被拒（entry 缺失）；坐席无保存权限；合法修改可保存"""
    r = client.get("/api/ivr/flow", headers=auth).json()
    assert r["success"] and r["data"]["flow"]["entry"] == "welcome"
    assert {n["id"] for n in r["data"]["flow"]["nodes"]} >= {"menu", "q1", "q2", "vm", "transfer", "bye"}
    original = r["data"]["flow"]
    bad = json.loads(json.dumps(original))
    bad["entry"] = "nope"
    rb = client.put("/api/ivr/flow", headers=sup_auth, json={"name": "x", "flow": bad}).json()
    assert not rb["success"] and "entry" in rb["message"]
    rp = client.put("/api/ivr/flow", headers=auth, json={"name": "x", "flow": original}).json()
    assert not rp["success"] and rp["code"] == "4032"  # 坐席无权保存
    mod = json.loads(json.dumps(original))
    mod["nodes"].append({"id": "vip", "type": "play", "text": "尊贵的VIP客户您好", "next": "menu"})
    mod["nodes"][1]["branches"]["9"] = "vip"  # 菜单加 9 键分支
    rok = client.put("/api/ivr/flow", headers=sup_auth, json={"name": "VIP流程", "flow": mod}).json()
    assert rok["success"]
    r2 = client.get("/api/ivr/flow", headers=auth).json()
    assert r2["data"]["name"] == "VIP流程" and "9" in r2["data"]["flow"]["nodes"][1]["branches"]
    # 还原种子流程（避免污染后续用例）
    rbk = client.put("/api/ivr/flow", headers=sup_auth, json={"name": "默认呼入流程", "flow": original}).json()
    assert rbk["success"]


# ── ⑭ IVR 呼入走线 ──
def test_16_ivr_call_walk(client, auth):
    """P1：呼入自动放欢迎音→菜单；无效键重播；1→性别(2=女)→打分(7)→结束落库 SURVEY；转人工/放弃分支"""
    r = client.post("/api/ivr/call", headers=auth, json={}).json()
    assert r["success"] and r["data"]["node"]["id"] == "menu"  # welcome 自动放完
    sid = r["data"]["sessionId"]
    r1 = client.post(f"/api/ivr/call/{sid}/input", headers=auth, json={"key": "9"}).json()
    assert r1["success"] and r1["data"]["invalidKey"]  # 无效键重播
    r2 = client.post(f"/api/ivr/call/{sid}/input", headers=auth, json={"key": "1"}).json()
    assert r2["success"] and r2["data"]["node"]["id"] == "q1"
    r3 = client.post(f"/api/ivr/call/{sid}/input", headers=auth, json={"key": "2"}).json()
    assert r3["data"]["node"]["id"] == "q2" and r3["data"]["answers"]["Q11"] == "女"
    r4 = client.post(f"/api/ivr/call/{sid}/input", headers=auth, json={"key": "7"}).json()
    assert r4["data"]["done"] and r4["data"]["outcome"] == "SURVEY" and r4["data"]["answers"]["Q12"] == "7"
    # 转人工分支：menu 按 0 → transfer 自动流转到 bye
    rc = client.post("/api/ivr/call", headers=auth, json={"callerNo": "13900001111"}).json()
    rt = client.post(f"/api/ivr/call/{rc['data']['sessionId']}/input", headers=auth, json={"key": "0"}).json()
    assert rt["data"]["done"] and rt["data"]["outcome"] == "TRANSFER:MANUAL"
    # 主叫中途挂断 → ABANDONED 落库
    rh0 = client.post("/api/ivr/call", headers=auth, json={}).json()
    rh = client.post(f"/api/ivr/call/{rh0['data']['sessionId']}/hangup", headers=auth, json={}).json()
    assert rh["data"]["outcome"] == "ABANDONED"
    rl = client.get("/api/ivr/logs?limit=10", headers=auth).json()
    outcomes = [x["outcome"] for x in rl["data"]]
    assert "SURVEY" in outcomes and "TRANSFER:MANUAL" in outcomes and "ABANDONED" in outcomes


# ── ⑮ 实时质检（S5：监听/插话/示忙/示闲/强挂/签出）──
def test_17_qc_push(client, sup_auth, auth):
    """P1：坐席无质检权限（4032）；督导 LISTEN → 坐席 WS 实时收到 qc 事件；坏工号/坏 action"""
    forb = client.post("/api/monitor/qc", headers=auth,
                       json={"agentNo": "1020", "action": "LISTEN"}).json()
    assert not forb["success"] and forb["code"] == "4032"
    with client.websocket_connect("/ws/agent?token=" + auth["Authorization"].split()[1]) as ws:
        push = client.post("/api/monitor/qc", headers=sup_auth,
                           json={"agentNo": "1020", "action": "LISTEN", "note": "新人质检"}).json()
        assert push["success"] and push["data"]["actionName"] == "监听"
        msg = ws.receive_json()
        assert msg["type"] == "qc"
        assert msg["data"]["action"] == "LISTEN" and msg["data"]["by"] == "督导演示" and msg["data"]["note"] == "新人质检"
    nf = client.post("/api/monitor/qc", headers=sup_auth,
                     json={"agentNo": "9999", "action": "LISTEN"}).json()
    assert not nf["success"] and nf["code"] == "4044"
    bad = client.post("/api/monitor/qc", headers=sup_auth,
                      json={"agentNo": "1020", "action": "KICK"}).json()
    assert not bad["success"] and bad["code"] == "4004"


# ── ⑯ 强制签出 ──
def test_18_force_checkout(client, sup_auth):
    """P1：FORCECHECKOUT → 目标坐席全部会话失效（后续请求 401），WS 收到事件"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    atok = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    assert client.get("/api/agent/dispatch", headers=atok).json()["success"]
    with client.websocket_connect("/ws/agent?token=" + atok["Authorization"].split()[1]) as ws:
        push = client.post("/api/monitor/qc", headers=sup_auth,
                           json={"agentNo": "1020", "action": "FORCECHECKOUT", "note": "质检违规"}).json()
        assert push["success"]
        msg = ws.receive_json()
        assert msg["data"]["action"] == "FORCECHECKOUT" and msg["data"]["note"] == "质检违规"
    assert client.get("/api/agent/dispatch", headers=atok).status_code == 401  # 会话已被踢



def test_19_recording_replay(client, sup_auth, auth):
    """P1：答卷明细含录音地标（offset 与音轨一致）；WAV 真实可播（RIFF 魔数）；
    坐席无录音权限（403）；话务不存在（404）"""
    sheets = client.get("/api/sheet", headers=sup_auth).json()["data"]["rows"]
    sheet = sheets[0]
    d = client.get(f"/api/sheet/{sheet['id']}/answers", headers=sup_auth).json()
    assert d["success"]
    assert d["data"]["sheet"]["call_id"] == sheet["call_id"]
    assert len(d["data"]["answers"]) >= 1 and d["data"]["answers"][0]["title"]
    cues = d["data"]["cues"]
    assert len(cues) == len(d["data"]["answers"]) and cues[0]["offset"] == 0.8
    assert d["data"]["duration"] >= 10
    r = client.get(f"/api/qc/recording/{sheet['call_id']}?token={sup_auth['Authorization'].split()[1]}")
    assert r.status_code == 200 and r.headers["content-type"] == "audio/wav"
    assert r.content[:4] == b"RIFF" and len(r.content) > 80000  # ≥10s × 8kHz
    forb = client.get(f"/api/qc/recording/{sheet['call_id']}?token={auth['Authorization'].split()[1]}")
    assert forb.status_code == 403  # 坐席无录音回放权限
    nf = client.get(f"/api/qc/recording/99999?token={sup_auth['Authorization'].split()[1]}")
    assert nf.status_code == 404


# ── ⑱ 答案展示值（数值/选项/文本三态）──
def test_20_answer_display(client, sup_auth, auth):
    """P1：明细接口对数值题返回数值文本（'9'），选项题返回选项文本，不出现空串"""
    conn = _db()  # 链末样本池已排空，注入专属样本
    _insert_sample(conn, 901901, "答案展示", "男", 901901)
    conn.close()
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    assert r["success"]  # test_18 曾强制签出，需重新登录
    auth = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    d = client.get("/api/agent/dispatch", headers=auth).json()["data"]
    assert d
    client.post("/api/agent/answer", headers=auth,
                json={"callId": d["callId"], "questionId": 11, "optionIds": [111]})
    client.post("/api/agent/answer", headers=auth,
                json={"callId": d["callId"], "questionId": 12, "numericValue": 9})
    client.post("/api/agent/result", headers=auth,
                json={"callId": d["callId"], "resultCode": "SUCCESS"})
    sheets = client.get("/api/sheet", headers=sup_auth).json()["data"]["rows"]
    sheet = next(s for s in sheets if s["call_id"] == d["callId"])
    detail = client.get(f"/api/sheet/{sheet['id']}/answers", headers=sup_auth).json()["data"]
    vals = {a["questionId"]: a["value"] for a in detail["answers"]}
    assert vals[11] == "男" and vals[12] == "9.0" or vals[12] == "9"
    assert all(v != "" for v in vals.values())


# ── ⑲ 多项目：种子项目2 派样/跨项目守卫/独立配额 ──
def test_21_project2_walk(client, sup_auth, auth):
    """P1：项目2 派样 201（203黑名单/202闭环被过滤）；跨项目作答被拒；Q21 配额独立扣减；导出含 Q21 列"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    assert r["success"]  # test_18 曾强制签出，重新登录
    auth = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    d = client.get("/api/agent/dispatch?projectId=2", headers=auth).json()
    assert d["success"] and d["data"]["sampleId"] == 201, d
    qids = [q["questionId"] for q in d["data"]["questionnaire"]["questions"]]
    assert qids == [21, 22]  # 问卷随项目下发
    cross = client.post("/api/agent/answer", headers=auth,
                        json={"callId": d["data"]["callId"], "questionId": 11, "optionIds": [111]}).json()
    assert not cross["success"] and cross["code"] == "4004"  # 项目1 的题不能答项目2 的话务
    a1 = client.post("/api/agent/answer", headers=auth,
                     json={"callId": d["data"]["callId"], "questionId": 21, "optionIds": [211]}).json()
    a2 = client.post("/api/agent/answer", headers=auth,
                     json={"callId": d["data"]["callId"], "questionId": 22, "optionIds": [221]}).json()
    assert a1["success"] and a2["success"]
    r = client.post("/api/agent/result", headers=auth,
                    json={"callId": d["data"]["callId"], "resultCode": "SUCCESS"}).json()
    assert r["success"] and r["data"]["quotaConsume"] == "CONSUMED"
    conn = _db()
    assert conn.execute("SELECT done_count FROM qnr_quota_cell WHERE id=21").fetchone()[0] == 1
    conn.close()
    rep = client.get("/api/report/single?questionId=21", headers=sup_auth).json()
    cells = {c["cellId"]: c for c in rep["data"]["quota"]}
    assert cells[21]["done"] == 1 and cells[22]["done"] == 0 and cells[23]["target"] == 1
    ex = client.get("/api/export/answers?format=csv", headers=sup_auth)
    assert "项目" in ex.text and "Q21" in ex.text and "产品偏好调查_2026" in ex.text


# ── ⑳ 多项目：生命周期 创建→加题→配额→发布→导样→派样 ──
def test_22_project_lifecycle(client, sup_auth, auth):
    """P1：坐席无创建权限；DRAFT 未发布不可派样；发布后按新问卷派样成功"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    assert r["success"]
    auth = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    forb = client.post("/api/project", headers=auth, json={"name": "坐席偷建"}).json()
    assert not forb["success"] and forb["code"] == "4032"
    p = client.post("/api/project", headers=sup_auth, json={"name": "客户回访_测试"}).json()["data"]
    pid = p["projectId"]
    empty = client.post(f"/api/project/{pid}/publish", headers=sup_auth).json()
    assert not empty["success"] and "至少" in empty["message"]  # 无题不可发布
    q1 = client.post(f"/api/project/{pid}/questions", headers=sup_auth,
                     json={"text": "您是否满意本次服务？", "qType": "single",
                           "options": ["满意", "一般", "不满意"]}).json()["data"]["questionId"]
    q2r = client.post(f"/api/project/{pid}/questions", headers=sup_auth,
                      json={"text": "通话质量打分", "qType": "number", "min": 0, "max": 10}).json()
    assert q2r["success"]
    q2 = q2r["data"]["questionId"]
    qs = client.get("/api/agent/dispatch?projectId=" + str(pid), headers=auth).json()
    assert not qs["success"] and qs["code"] == "4031"  # DRAFT 不可派样
    setq = client.put(f"/api/project/{pid}/quota", headers=sup_auth,
                      json={"name": "满意度配额", "cells": [{"questionId": q1, "inList": [], "target": 1}]}).json()
    assert not setq["success"]  # 选项列表为空被拒
    opts = client.get("/api/agent/dispatch?projectId=1", headers=auth)  # 任意调用拿不到选项，走库查
    conn = _db()
    oids = [r[0] for r in conn.execute("SELECT id FROM qnr_option WHERE question_id=?", (q1,))]
    conn.close()
    ok_q = client.put(f"/api/project/{pid}/quota", headers=sup_auth,
                      json={"name": "满意度配额",
                            "cells": [{"questionId": q1, "inList": oids[:1], "target": 5}]}).json()
    assert ok_q["success"]
    pub = client.post(f"/api/project/{pid}/publish", headers=sup_auth).json()
    assert pub["success"] and pub["data"]["questions"] == 2
    imp = client.post(f"/api/project/{pid}/samples", headers=sup_auth,
                      json={"names": ["测试甲", "测试乙", "测试丙"]}).json()
    assert imp["success"] and imp["data"]["count"] == 3
    d = client.get(f"/api/agent/dispatch?projectId={pid}", headers=auth).json()
    assert d["success"] and d["data"]["sampleId"] == imp["data"]["sampleIds"][0]
    assert [q["questionId"] for q in d["data"]["questionnaire"]["questions"]] == [q1, q2]
    dup = client.post(f"/api/project/{pid}/questions", headers=sup_auth,
                      json={"text": "发布后再加题", "qType": "text"}).json()
    assert not dup["success"] and dup["code"] == "4031"  # 发布后问卷冻结
    pl = client.get("/api/project", headers=auth).json()["data"]
    mine = next(x for x in pl if x["projectId"] == pid)
    assert mine["status"] == "RUNNING" and mine["samples"]["total"] == 3 and mine["quota"]["target"] == 5


# ── ㉒ 工单：IVR 转人工自动落单 ──
def test_23_workorder_from_ivr(client, auth):
    """P0 行96（前半）：呼入按 0 转人工 → 自动生成 HIGH 工单（PENDING，含通话轨迹）"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    assert r["success"]
    auth = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    rc = client.post("/api/ivr/call", headers=auth, json={"callerNo": "13977776666"}).json()
    rt = client.post(f"/api/ivr/call/{rc['data']['sessionId']}/input", headers=auth, json={"key": "0"}).json()
    assert rt["data"]["done"] and rt["data"]["outcome"] == "TRANSFER:MANUAL"
    wl = client.get("/api/workorder", headers=auth).json()
    assert wl["success"] and wl["data"]["total"] >= 1
    w = wl["data"]["rows"][0]
    assert w["caller_no"] == "13977776666" and w["status"] == "PENDING"
    assert w["priority"] == "HIGH" and "transfer" in w["detail"]


# ── ㉓ 工单：受理→办结→归档 状态机 ──
def test_24_workorder_flow(client, sup_auth, auth):
    """P0 F1303（demo 级）：PENDING→ACCEPTED(认领)→RESOLVED(备注)→CLOSED(督导)；
    非法跳转被拒（4031）；坐席归档被拒（4032）"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    auth = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    wl = client.get("/api/workorder?status=PENDING", headers=auth).json()["data"]
    assert wl["rows"], "test_23 应已落单"
    tid = wl["rows"][0]["id"]
    skip = client.post(f"/api/workorder/{tid}/resolve", headers=auth, json={"remark": "x"}).json()
    assert not skip["success"] and skip["code"] == "4031"  # PENDING 不能直接办结
    a = client.post(f"/api/workorder/{tid}/accept", headers=auth).json()
    assert a["success"] and a["data"]["status"] == "ACCEPTED"
    dup = client.post(f"/api/workorder/{tid}/accept", headers=auth).json()
    assert not dup["success"]  # 重复受理被拒
    rs = client.post(f"/api/workorder/{tid}/resolve", headers=auth, json={"remark": "已回电解决"}).json()
    assert rs["success"] and rs["data"]["status"] == "RESOLVED"
    cl = client.post(f"/api/workorder/{tid}/close", headers=auth, json={}).json()
    assert not cl["success"] and cl["code"] == "4032"  # 坐席不能归档
    cl2 = client.post(f"/api/workorder/{tid}/close", headers=sup_auth, json={}).json()
    assert cl2["success"] and cl2["data"]["status"] == "CLOSED"
    conn = _db()  # wko_ticket 列序: 9 remark / 11 accepted_at / 12 resolved_at / 13 closed_at
    row = conn.execute("SELECT remark, accepted_at, resolved_at, closed_at FROM wko_ticket WHERE id=?", (tid,)).fetchone()
    conn.close()
    assert row[1] and row[2] and row[3] and row[0] == "已回电解决"


# ── ㉔ P0 F0704 断线续答不丢题（同题重复提交=更新）──
def test_25_resume_upsert(client, sup_auth, auth):
    """P0 F0704：派样→答Q11(男)→答Q12→改答Q11(女)→提交：答卷仅按最新值，答案不重复行"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    auth = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    conn = _db()
    _insert_sample(conn, 902201, "续答客户", "男", 902201)
    conn.close()
    rep0 = client.get("/api/report/single?questionId=11", headers=sup_auth).json()["data"]
    male0 = next(i for i in rep0["items"] if i["optionId"] == 111)["count"]
    d = client.get("/api/agent/dispatch", headers=auth).json()["data"]
    assert d
    a1 = client.post("/api/agent/answer", headers=auth,
                     json={"callId": d["callId"], "questionId": 11, "optionIds": [111]}).json()
    sheet_id = a1["data"]["sheetId"]
    client.post("/api/agent/answer", headers=auth,
                json={"callId": d["callId"], "questionId": 12, "numericValue": 5})
    client.post("/api/agent/answer", headers=auth,
                json={"callId": d["callId"], "questionId": 11, "optionIds": [112]})  # 改答
    conn = _db()
    rows = conn.execute("SELECT option_ids FROM ans_answer WHERE sheet_id=? AND question_id=11",
                        (sheet_id,)).fetchall()
    conn.close()
    assert len(rows) == 1 and json.loads(rows[0][0]) == [112]  # upsert 覆盖，不重复
    res = client.post("/api/agent/result", headers=auth,
                      json={"callId": d["callId"], "resultCode": "SUCCESS"}).json()
    assert res["success"]
    rep = client.get("/api/report/single?questionId=11", headers=sup_auth).json()["data"]
    male = next(i for i in rep["items"] if i["optionId"] == 111)["count"]
    female = next(i for i in rep["items"] if i["optionId"] == 112)["count"]
    assert male == male0  # 旧值（男）未计入统计
    assert female >= 1


# ── ㉕ P0 登录失败分支 ──
def test_26_login_failure(client):
    """P0：错误密码登录被拒（success=false），且不产生会话"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "wrong"}).json()
    assert not r["success"] and r["data"] is None


# ── ㉖ 多问卷版本化修订（P0 行24：版本+0.1，旧答卷保留快照）──
def test_27_questionnaire_versioning(client, sup_auth, auth):
    """P0：RUNNING 项目修订问卷 → v1.0→v1.1；已答答卷快照仍 v1.0；新话务按 v1.1 执行；detail 端点聚合正确"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    auth = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    pid = client.post("/api/project", headers=sup_auth, json={"name": "版本化项目"}).json()["data"]["projectId"]
    q1 = client.post(f"/api/project/{pid}/questions", headers=sup_auth,
                     json={"text": "首题（v1.0）", "qType": "single", "options": ["是", "否"]}).json()["data"]["questionId"]
    assert client.post(f"/api/project/{pid}/publish", headers=sup_auth).json()["success"]
    assert client.post(f"/api/project/{pid}/samples", headers=sup_auth,
                       json={"names": ["版本甲", "版本乙"]}).json()["success"]
    # v1.0 话务：答卷快照应为 v1.0
    d1 = client.get(f"/api/agent/dispatch?projectId={pid}", headers=auth).json()["data"]
    client.post("/api/agent/answer", headers=auth,
                json={"callId": d1["callId"], "questionId": q1, "optionIds": None, "answerText": "语音答：是"})
    client.post("/api/agent/result", headers=auth,
                json={"callId": d1["callId"], "resultCode": "SUCCESS"})
    # 修订：加第二题 → v1.1
    rv = client.post(f"/api/project/{pid}/revise", headers=sup_auth,
                     json={"addQuestions": [{"text": "补充题（v1.1）", "qType": "text"}]}).json()
    assert rv["success"] and rv["data"]["oldVersion"] == "v1.0" and rv["data"]["newVersion"] == "v1.1"
    q2 = rv["data"]["addedQuestionIds"][0]
    det = client.get(f"/api/project/{pid}", headers=sup_auth).json()["data"]
    assert det["qnrVersion"] == "v1.1" and len(det["questions"]) == 2
    assert det["sheetVersions"] == {"v1.0": 1}  # 已答答卷保留旧快照
    # 新话务：问卷含 2 题，答卷快照 v1.1
    d2 = client.get(f"/api/agent/dispatch?projectId={pid}", headers=auth).json()["data"]
    assert [q["questionId"] for q in d2["questionnaire"]["questions"]] == [q1, q2]
    client.post("/api/agent/answer", headers=auth,
                json={"callId": d2["callId"], "questionId": q1, "optionIds": [det["questions"][0]["options"][0]["optionId"]]})
    res = client.post("/api/agent/result", headers=auth,
                      json={"callId": d2["callId"], "resultCode": "NA"}).json()
    assert res["success"]
    sheets = client.get("/api/sheet", headers=sup_auth).json()["data"]["rows"]
    vers = {s["call_id"]: s["qnr_version"] for s in sheets}
    assert vers[d1["callId"]] == "v1.0" and vers[d2["callId"]] == "v1.1"


# ── ㉗ 项目状态机（pause/resume/finish + 非法跳转）──
def test_28_project_status_machine(client, sup_auth, auth):
    """P0 行12（demo 级）：RUNNING→PAUSED（派样拒）→RUNNING→FINISHED（终态，派样拒/再暂停拒/修订拒）"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    auth = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    pid = client.post("/api/project", headers=sup_auth, json={"name": "状态机项目"}).json()["data"]["projectId"]
    client.post(f"/api/project/{pid}/questions", headers=sup_auth,
                json={"text": "Q", "qType": "text"})
    client.post(f"/api/project/{pid}/publish", headers=sup_auth)
    client.post(f"/api/project/{pid}/samples", headers=sup_auth, json={"names": ["状态甲"]})
    pz = client.post(f"/api/project/{pid}/status", headers=sup_auth, json={"action": "pause"}).json()
    assert pz["success"] and pz["data"]["status"] == "PAUSED"
    d1 = client.get(f"/api/agent/dispatch?projectId={pid}", headers=auth).json()
    assert not d1["success"] and d1["code"] == "4031"  # 暂停期间不派样
    rs = client.post(f"/api/project/{pid}/status", headers=sup_auth, json={"action": "resume"}).json()
    assert rs["success"] and rs["data"]["status"] == "RUNNING"
    d2 = client.get(f"/api/agent/dispatch?projectId={pid}", headers=auth).json()
    assert d2["success"] and d2["data"]
    fn = client.post(f"/api/project/{pid}/status", headers=sup_auth, json={"action": "finish"}).json()
    assert fn["success"] and fn["data"]["status"] == "FINISHED"
    d3 = client.get(f"/api/agent/dispatch?projectId={pid}", headers=auth).json()
    assert not d3["success"]  # 终态不派样
    again = client.post(f"/api/project/{pid}/status", headers=sup_auth, json={"action": "pause"}).json()
    assert not again["success"] and again["code"] == "4031"  # FINISHED 不可再变更
    rv = client.post(f"/api/project/{pid}/revise", headers=sup_auth,
                     json={"addQuestions": [{"text": "x", "qType": "text"}]}).json()
    assert not rv["success"] and "RUNNING" in rv["message"]  # 终态不可修订


# ── ㉘ 样本导入同项目查重（P0 行29 demo 级）──
def test_29_import_dedup(client, sup_auth):
    """P0：显式号码重复 → 跳过并报告 skipped；detail 样本分状态统计正确"""
    pid = client.post("/api/project", headers=sup_auth, json={"name": "查重项目"}).json()["data"]["projectId"]
    r1 = client.post(f"/api/project/{pid}/samples", headers=sup_auth,
                     json={"names": ["查重甲", "查重乙"], "phones": ["13700000001", "13700000001"]}).json()
    assert r1["success"] and r1["data"]["count"] == 1 and r1["data"]["skipped"] == 1
    r2 = client.post(f"/api/project/{pid}/samples", headers=sup_auth,
                     json={"names": ["查重丙"], "phones": ["13700000001"]}).json()
    assert r2["data"]["count"] == 0 and r2["data"]["skipped"] == 1
    det = client.get(f"/api/project/{pid}", headers=sup_auth).json()["data"]
    assert det["samplesByStatus"].get("IDLE") == 1


# ── ㉙ 工单回访闭环 E2E（P0 行96 后半）──
def test_30_ticket_revisit_e2e(client, sup_auth, auth):
    """P0 行96：IVR转人工→工单→归档自动生成回访样本→外呼回访→答卷审核 AUDITED→详情端点双链路关联"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    assert r["success"]
    auth = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    # ① 呼入转人工落单
    rc = client.post("/api/ivr/call", headers=auth, json={"callerNo": "13966665555"}).json()
    rt = client.post(f"/api/ivr/call/{rc['data']['sessionId']}/input", headers=auth, json={"key": "0"}).json()
    assert rt["data"]["outcome"] == "TRANSFER:MANUAL"
    tid = client.get("/api/workorder?status=PENDING", headers=auth).json()["data"]["rows"][0]["id"]
    # ② 受理 → 办结 → 归档（自动生成回访样本）
    assert client.post(f"/api/workorder/{tid}/accept", headers=auth).json()["success"]
    assert client.post(f"/api/workorder/{tid}/resolve", headers=auth, json={"remark": "需回访确认"}).json()["success"]
    cl = client.post(f"/api/workorder/{tid}/close", headers=sup_auth, json={}).json()
    assert cl["success"] and cl["data"]["revisitSampleId"]
    rid = cl["data"]["revisitSampleId"]
    conn = _db()
    sm = conn.execute("SELECT project_id, status, cust_name FROM smp_sample WHERE id=?", (rid,)).fetchone()
    conn.close()
    assert sm[0] == 1 and sm[1] == "IDLE" and sm[2] == f"回访-工单#{tid}"
    # ③ 外呼回访：非目标样本提交 NA 回池，直到派出回访样本
    hit = None
    for _ in range(6):
        d = client.get("/api/agent/dispatch", headers=auth).json()
        assert d["success"] and d["data"], "回访样本应可被派出"
        if d["data"]["sampleId"] == rid:
            hit = d["data"]
            break
        na = client.post("/api/agent/result", headers=auth,
                         json={"callId": d["data"]["callId"], "resultCode": "NA"}).json()
        assert na["success"] and na["data"]["destination"] == "REDIAL_POOL"
    assert hit, "回访样本未在 6 轮内被派出"
    client.post("/api/agent/answer", headers=auth, json={"callId": hit["callId"], "questionId": 11, "optionIds": [111]})
    client.post("/api/agent/answer", headers=auth, json={"callId": hit["callId"], "questionId": 12, "numericValue": 8})
    res = client.post("/api/agent/result", headers=auth, json={"callId": hit["callId"], "resultCode": "SUCCESS"})
    assert res.json()["success"] and res.json()["data"]["destination"] == "CLOSED_SUCCESS"
    # ④ 审核 → AUDITED
    sheets = client.get("/api/sheet?status=SUBMITTED", headers=sup_auth).json()["data"]["rows"]
    sheet = next(s for s in sheets if s["sample_id"] == rid)
    au = client.post(f"/api/sheet/{sheet['id']}/audit", headers=sup_auth, json={"action": "PASS", "remark": "回访合格"}).json()
    assert au["success"] and au["data"]["status"] == "AUDITED"
    # ⑤ 详情端点：双链路关联断言
    det = client.get(f"/api/workorder/{tid}", headers=sup_auth).json()["data"]
    assert det["revisit"]["sampleId"] == rid
    assert det["revisit"]["sheet"]["id"] == sheet["id"]
    assert det["revisit"]["sheet"]["status"] == "AUDITED"
    assert det["revisit"]["sheet"]["qnr_version"] == "v1.0"  # 问卷版本快照随链路可查


# ── ㉚ SPSS .sav 真实导出（P0 行69）──
def test_31_export_spss(client, sup_auth, auth):
    """P0 行69：.sav 魔数 $FL2；回读对账 行数=AUDITED 答卷数、变量标签=题干、值标签含 男/女；坐席 4032"""
    r = client.post("/api/auth/login", json={"loginName": "agent01", "password": "123456"}).json()
    auth = {"Authorization": "Bearer " + r["data"]["sessionId"]}
    forb = client.get("/api/export/spss", headers=auth)
    assert forb.status_code == 200 and forb.json()["code"] == "4032"  # ResultInfo 风格权限拒绝
    conn = _db()
    n_audited = conn.execute(
        "SELECT COUNT(*) c FROM ans_sheet WHERE project_id=1 AND status='AUDITED'").fetchone()[0]
    conn.close()
    resp = client.get("/api/export/spss?projectId=1", headers=sup_auth)
    assert resp.status_code == 200 and resp.content[:4] == b"$FL2"  # SPSS 系统文件魔数
    import pyreadstat
    with open("/tmp/_chk.sav", "wb") as f:
        f.write(resp.content)
    df, meta = pyreadstat.read_sav("/tmp/_chk.sav")
    assert len(df) == n_audited and n_audited >= 1
    assert "您的性别是？" in (meta.column_labels or [])
    assert "Q11" in (meta.variable_value_labels or {})
    vl = meta.variable_value_labels["Q11"]
    assert "男" in vl.values() and "女" in vl.values()
    assert "Q12" in df.columns  # 数值题原值列
    # 空项目导出：仍为合法 .sav（0 行）
    p = client.post("/api/project", headers=sup_auth, json={"name": "空导出项目"}).json()["data"]["projectId"]
    r2 = client.get(f"/api/export/spss?projectId={p}", headers=sup_auth)
    assert r2.status_code == 200 and r2.content[:4] == b"$FL2"
