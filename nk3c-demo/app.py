# -*- coding: utf-8 -*-
"""
NK3C / ITACATI 最小可运行后端 Demo（FastAPI + SQLite）
=========================================================
实现《ITACATI_功能细化与全链路深度分析》核心闭环（单机演示级，生产按蓝图实现）：

  登录 → 派样(半年原则/黑名单/重拨上限/派样锁) → 逐题作答(断点续答) →
  提交结果码(幂等/样本去向判定) → 配额原子扣减 → 答卷审核 → 单题统计

- 响应统一 ResultInfo 封装【S6】；ID 以数值返回（演示版），生产按 18 位 String
- 路径全小写；鉴权 Shiro 风格 Bearer Token；权限码 domainAdmin/phoneAdmin【S7】
- 表结构为《ITACATI_NK3C_数据库建表.sql》的 SQLite 子集（同名表/字段，便于对照）
运行：
  pip install -r requirements.txt
  uvicorn app:app --host 0.0.0.0 --port 8000     # Swagger: http://localhost:8000/docs
测试：
  pytest -v
"""
import asyncio
import csv
import io
import json
import os
import re
import sqlite3
import uuid
from datetime import datetime, timedelta, timezone
from typing import Optional
from urllib.parse import quote

from fastapi import (Depends, FastAPI, Header, HTTPException, Query, Response,
                     WebSocket, WebSocketDisconnect)
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import HTMLResponse, RedirectResponse
from pydantic import BaseModel

BASE = os.path.dirname(os.path.abspath(__file__))
DB_PATH = os.path.join(BASE, "demo.db")

app = FastAPI(
    title="NK3C 云联络中心 · 最小可运行 Demo",
    description="派样→作答→结果码→配额→审核→统计 核心闭环（对应开发蓝图："
                "《ITACATI_功能细化与全链路深度分析》4.2 状态机 / 4.5 算法 / API 接口规格）",
    version="1.0.0",
)
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])

TOKENS = {}  # token -> user dict（演示用内存会话；生产用 Shiro 会话/Redis）


def now_iso():
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def db():
    conn = sqlite3.connect(DB_PATH)
    conn.row_factory = sqlite3.Row
    conn.isolation_level = None  # 显式事务管理
    return conn


def ok(data=None, message="ok"):
    return {"success": True, "code": "0", "message": message, "data": data}


def fail(code, message):
    return {"success": False, "code": code, "message": message, "data": None}


# ============================================================================
# 建表（与《ITACATI_NK3C_数据库建表.sql》同名的 SQLite 子集）与种子数据
# ============================================================================
SCHEMA = """
CREATE TABLE IF NOT EXISTS sys_user(
  id INTEGER PRIMARY KEY, login_name TEXT UNIQUE, password TEXT, user_name TEXT,
  agent_no TEXT, roles TEXT, status INTEGER DEFAULT 1);
CREATE TABLE IF NOT EXISTS sys_param(param_code TEXT PRIMARY KEY, param_value TEXT);
CREATE TABLE IF NOT EXISTS prj_project(id INTEGER PRIMARY KEY, project_code TEXT, project_name TEXT,
  status TEXT DEFAULT 'DRAFT', questionnaire_id INTEGER);
CREATE TABLE IF NOT EXISTS qnr_questionnaire(id INTEGER PRIMARY KEY, title TEXT, version TEXT, status TEXT);
CREATE TABLE IF NOT EXISTS qnr_question(id INTEGER PRIMARY KEY, qnr_id INTEGER, q_no INTEGER, q_type TEXT,
  title TEXT, required INTEGER DEFAULT 1, min_value REAL, max_value REAL);
CREATE TABLE IF NOT EXISTS qnr_option(id INTEGER PRIMARY KEY, question_id INTEGER, opt_no INTEGER,
  opt_text TEXT, opt_value TEXT, jump_type TEXT DEFAULT 'NEXT', jump_question_id INTEGER);
CREATE TABLE IF NOT EXISTS qnr_quota(id INTEGER PRIMARY KEY, qnr_id INTEGER, quota_name TEXT, total_target INTEGER);
CREATE TABLE IF NOT EXISTS qnr_quota_cell(id INTEGER PRIMARY KEY, quota_id INTEGER, conditions_json TEXT,
  target_count INTEGER, done_count INTEGER DEFAULT 0);
CREATE TABLE IF NOT EXISTS smp_sample(id INTEGER PRIMARY KEY, project_id INTEGER, cust_name TEXT, gender TEXT,
  status TEXT DEFAULT 'IDLE', owner_agent_id INTEGER, attempts INTEGER DEFAULT 0,
  last_connected_at TEXT, shuffle_key INTEGER DEFAULT 0);
CREATE TABLE IF NOT EXISTS smp_phone(id INTEGER PRIMARY KEY, sample_id INTEGER, phone_no TEXT,
  sort_no INTEGER DEFAULT 1, valid_flag INTEGER DEFAULT 1);
CREATE TABLE IF NOT EXISTS smp_blacklist(id INTEGER PRIMARY KEY, phone_no TEXT UNIQUE, scope TEXT DEFAULT 'GLOBAL', reason TEXT);
CREATE TABLE IF NOT EXISTS smp_status_code(code TEXT PRIMARY KEY, name TEXT, category TEXT,
  closed_flag INTEGER, allow_redial INTEGER, hit_black_flag INTEGER);
CREATE TABLE IF NOT EXISTS cti_call_record(id INTEGER PRIMARY KEY, project_id INTEGER, sample_id INTEGER,
  agent_id INTEGER, agent_no TEXT, caller_no TEXT, called_no TEXT, status TEXT DEFAULT 'DIALING',
  begin_time TEXT, connect_time TEXT, end_time TEXT, result_code TEXT);
CREATE TABLE IF NOT EXISTS ans_sheet(id INTEGER PRIMARY KEY, call_id INTEGER UNIQUE, project_id INTEGER,
  sample_id INTEGER, agent_id INTEGER, qnr_id INTEGER, qnr_version TEXT, status TEXT DEFAULT 'DOING', audit_remark TEXT);
CREATE TABLE IF NOT EXISTS ans_answer(id INTEGER PRIMARY KEY, sheet_id INTEGER, question_id INTEGER,
  option_ids TEXT, answer_text TEXT, numeric_value REAL, answered_at TEXT,
  UNIQUE(sheet_id, question_id));
CREATE TABLE IF NOT EXISTS ivr_flow(id INTEGER PRIMARY KEY, name TEXT, flow_json TEXT, updated_at TEXT);
CREATE TABLE IF NOT EXISTS ivr_call_log(id INTEGER PRIMARY KEY, caller_no TEXT, start_time TEXT,
  end_time TEXT, outcome TEXT, path_json TEXT, answers_json TEXT);
CREATE TABLE IF NOT EXISTS wko_ticket(id INTEGER PRIMARY KEY, project_id INTEGER, call_id INTEGER,
  caller_no TEXT, subject TEXT, detail TEXT, status TEXT DEFAULT 'PENDING',
  assigned_agent_id INTEGER, priority TEXT DEFAULT 'NORMAL', remark TEXT,
  created_at TEXT, accepted_at TEXT, resolved_at TEXT, closed_at TEXT,
  revisit_sample_id INTEGER);
"""


def init_db(force=False):
    if force and os.path.exists(DB_PATH):
        os.remove(DB_PATH)
    conn = db()
    conn.executescript(SCHEMA)
    has_user = conn.execute("SELECT COUNT(*) c FROM sys_user").fetchone()["c"]
    if has_user == 0:
        seed(conn)
    conn.close()


def seed(conn):
    conn.execute("BEGIN")
    # 用户（演示密码明文；生产 Shiro 加盐散列）
    conn.execute("INSERT INTO sys_user VALUES(1,'admin','123456','系统管理员',NULL,'domainAdmin,orgAdmin,groupAdmin,phoneAdmin',1)")
    conn.execute("INSERT INTO sys_user VALUES(2,'agent01','123456','坐席演示','1020','phoneAdmin',1)")
    conn.execute("INSERT INTO sys_user VALUES(3,'sup01','123456','督导演示',NULL,'groupAdmin',1)")
    # 参数
    for k, v in [("halfyear.days", "180"), ("redial.max", "3"), ("predict.abandon.max", "3.0")]:
        conn.execute("INSERT INTO sys_param VALUES(?,?)", (k, v))
    # 项目 / 问卷（已发布 v1.0）
    conn.execute("INSERT INTO prj_project VALUES(1,'P2026-001','客户满意度调查_2026','RUNNING',1)")
    conn.execute("INSERT INTO qnr_questionnaire VALUES(1,'客户满意度调查_2026','v1.0','PUBLISHED')")
    # Q11 性别（配额题） Q12 满意度（0-10）
    conn.execute("INSERT INTO qnr_question VALUES(11,1,1,'single','您的性别是？',1,NULL,NULL)")
    conn.execute("INSERT INTO qnr_question VALUES(12,1,2,'number','总体而言您打几分？（0-10）',1,0,10)")
    conn.execute("INSERT INTO qnr_option VALUES(111,11,1,'男','M','NEXT',NULL)")
    conn.execute("INSERT INTO qnr_option VALUES(112,11,2,'女','F','NEXT',NULL)")
    # 配额：性别 ×2 格（演示原子扣减）
    conn.execute("INSERT INTO qnr_quota VALUES(1,1,'性别配额',4)")
    conn.execute("INSERT INTO qnr_quota_cell VALUES(11,1,?,2,0)", (json.dumps([{"questionId": 11, "in": [111]}]),))
    conn.execute("INSERT INTO qnr_quota_cell VALUES(12,1,?,2,0)", (json.dumps([{"questionId": 11, "in": [112]}]),))
    # 样本池（覆盖各过滤规则）
    samples = [
        (101, "张一", "男", "IDLE", 0, None, 1),   # 可派（优先）
        (102, "李二", "女", "SUCCESS", 1, now_iso(), 2),  # 已成功闭环，不再派
        (103, "王三", "男", "IDLE", 0, None, 3),   # 号码在黑名单
        (104, "赵四", "女", "IDLE", 1, now_iso(), 4),  # 30 天前接通过：半年原则拦截
        (105, "孙五", "男", "IDLE", 3, None, 5),   # 重拨超限
        (106, "周六", "女", "IDLE", 1, None, 6),   # 呼过未接通：可派（第二优先）
    ]
    for sid, name, gender, status, attempts, lc, sk in samples:
        conn.execute("INSERT INTO smp_sample VALUES(?,?,?,?,?,?,?,?,?)",
                     (sid, 1, name, gender, status, None, attempts, lc, sk))
        conn.execute("INSERT INTO smp_phone VALUES(?,?,?,1,1)", (sid, sid, f"138{sid:08d}"))
    # 黑名单：103 的号码
    conn.execute("INSERT INTO smp_blacklist VALUES(1,'13800000103','GLOBAL','拒访')")
    # 状态码表（同 MySQL 版初始化）
    codes = [
        ("SUCCESS", "访问成功", "SUCCESS", 1, 0, 0), ("PARTIAL", "部分完成", "NEUTRAL", 1, 1, 0),
        ("QUFAIL", "甄别不合格", "FAIL", 1, 0, 0), ("REFUSE", "拒访", "FAIL", 1, 0, 1),
        ("BREAKOFF", "中途挂断", "FAIL", 1, 1, 0), ("APPOINT", "预约回拨", "APPOINT", 0, 0, 0),
        ("NA", "无人接听", "FAIL", 0, 1, 0), ("BUSY", "占线", "FAIL", 0, 1, 0),
        ("INVALID", "空号", "FAIL", 1, 0, 1), ("FAX", "传真/数据线", "FAIL", 1, 0, 0),
    ]
    conn.executemany("INSERT INTO smp_status_code VALUES(?,?,?,?,?,?)", codes)
    # 项目 2 / 问卷 2（多项目演示：与项目 1 的问卷/配额/样本完全隔离）
    conn.execute("INSERT INTO prj_project VALUES(2,'P2026-002','产品偏好调查_2026','RUNNING',2)")
    conn.execute("INSERT INTO qnr_questionnaire VALUES(2,'产品偏好调查_2026','v1.0','PUBLISHED')")
    conn.execute("INSERT INTO qnr_question VALUES(21,2,1,'single','您的年龄段？',1,NULL,NULL)")
    conn.execute("INSERT INTO qnr_question VALUES(22,2,2,'single','是否愿意推荐给朋友？',2,NULL,NULL)")
    conn.execute("INSERT INTO qnr_option VALUES(211,21,1,'18-30岁','A','NEXT',NULL)")
    conn.execute("INSERT INTO qnr_option VALUES(212,21,2,'31-45岁','B','NEXT',NULL)")
    conn.execute("INSERT INTO qnr_option VALUES(213,21,3,'46岁以上','C','NEXT',NULL)")
    conn.execute("INSERT INTO qnr_option VALUES(221,22,1,'愿意','Y','NEXT',NULL)")
    conn.execute("INSERT INTO qnr_option VALUES(222,22,2,'不愿意','N','NEXT',NULL)")
    conn.execute("INSERT INTO qnr_quota VALUES(2,2,'年龄段配额',5)")
    conn.execute("INSERT INTO qnr_quota_cell VALUES(21,2,?,2,0)", (json.dumps([{"questionId": 21, "in": [211]}]),))
    conn.execute("INSERT INTO qnr_quota_cell VALUES(22,2,?,2,0)", (json.dumps([{"questionId": 21, "in": [212]}]),))
    conn.execute("INSERT INTO qnr_quota_cell VALUES(23,2,?,1,0)", (json.dumps([{"questionId": 21, "in": [213]}]),))
    samples2 = [
        (201, "钱七", "男", "IDLE", 0, None, 1),        # 可派（优先）
        (202, "吴八", "女", "SUCCESS", 1, now_iso(), 2),  # 已闭环
        (203, "郑九", "男", "IDLE", 0, None, 3),        # 号码在黑名单
        (205, "冯十", "女", "IDLE", 3, None, 5),        # 重拨超限
        (206, "陈一", "女", "IDLE", 1, None, 6),        # 呼过未接通：次优
    ]
    for sid, name, gender, status, attempts, lc, sk in samples2:
        conn.execute("INSERT INTO smp_sample VALUES(?,?,?,?,?,?,?,?,?)",
                     (sid, 2, name, gender, status, None, attempts, lc, sk))
        conn.execute("INSERT INTO smp_phone VALUES(?,?,?,1,1)", (sid, sid, f"139{sid:08d}"))
    conn.execute("INSERT INTO smp_blacklist VALUES(2,'13900000203','GLOBAL','拒访')")
    # IVR 呼入流程（S4/S5：菜单分流/问卷/留言/转人工；designer 可视化编排的存储形态）
    seed_flow = {
        "entry": "welcome",
        "nodes": [
            {"id": "welcome", "type": "play", "text": "欢迎致电 NK3C 客户服务中心", "next": "menu"},
            {"id": "menu", "type": "menu", "text": "1键参与满意度调研；2键语音留言；0键转人工",
             "branches": {"1": "q1", "2": "vm", "0": "transfer"}},
            {"id": "q1", "type": "question", "text": "请问您的性别？1键男，2键女",
             "options": {"1": "男", "2": "女"}, "tag": "Q11", "next": "q2"},
            {"id": "q2", "type": "question", "text": "总体而言您打几分？请按0到9键",
             "options": {str(i): str(i) for i in range(10)}, "tag": "Q12", "next": "bye"},
            {"id": "vm", "type": "voicemail", "text": "请在滴声后留言，按井号键结束", "next": "bye"},
            {"id": "transfer", "type": "transfer", "text": "正在为您转接人工坐席，请稍候", "queue": "MANUAL", "next": "bye"},
            {"id": "bye", "type": "end", "text": "感谢来电，再见"},
        ],
    }
    conn.execute("INSERT INTO ivr_flow VALUES(1,'默认呼入流程',?,?)", (json.dumps(seed_flow, ensure_ascii=False), now_iso()))
    conn.execute("COMMIT")


# ============================================================================
# WebSocket 实时推送（监控墙；生产为 nagentstateserver → WS 增量推送）
# ============================================================================
class Hub:
    """WS 连接登记表；同步端点经 broadcast()/send_to_uid() 线程安全地推送事件"""

    def __init__(self):
        self.clients = {}   # websocket -> {"uid","name","agentNo","channel"(monitor/agent)}
        self.loop = None    # 主事件循环（startup 时捕获，供线程池端点投递）

    async def _send_all(self, payload):
        for ws in list(self.clients):
            try:
                await ws.send_json(payload)
            except Exception:
                self.clients.pop(ws, None)

    async def _send_to_uids(self, uids, payload):
        for ws, meta in list(self.clients.items()):
            if meta and meta.get("uid") in uids:
                try:
                    await ws.send_json(payload)
                except Exception:
                    self.clients.pop(ws, None)

    async def _send_channel(self, channel, payload):
        for ws, meta in list(self.clients.items()):
            if meta and meta.get("channel") == channel:
                try:
                    await ws.send_json(payload)
                except Exception:
                    self.clients.pop(ws, None)

    def broadcast(self, type_, data=None):
        if self.loop and self.clients:
            asyncio.run_coroutine_threadsafe(
                self._send_all({"type": type_, "data": data, "ts": now_iso()}), self.loop)

    def broadcast_channel(self, channel, type_, data=None):
        """仅向指定通道（monitor/agent）广播，避免跨通道回声"""
        if self.loop and self.clients:
            asyncio.run_coroutine_threadsafe(
                self._send_channel(channel, {"type": type_, "data": data, "ts": now_iso()}), self.loop)

    def send_to_uid(self, uid, type_, data=None):
        """定向推送：实时质检等仅目标坐席可见的事件"""
        if self.loop and self.clients:
            asyncio.run_coroutine_threadsafe(
                self._send_to_uids({uid}, {"type": type_, "data": data, "ts": now_iso()}), self.loop)


hub = Hub()


@app.on_event("startup")
def _startup():
    init_db()
    hub.loop = asyncio.get_running_loop()


# ============================================================================
# 鉴权
# ============================================================================
def require_auth(authorization: Optional[str] = Header(None)):
    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(401, "未登录（Authorization: Bearer <sessionId>）")
    user = TOKENS.get(authorization[7:])
    if not user:
        raise HTTPException(401, "会话失效，请重新登录")
    return user


class LoginIn(BaseModel):
    loginName: str
    password: str


@app.post("/api/auth/login", tags=["auth"])
def login(body: LoginIn):
    conn = db()
    u = conn.execute("SELECT * FROM sys_user WHERE login_name=? AND password=? AND status=1",
                     (body.loginName, body.password)).fetchone()
    conn.close()
    if not u:
        return fail("4003", "登录名或密码错误")
    token = uuid.uuid4().hex
    user = {"id": u["id"], "user_name": u["user_name"], "agent_no": u["agent_no"], "roles": u["roles"]}
    TOKENS[token] = user
    push_wall()
    return ok({"sessionId": token, "userId": str(u["id"]), "userName": u["user_name"],
               "agentNo": u["agent_no"], "roles": u["roles"].split(","),
               "perms": u["roles"].split(",")}, "登录成功")


# ============================================================================
# 派样（半年原则 + 黑名单 + 重拨上限 + 派样锁，等价 MySQL 附录样例1）
# ============================================================================
@app.get("/api/agent/dispatch", tags=["agent"])
@app.get("/api/agent/dispatch", tags=["agent"])
def dispatch(projectId: int = 1, agent: dict = Depends(require_auth)):
    conn = db()
    try:
        conn.execute("BEGIN IMMEDIATE")  # 派样锁：串行化取样（生产为 FOR UPDATE SKIP LOCKED）
        proj = conn.execute("SELECT * FROM prj_project WHERE id=?", (projectId,)).fetchone()
        if not proj:
            conn.execute("ROLLBACK")
            return fail("4044", f"项目 {projectId} 不存在")
        qnr_row = conn.execute("SELECT * FROM qnr_questionnaire WHERE id=?", (proj["questionnaire_id"],)).fetchone()
        if proj["status"] != "RUNNING" or (qnr_row and qnr_row["status"] != "PUBLISHED"):
            conn.execute("ROLLBACK")
            return fail("4031", f"项目 {projectId} 非运行中（{proj['status']}）或问卷未发布（{qnr_row['status'] if qnr_row else '无'}）")
        halfyear = int(param(conn, "halfyear.days", "180"))
        redial_max = int(param(conn, "redial.max", "3"))
        cutoff = (datetime.now(timezone.utc) - timedelta(days=halfyear)).isoformat(timespec="seconds")
        row = conn.execute(
            """SELECT s.id, s.cust_name, s.gender, s.attempts, p.phone_no
               FROM smp_sample s
               JOIN smp_phone p ON p.sample_id = s.id AND p.valid_flag = 1
               WHERE s.project_id = ? AND s.status = 'IDLE' AND s.attempts < ?
                 AND (s.last_connected_at IS NULL OR s.last_connected_at < ?)
                 AND NOT EXISTS (SELECT 1 FROM smp_blacklist b WHERE b.phone_no = p.phone_no)
               ORDER BY s.shuffle_key LIMIT 1""",
            (projectId, redial_max, cutoff)).fetchone()
        if not row:
            conn.execute("COMMIT")
            return ok(None, "派样失败：过滤后无可用样本（黑名单/半年原则/重拨上限/池空）")
        claim = conn.execute(
            "UPDATE smp_sample SET status='ASSIGNED', owner_agent_id=? WHERE id=? AND status='IDLE'",
            (agent["id"], row["id"]))
        if claim.rowcount != 1:  # 并发竞争失败
            conn.execute("COMMIT")
            return ok(None, "派样竞争失败，请重试")
        cur = conn.execute(
            """INSERT INTO cti_call_record(project_id, sample_id, agent_id, agent_no, caller_no, called_no,
               status, begin_time) VALUES(?,?,?,?,?,?,'DIALING',?)""",
            (projectId, row["id"], agent["id"], agent["agent_no"], "95533", row["phone_no"], now_iso()))
        call_id = cur.lastrowid
        phones = [r["phone_no"] for r in conn.execute(
            "SELECT phone_no FROM smp_phone WHERE sample_id=? AND valid_flag=1 ORDER BY sort_no", (row["id"],))]
        qnr = load_questionnaire(conn, proj["questionnaire_id"])
        conn.execute("COMMIT")
        push_wall()
        return ok({"callId": call_id, "sampleId": row["id"], "custName": row["cust_name"],
                   "gender": row["gender"], "attempts": row["attempts"],
                   "phones": phones, "currentPhone": row["phone_no"], "questionnaire": qnr}, "派样成功")
    except Exception:
        conn.execute("ROLLBACK")
        raise
    finally:
        conn.close()


def param(conn, code, default):
    r = conn.execute("SELECT param_value FROM sys_param WHERE param_code=?", (code,)).fetchone()
    return r["param_value"] if r else default


def load_questionnaire(conn, qnr_id):
    qnr = conn.execute("SELECT * FROM qnr_questionnaire WHERE id=?", (qnr_id,)).fetchone()
    qs = []
    for q in conn.execute("SELECT * FROM qnr_question WHERE qnr_id=? ORDER BY q_no", (qnr_id,)):
        opts = [{"optionId": o["id"], "text": o["opt_text"], "value": o["opt_value"]}
                for o in conn.execute("SELECT * FROM qnr_option WHERE question_id=? ORDER BY opt_no", (q["id"],))]
        qs.append({"questionId": q["id"], "qNo": q["q_no"], "type": q["q_type"], "title": q["title"],
                   "required": bool(q["required"]), "min": q["min_value"], "max": q["max_value"], "options": opts})
    return {"questionnaireId": qnr["id"], "title": qnr["title"], "version": qnr["version"], "questions": qs}


# ============================================================================
# 逐题作答（实时入库 upsert = 断点续答依据；首题提交视为接通）
# ============================================================================
class AnswerIn(BaseModel):
    callId: int
    questionId: int
    optionIds: Optional[list] = None
    answerText: Optional[str] = None
    numericValue: Optional[float] = None


@app.post("/api/agent/answer", tags=["agent"])
def submit_answer(body: AnswerIn, agent: dict = Depends(require_auth)):
    conn = db()
    try:
        conn.execute("BEGIN IMMEDIATE")
        call = conn.execute("SELECT * FROM cti_call_record WHERE id=? AND agent_id=?",
                            (body.callId, agent["id"])).fetchone()
        if not call:
            conn.execute("ROLLBACK")
            return fail("4040", "话务不存在或非本坐席话务")
        if call["status"] == "CLOSED":
            conn.execute("ROLLBACK")
            return fail("4091", "话务已结束，不能再作答")
        ts = now_iso()
        if not call["connect_time"]:  # 首题提交 = 接通
            conn.execute("UPDATE cti_call_record SET connect_time=? WHERE id=?", (ts, body.callId))
            conn.execute("UPDATE smp_sample SET status='INCALL' WHERE id=?", (call["sample_id"],))
        proj = conn.execute("SELECT * FROM prj_project WHERE id=?", (call["project_id"],)).fetchone()
        qnr_row = conn.execute("SELECT * FROM qnr_questionnaire WHERE id=?", (proj["questionnaire_id"],)).fetchone()
        if not conn.execute("SELECT 1 FROM qnr_question WHERE id=? AND qnr_id=?",
                            (body.questionId, proj["questionnaire_id"])).fetchone():
            conn.execute("ROLLBACK")
            return fail("4004", f"题目 {body.questionId} 不属于项目 {call['project_id']} 的问卷（跨项目作答被拒）")
        sheet = conn.execute("SELECT * FROM ans_sheet WHERE call_id=?", (body.callId,)).fetchone()
        if not sheet:
            cur = conn.execute(
                """INSERT INTO ans_sheet(call_id, project_id, sample_id, agent_id, qnr_id, qnr_version, status)
                   VALUES(?,?,?,?,?,?,'DOING')""",
                (body.callId, call["project_id"], call["sample_id"], agent["id"],
                 proj["questionnaire_id"], qnr_row["version"]))
            sheet_id = cur.lastrowid
        else:
            sheet_id = sheet["id"]
        conn.execute(
            """INSERT INTO ans_answer(sheet_id, question_id, option_ids, answer_text, numeric_value, answered_at)
               VALUES(?,?,?,?,?,?)
               ON CONFLICT(sheet_id, question_id) DO UPDATE SET option_ids=excluded.option_ids,
                 answer_text=excluded.answer_text, numeric_value=excluded.numeric_value,
                 answered_at=excluded.answered_at""",
            (sheet_id, body.questionId, json.dumps(body.optionIds or []), body.answerText,
             body.numericValue, ts))
        conn.execute("COMMIT")
        return ok({"sheetId": sheet_id, "answeredAt": ts}, "答案已实时入库（断点续答依据）")
    except Exception:
        conn.execute("ROLLBACK")
        raise
    finally:
        conn.close()


# ============================================================================
# 结果码提交（幂等；样本去向判定 4.2.3；配额原子扣减 4.5.3）
# ============================================================================
class ResultIn(BaseModel):
    callId: int
    resultCode: str
    appointment: Optional[dict] = None


@app.post("/api/agent/result", tags=["agent"])
def submit_result(body: ResultIn, agent: dict = Depends(require_auth)):
    conn = db()
    try:
        conn.execute("BEGIN IMMEDIATE")
        call = conn.execute("SELECT * FROM cti_call_record WHERE id=? AND agent_id=?",
                            (body.callId, agent["id"])).fetchone()
        if not call:
            conn.execute("ROLLBACK")
            return fail("4040", "话务不存在或非本坐席话务")
        if call["status"] == "CLOSED":  # 幂等：返回首写结果
            conn.execute("ROLLBACK")
            return ok({"duplicate": True, "firstResultCode": call["result_code"],
                       "sampleId": call["sample_id"]}, "重复提交，已返回首写结果")
        code = conn.execute("SELECT * FROM smp_status_code WHERE code=?", (body.resultCode,)).fetchone()
        if not code:
            conn.execute("ROLLBACK")
            return fail("4004", "未知结果码")
        ts = now_iso()
        conn.execute("UPDATE cti_call_record SET status='CLOSED', end_time=?, result_code=? WHERE id=?",
                     (ts, body.resultCode, body.callId))
        # ── 样本去向判定（4.2.3）──
        sample_id = call["sample_id"]
        if code["hit_black_flag"]:
            phone = conn.execute("SELECT phone_no FROM smp_phone WHERE sample_id=? AND valid_flag=1 ORDER BY sort_no",
                                 (sample_id,)).fetchone()
            if phone:
                conn.execute("INSERT OR IGNORE INTO smp_blacklist(phone_no, scope, reason) VALUES(?,'GLOBAL',?)",
                             (phone["phone_no"], f"{code['name']}（系统自动）"))
            dest = "BANNED"
            conn.execute("UPDATE smp_sample SET status='BANNED', owner_agent_id=NULL WHERE id=?", (sample_id,))
        elif body.resultCode == "APPOINT":
            dest = "APPOINT_QUEUE"
            conn.execute("UPDATE smp_sample SET status='APPOINT', owner_agent_id=NULL WHERE id=?", (sample_id,))
        elif code["closed_flag"]:
            dest = "CLOSED_" + body.resultCode
            st = "SUCCESS" if body.resultCode == "SUCCESS" else "FAIL"
            conn.execute("UPDATE smp_sample SET status=?, owner_agent_id=NULL, last_connected_at=? WHERE id=?",
                         (st, ts, sample_id))
        else:  # 可重拨 → 回池
            dest = "REDIAL_POOL"
            conn.execute("UPDATE smp_sample SET status='IDLE', owner_agent_id=NULL, attempts=attempts+1 WHERE id=?",
                         (sample_id,))
        # ── 答卷与配额 ──
        quota_full = None
        sheet = conn.execute("SELECT * FROM ans_sheet WHERE call_id=?", (body.callId,)).fetchone()
        if sheet and sheet["status"] == "DOING":
            if body.resultCode == "SUCCESS":
                quota_full = try_consume_quota(conn, sheet["id"])
                conn.execute("UPDATE ans_sheet SET status='SUBMITTED' WHERE id=?", (sheet["id"],))
            elif code["closed_flag"]:
                conn.execute("UPDATE ans_sheet SET status='VOID', audit_remark=? WHERE id=?",
                             (f"{code['name']}，答卷作废", sheet["id"]))
        conn.execute("COMMIT")
        push_wall()
        data = {"sampleId": sample_id, "destination": dest,
                "sheetStatus": ("SUBMITTED" if body.resultCode == "SUCCESS" else
                                ("VOID" if (sheet and code["closed_flag"]) else None))}
        if quota_full is not None:
            data["quotaConsume"] = ("CONSUMED" if quota_full else "QUOTA_FULL（按 overflow 策略放行）")
        return ok(data, f"结果码 {body.resultCode} 已提交，样本 → {dest}")
    except Exception:
        conn.execute("ROLLBACK")
        raise
    finally:
        conn.close()


def try_consume_quota(conn, sheet_id):
    """配额命中判定 + 原子扣减：UPDATE done=done+1 WHERE done<target（0 行=满格）"""
    answers = conn.execute("SELECT * FROM ans_answer WHERE sheet_id=?", (sheet_id,)).fetchall()
    for cell in conn.execute(
            """SELECT c.* FROM qnr_quota_cell c JOIN qnr_quota q ON q.id=c.quota_id
               JOIN qnr_questionnaire n ON n.id=q.qnr_id
               JOIN ans_sheet s ON s.qnr_id=n.id WHERE s.id=?""", (sheet_id,)):
        conds = json.loads(cell["conditions_json"])
        hit = True
        for cond in conds:
            ans = next((a for a in answers if a["question_id"] == cond["questionId"]), None)
            if not ans:
                hit = False
                break
            chose = json.loads(ans["option_ids"] or "[]")
            if not any(v in chose for v in cond["in"]):
                hit = False
                break
        if hit:
            upd = conn.execute(
                "UPDATE qnr_quota_cell SET done_count=done_count+1 WHERE id=? AND done_count<target_count",
                (cell["id"],))
            return upd.rowcount == 1
    return None


# ============================================================================
# 监控 / 答卷 / 统计
# ============================================================================
def _wall_data(conn):
    agents = []
    for u in conn.execute("SELECT * FROM sys_user WHERE agent_no IS NOT NULL AND status=1"):
        assigned = conn.execute("SELECT id FROM smp_sample WHERE owner_agent_id=? AND status='ASSIGNED'", (u["id"],)).fetchone()
        incall = conn.execute(
            """SELECT c.id call_id, c.connect_time FROM cti_call_record c
               WHERE c.agent_id=? AND c.status='DIALING' ORDER BY c.id DESC LIMIT 1""", (u["id"],)).fetchone()
        talking = conn.execute(
            """SELECT s.id sheet_id FROM ans_sheet s JOIN cti_call_record c ON c.id=s.call_id
               WHERE s.agent_id=? AND s.status='DOING' AND c.connect_time IS NOT NULL
               ORDER BY s.id DESC LIMIT 1""", (u["id"],)).fetchone()
        state = "READY"
        if talking:
            state = "TALKING"
        elif incall:
            state = "DIALING"
        elif assigned:
            state = "DIALING"
        agents.append({"agentNo": u["agent_no"], "userName": u["user_name"], "state": state,
                       "sampleId": assigned["id"] if assigned else None,
                       "callId": incall["call_id"] if incall else None})
    today = now_iso()[:10]
    summary = {
        "dialCount": conn.execute("SELECT COUNT(*) c FROM cti_call_record WHERE substr(begin_time,1,10)=?", (today,)).fetchone()["c"],
        "connectCount": conn.execute("SELECT COUNT(*) c FROM cti_call_record WHERE connect_time IS NOT NULL").fetchone()["c"],
        "successCount": conn.execute("SELECT COUNT(*) c FROM cti_call_record WHERE result_code='SUCCESS'").fetchone()["c"],
        "abandonCount": 0,
    }
    return {"agents": agents, "summary": summary}


def push_wall():
    """广播监控墙快照（login/dispatch/result/audit 等变更点触发）"""
    try:
        conn = db()
        hub.broadcast("wall", _wall_data(conn))
        conn.close()
    except Exception:
        pass


@app.get("/api/monitor/wall", tags=["monitor"])
def monitor_wall(user: dict = Depends(require_auth)):
    conn = db()
    data = _wall_data(conn)
    conn.close()
    return ok(data)


@app.get("/api/sheet", tags=["answer"])
def sheet_list(status: Optional[str] = None, user: dict = Depends(require_auth)):
    conn = db()
    if status:
        rows = conn.execute("SELECT * FROM ans_sheet WHERE status=? ORDER BY id DESC", (status,)).fetchall()
    else:
        rows = conn.execute("SELECT * FROM ans_sheet ORDER BY id DESC").fetchall()
    conn.close()
    return ok({"total": len(rows),
               "rows": [dict(r) for r in rows]})


class AuditIn(BaseModel):
    action: str  # PASS / REJECT / VOID
    remark: Optional[str] = None


@app.post("/api/sheet/{sheet_id}/audit", tags=["answer"])
def audit(sheet_id: int, body: AuditIn, user: dict = Depends(require_auth)):
    if user["roles"].find("groupAdmin") < 0 and user["roles"].find("orgAdmin") < 0 and user["roles"].find("domainAdmin") < 0:
        return fail("4032", "需要督导及以上权限（groupAdmin）")
    conn = db()
    try:
        conn.execute("BEGIN IMMEDIATE")
        sheet = conn.execute("SELECT * FROM ans_sheet WHERE id=?", (sheet_id,)).fetchone()
        if not sheet:
            conn.execute("ROLLBACK")
            return fail("4042", "答卷不存在")
        if sheet["status"] != "SUBMITTED":
            conn.execute("ROLLBACK")
            return fail("4031", f"答卷当前 {sheet['status']}，仅 SUBMITTED 可审核")
        target = {"PASS": "AUDITED", "REJECT": "REJECTED", "VOID": "VOID"}.get(body.action)
        if not target:
            conn.execute("ROLLBACK")
            return fail("4004", "action 须为 PASS/REJECT/VOID")
        conn.execute("UPDATE ans_sheet SET status=?, audit_remark=? WHERE id=?",
                     (target, body.remark or "", sheet_id))
        if body.action == "REJECT":  # 驳回 → 生成重访（样本回池，4.2.3）
            conn.execute("UPDATE smp_sample SET status='IDLE', owner_agent_id=NULL WHERE id=? AND status='SUCCESS'",
                         (sheet["sample_id"],))
        conn.execute("COMMIT")
        push_wall()
        return ok({"sheetId": sheet_id, "status": target}, "审核完成")
    except Exception:
        conn.execute("ROLLBACK")
        raise
    finally:
        conn.close()


@app.get("/api/report/single", tags=["report"])
def report_single(questionId: int, user: dict = Depends(require_auth)):
    """单题频数（实时口径：SUBMITTED+AUDITED），与《建表脚本》附录样例3一致"""
    conn = db()
    q = conn.execute("SELECT * FROM qnr_question WHERE id=?", (questionId,)).fetchone()
    if not q:
        conn.close()
        return fail("4043", "题目不存在")
    rows = conn.execute(
        """SELECT a.option_ids, COUNT(*) c FROM ans_answer a
           JOIN ans_sheet s ON s.id=a.sheet_id
           WHERE a.question_id=? AND s.status IN ('SUBMITTED','AUDITED')
           GROUP BY a.option_ids""", (questionId,)).fetchall()
    counts = {}
    for r in rows:
        for oid in json.loads(r["option_ids"] or "[]"):
            counts[oid] = counts.get(oid, 0) + int(r["c"])
    opts = conn.execute("SELECT * FROM qnr_option WHERE question_id=?", (questionId,)).fetchall()
    total = sum(counts.values())
    items = [{"optionId": o["id"], "label": o["opt_text"], "value": o["opt_value"],
              "count": counts.get(o["id"], 0),
              "pct": round(counts.get(o["id"], 0) / total * 100, 1) if total else 0}
             for o in opts]
    quota = None
    cells = conn.execute(
        """SELECT c.id, c.conditions_json, c.target_count, c.done_count
           FROM qnr_quota_cell c JOIN qnr_quota q ON q.id=c.quota_id
           WHERE q.qnr_id=?""", (q["qnr_id"],)).fetchall()
    if cells:
        quota = [{"cellId": c["id"], "target": c["target_count"], "done": c["done_count"],
                  "pct": round(c["done_count"] / c["target_count"] * 100, 1) if c["target_count"] else 0}
                 for c in cells]
    conn.close()
    return ok({"questionId": questionId, "title": q["title"], "total": total, "items": items, "quota": quota})


@app.get("/api/health", tags=["sys"])
def health():
    return ok({"service": "nk3c-demo", "time": now_iso(), "db": os.path.exists(DB_PATH)})


# ============================================================================
# 前端集成：控制台首页 / 坐席台 Live / 督导台 Live（前后端贯通演示）
# ============================================================================
STATIC_DIR = os.path.join(BASE, "static")
AGENT_HTML_PATH = os.path.join(STATIC_DIR, "agent_live.html")
SUPER_HTML_PATH = os.path.join(STATIC_DIR, "supervisor_live.html")

LANDING_HTML = """<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>NK3C 演示系统 · 控制台</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:"Microsoft YaHei","PingFang SC",sans-serif;background:#0f1e2e;color:#e8eef4;min-height:100vh;display:flex;align-items:center;justify-content:center}
.box{max-width:760px;width:92%}
h1{font-size:24px;letter-spacing:2px;text-align:center;margin-bottom:6px}
h1 b{color:#6fc2ff}
.sub{text-align:center;color:#8fa5ba;font-size:13px;margin-bottom:26px;line-height:1.8}
.grid{display:grid;grid-template-columns:1fr 1fr 1fr;gap:14px}
.card{background:#16293c;border:1px solid #24405c;border-radius:10px;padding:20px 18px;text-align:center;text-decoration:none;color:#e8eef4;transition:.15s;display:block}
.card:hover{border-color:#6fc2ff;transform:translateY(-2px)}
.card .ico{font-size:30px}.card b{display:block;margin:10px 0 6px;font-size:15px}
.card p{color:#8fa5ba;font-size:12px;line-height:1.7}
.tag{display:inline-block;background:#27863c;color:#fff;font-size:11px;border-radius:3px;padding:1px 7px;margin-top:10px}
.note{margin-top:24px;background:rgba(230,126,34,.08);border:1px solid rgba(230,126,34,.3);color:#e8b98a;font-size:12px;border-radius:8px;padding:10px 16px;line-height:1.9;text-align:center}
</style></head><body><div class="box">
<h1>NK3C <b>演示系统</b> · 控制台</h1>
<div class="sub">派样 → 作答 → 结果码 → 配额 → 审核 → 统计 全链路真实运行（FastAPI + SQLite）<br>
种子双项目并行：①客户满意度（101/106 可派，过滤 103/104/105/102）②产品偏好（201 优先 / 206 次优，配额独立）<br>派样接口支持 ?projectId= 任意项目；新建项目可在 /docs 或 API 完成 创建→加题→配额→发布→导样 全流程</div>
<div class="grid" style="grid-template-columns:repeat(3,1fr)">
<a class="card" href="/agent"><div class="ico">☎</div><b>坐席工作台</b><p>登录 → 派样 → 呼叫 → 作答<br>→ 提交结果码（看样本去向与配额扣减）</p><span class="tag">agent01 / 123456</span></a>
<a class="card" href="/supervisor"><div class="ico">🎧</div><b>督导控制台</b><p>监控墙 WS 实时推送 → 话务流水<br>→ 审核（通过/驳回）→ 导出 Excel/CSV</p><span class="tag">sup01 / 123456</span></a>
<a class="card" href="/ivr"><div class="ico">Tree</div><b>IVR 流程设计器</b><p>可视化编排呼入流程 → 话机模拟器<br>→ 按键走线（菜单/问题/留言/转人工）</p><span class="tag">admin / 123456</span></a>
<a class="card" href="/projects"><div class="ico">🗂</div><b>项目管理台</b><p>创建→加题→配额→发布→导样<br>→ 修订 v1.x+1 / 暂停 / 结束</p><span class="tag">admin / 123456</span></a>
<a class="card" href="/docs"><div class="ico">🔌</div><b>API 文档</b><p>OpenAPI 3.0 交互式文档<br>47 端点规格的 demo 实现</p><span class="tag">Swagger UI</span></a>
</div>
<div class="note">提示：演示数据被玩坏后，用 admin（123456）登录任一控制台，点击右下角【↺ 重置演示数据】即可恢复种子状态。<br>源码：nk3c-demo/ · 蓝图：index.html 原型与文档中心</div>
</div></body></html>"""


@app.get("/", tags=["sys"])
def index():
    """控制台首页：坐席台 / 督导台 / API 文档入口"""
    return HTMLResponse(LANDING_HTML)


@app.get("/agent", response_class=HTMLResponse, tags=["sys"])
def agent_page():
    """坐席工作台 Live 版（fetch 调用本服务 API，同源免跨域）"""
    if os.path.exists(AGENT_HTML_PATH):
        with open(AGENT_HTML_PATH, encoding="utf-8") as f:
            return HTMLResponse(f.read())
    return HTMLResponse("<h3>static/agent_live.html 不存在</h3>", status_code=404)


@app.get("/supervisor", response_class=HTMLResponse, tags=["sys"])
def supervisor_page():
    """督导控制台 Live 版（监控墙轮询 / 话务流水 / 答卷审核 / 实时统计）"""
    if os.path.exists(SUPER_HTML_PATH):
        with open(SUPER_HTML_PATH, encoding="utf-8") as f:
            return HTMLResponse(f.read())
    return HTMLResponse("<h3>static/supervisor_live.html 不存在</h3>", status_code=404)


@app.get("/projects", response_class=HTMLResponse, tags=["sys"])
def projects_page():
    """项目管理台 Live 版（创建→加题→配额→发布→导样→修订→暂停/结束 全生命周期可视化）"""
    path = os.path.join(STATIC_DIR, "projects_live.html")
    if os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            return HTMLResponse(f.read())
    return HTMLResponse("<h3>static/projects_live.html 不存在</h3>", status_code=404)


@app.get("/ivr", response_class=HTMLResponse, tags=["sys"])
def ivr_page():
    """IVR 流程设计器 Live 版（可视化编排 + 话机模拟器，呼入按键真实走线）"""
    path = os.path.join(STATIC_DIR, "ivr_live.html")
    if os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            return HTMLResponse(f.read())
    return HTMLResponse("<h3>static/ivr_live.html 不存在</h3>", status_code=404)


@app.get("/api/monitor/calls", tags=["monitor"])
def recent_calls(limit: int = 12, user: dict = Depends(require_auth)):
    """最近话务流水（M10 话务统计的实时版；含样本/坐席/结果码/时间）"""
    conn = db()
    rows = conn.execute(
        """SELECT c.id, c.sample_id, s.cust_name, c.agent_no, c.status, c.result_code,
                  c.begin_time, c.connect_time, c.end_time
           FROM cti_call_record c LEFT JOIN smp_sample s ON s.id = c.sample_id
           ORDER BY c.id DESC LIMIT ?""", (max(1, min(limit, 50)),)).fetchall()
    conn.close()
    return ok([dict(r) for r in rows])


@app.websocket("/ws/monitor")
async def ws_monitor(websocket: WebSocket, token: str = Query("")):
    """监控实时推送通道：连接即推监控墙快照，此后 dispatch/result/audit/reset 事件驱动增量推送"""
    user = TOKENS.get(token or "")
    roles = (user or {}).get("roles", "")
    if not user or not any(r in roles for r in ("groupAdmin", "orgAdmin", "domainAdmin")):
        await websocket.close(code=4401)  # 未认证/权限不足
        return
    await websocket.accept()
    conn = db()
    await websocket.send_json({"type": "wall", "data": _wall_data(conn), "ts": now_iso()})
    conn.close()
    hub.clients[websocket] = {"uid": user["id"], "name": user["user_name"],
                              "agentNo": user.get("agent_no"), "channel": "monitor"}
    try:
        while True:
            await websocket.receive_text()  # 仅保活；推送由服务端事件驱动
    except WebSocketDisconnect:
        pass
    finally:
        hub.clients.pop(websocket, None)


@app.websocket("/ws/agent")
async def ws_agent(websocket: WebSocket, token: str = Query("")):
    """坐席实时通道：接收督导实时质检指令（监听/插话/示忙/示闲/强挂/签出）"""
    user = TOKENS.get(token or "")
    if not user:
        await websocket.close(code=4401)
        return
    await websocket.accept()
    hub.clients[websocket] = {"uid": user["id"], "name": user["user_name"],
                              "agentNo": user.get("agent_no"), "channel": "agent"}
    try:
        while True:
            await websocket.receive_text()
    except WebSocketDisconnect:
        pass
    finally:
        hub.clients.pop(websocket, None)


# ============================================================================
# 实时质检（S5 督导接口：监听/插话/强制示忙/示闲/挂断/签出；demo 经 WS 下发指令）
# ============================================================================
QC_ACTIONS = {
    "LISTEN": "监听", "INSERT": "插话", "SETBUSY": "强制示忙",
    "SETIDLE": "强制示闲", "FORCEHANGUP": "强制挂断", "FORCECHECKOUT": "强制签出",
}


class QcIn(BaseModel):
    agentNo: str
    action: str
    note: Optional[str] = None


@app.post("/api/monitor/qc", tags=["qc"])
def qc_act(body: QcIn, user: dict = Depends(require_auth)):
    """下发实时质检指令（groupAdmin+）；FORCECHECKOUT 同时失效目标坐席全部会话"""
    if not any(r in (user["roles"] or "") for r in ("groupAdmin", "orgAdmin", "domainAdmin")):
        return fail("4032", "实时质检需要督导及以上权限（groupAdmin）")
    if body.action not in QC_ACTIONS:
        return fail("4004", f"action 须为 {'/'.join(QC_ACTIONS)}")
    conn = db()
    u = conn.execute("SELECT * FROM sys_user WHERE agent_no=? AND status=1", (body.agentNo,)).fetchone()
    conn.close()
    if not u:
        return fail("4044", f"工号 {body.agentNo} 不存在")
    if body.action == "FORCECHECKOUT":  # 服务端执行：踢会话（生产同步通知 nagentstateserver）
        for t in [t for t, uu in TOKENS.items() if uu["id"] == u["id"]]:
            TOKENS.pop(t, None)
    hub.send_to_uid(u["id"], "qc", {"action": body.action, "actionName": QC_ACTIONS[body.action],
                                    "note": body.note or "", "by": user["user_name"]})
    hub.broadcast_channel("monitor", "qclog", {"agentNo": body.agentNo, "action": body.action,
                                               "actionName": QC_ACTIONS[body.action], "by": user["user_name"]})
    return ok({"agentNo": body.agentNo, "action": body.action, "actionName": QC_ACTIONS[body.action]},
              f"已下发【{QC_ACTIONS[body.action]}】至工号 {body.agentNo}")


# ============================================================================
# 录音回放同步质检（S2：答卷审核 + 录音回放同步；demo 为服务端合成音轨，生产对接录音存储）
# ============================================================================
def _answer_display(conn, a):
    """答案展示值：选项→文本 / 数值 / 文本"""
    ids = json.loads(a["option_ids"] or "[]") if a["option_ids"] else []
    if ids:
        texts = []
        for oid in ids:
            o = conn.execute("SELECT opt_text FROM qnr_option WHERE id=?", (oid,)).fetchone()
            texts.append(o["opt_text"] if o else str(oid))
        return "/".join(texts)
    if a["numeric_value"] is not None:
        return str(a["numeric_value"])
    return a["answer_text"] or ""


def _recording_layout(answers):
    """合成音轨的确定性地标：第 i 题在 0.8 + i*4.0s 处发声 0.7s；总长≥10s（与 WAV 生成共用）"""
    cues = []
    for i, a in enumerate(answers):
        keys = a.keys()
        qid = a["question_id"] if "question_id" in keys else a["questionId"]
        cues.append({"questionId": qid, "offset": round(0.8 + i * 4.0, 2), "dur": 0.7})
    total = round(0.8 + len(answers) * 4.0 + 1.2, 2)
    return cues, max(total, 10.0)


@app.get("/api/sheet/{sheet_id}/answers", tags=["qc"])
def sheet_answers(sheet_id: int, user: dict = Depends(require_auth)):
    """答卷明细 + 录音地标（offset/dur 与合成音轨一致，供回放同步高亮）"""
    conn = db()
    s = conn.execute(
        """SELECT s.id, s.call_id, s.sample_id, s.qnr_version, s.status, s.audit_remark,
                  sp.cust_name, u.agent_no
           FROM ans_sheet s
           JOIN smp_sample sp ON sp.id = s.sample_id
           LEFT JOIN sys_user u ON u.id = s.agent_id
           WHERE s.id=?""", (sheet_id,)).fetchone()
    if not s:
        conn.close()
        return fail("4042", "答卷不存在")
    answers = []
    for a in conn.execute(
            """SELECT a.*, q.title FROM ans_answer a JOIN qnr_question q ON q.id=a.question_id
               WHERE a.sheet_id=? ORDER BY a.answered_at, a.id""", (sheet_id,)):
        answers.append({"questionId": a["question_id"], "title": a["title"],
                        "value": _answer_display(conn, a), "answeredAt": a["answered_at"]})
    cues, total = _recording_layout(answers)
    conn.close()
    return ok({"sheet": dict(s), "answers": answers, "cues": cues, "duration": total})


def _synth_wav(cues, total):
    """8kHz/8bit 单声道 WAV：每题在 offset 处发对应频率提示音（660/880/520…），间隔为低幅底噪"""
    import math
    import wave
    RATE = 8000
    FREQS = [660, 880, 520, 740, 990, 440]
    n = int(total * RATE)
    samples = bytearray(n)
    for i in range(n):  # 底噪：微幅噪声，模拟线路底噪
        samples[i] = 127 + (i * 2654435761 % 7 - 3)
    for k, c in enumerate(cues):
        f = FREQS[k % len(FREQS)]
        a0, a1 = int(c["offset"] * RATE), int((c["offset"] + c["dur"]) * RATE)
        for i in range(a0, min(a1, n)):
            ph = 2 * math.pi * f * (i - a0) / RATE
            edge = min(1.0, (i - a0) / (0.02 * RATE), (a1 - i) / (0.02 * RATE))
            samples[i] = int(127 + 88 * math.sin(ph) * max(edge, 0))
    buf = io.BytesIO()
    with wave.open(buf, "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(1)
        w.setframerate(RATE)
        w.writeframes(bytes(samples))
    return buf.getvalue()


@app.get("/api/qc/recording/{call_id}", tags=["qc"])
def qc_recording(call_id: int, token: str = Query("")):
    """通话录音（demo 为服务端按答卷地标合成；音频控件无法带 Header，用 token 查询参数鉴权）"""
    user = TOKENS.get(token or "")
    roles = (user or {}).get("roles", "")
    if not user or not any(r in roles for r in ("groupAdmin", "orgAdmin", "domainAdmin")):
        raise HTTPException(403, "录音回放需要督导及以上权限（groupAdmin）")
    conn = db()
    call = conn.execute("SELECT id FROM cti_call_record WHERE id=?", (call_id,)).fetchone()
    answers = conn.execute(
        """SELECT a.question_id FROM ans_answer a JOIN ans_sheet s ON s.id=a.sheet_id
           WHERE s.call_id=? ORDER BY a.answered_at, a.id""", (call_id,)).fetchall()
    conn.close()
    if not call:
        raise HTTPException(404, "话务不存在")
    cues, total = _recording_layout(answers)
    data = _synth_wav(cues, total)
    return Response(content=data, media_type="audio/wav",
                    headers={"Content-Disposition": f"inline; filename=call_{call_id}.wav"})


# ============================================================================
# 导出中心（S3：ITACATI 导出 Excel/Quantum/SPSS/Txt —— demo 实现 Excel/CSV）
# ============================================================================
def _perm_fail(user, roles):
    if not any(r in (user["roles"] or "") for r in roles):
        return fail("4032", f"需要督导及以上权限（{'/'.join(roles)}）")
    return None


def _csv_response(header, rows, name):
    """CSV（带 UTF-8 BOM，Excel 直接打开不乱码）"""
    buf = io.StringIO()
    buf.write("\ufeff")
    w = csv.writer(buf)
    w.writerow(header)
    w.writerows(rows)
    return Response(
        content=buf.getvalue(), media_type="text/csv; charset=utf-8",
        headers={"Content-Disposition": f"attachment; filename*=UTF-8''{quote('NK3C_' + name + '.csv')}"})


def _xlsx_response(sheets, name):
    """xlsx（openpyxl 内存生成；sheets=[(sheet名, 表头, 行), ...]）"""
    from openpyxl import Workbook
    wb = Workbook()
    wb.remove(wb.active)
    for sname, header, rows in sheets:
        ws = wb.create_sheet(sname[:31])
        ws.append(header)
        for r in rows:
            ws.append(r)
    buf = io.BytesIO()
    wb.save(buf)
    return Response(
        content=buf.getvalue(),
        media_type="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        headers={"Content-Disposition": f"attachment; filename*=UTF-8''{quote('NK3C_' + name + '.xlsx')}"})


def _export_rows():
    """答卷明细：sheet × 动态题目列（按答卷实际作答的题目透视，多项目通用）"""
    conn = db()
    sheets = conn.execute(
        """SELECT s.id, s.sample_id, s.qnr_version, s.status, c.end_time AS submit_time,
                  s.audit_remark, sp.cust_name, u.agent_no, p.project_name
           FROM ans_sheet s
           JOIN smp_sample sp ON sp.id = s.sample_id
           JOIN prj_project p ON p.id = s.project_id
           LEFT JOIN sys_user u ON u.id = s.agent_id
           LEFT JOIN cti_call_record c ON c.id = s.call_id
           ORDER BY s.id""").fetchall()
    qmap, qcols = {}, []
    for a in conn.execute("SELECT sheet_id, question_id, option_ids, numeric_value, answer_text FROM ans_answer"):
        d = qmap.setdefault(a["sheet_id"], {})
        d[a["question_id"]] = _answer_display(conn, a)
    titles = {r["id"]: r["title"] for r in conn.execute("SELECT id, title FROM qnr_question")}
    qids = sorted({q for d in qmap.values() for q in d}, key=lambda x: (x > 100, x))
    for qid in qids:
        qcols.append(f"Q{qid} {titles.get(qid, '')[:10]}")
    header = (["答卷ID", "项目", "样本ID", "客户姓名", "坐席工号", "问卷版本"] + qcols +
              ["状态", "提交时间", "审核备注"])
    rows = []
    for sh in sheets:
        d = qmap.get(sh["id"], {})
        rows.append([sh["id"], sh["project_name"], sh["sample_id"], sh["cust_name"],
                     sh["agent_no"] or "", sh["qnr_version"]] +
                    [d.get(qid, "") for qid in qids] +
                    [sh["status"], sh["submit_time"] or "", sh["audit_remark"] or ""])
    conn.close()
    return header, rows


@app.get("/api/export/answers", tags=["export"])
def export_answers(format: str = "csv", user: dict = Depends(require_auth)):
    """答卷明细导出（format=csv|xlsx；生产对接导出中心 Excel/Quantum/SPSS/Txt【S3】）"""
    guard = _perm_fail(user, ("groupAdmin", "orgAdmin", "domainAdmin"))
    if guard:
        return guard
    header, rows = _export_rows()
    if format == "xlsx":
        return _xlsx_response([("答卷明细", header, rows)], "答卷明细")
    return _csv_response(header, rows, "答卷明细")


@app.get("/api/export/stats", tags=["export"])
def export_stats(format: str = "csv", user: dict = Depends(require_auth)):
    """统计报表导出：Q11 频数（SUBMITTED+AUDITED 口径）+ Q12 均分 + 配额进度"""
    guard = _perm_fail(user, ("groupAdmin", "orgAdmin", "domainAdmin"))
    if guard:
        return guard
    conn = db()
    q11 = conn.execute("SELECT title FROM qnr_question WHERE id=11").fetchone()
    q12 = conn.execute("SELECT title FROM qnr_question WHERE id=12").fetchone()
    freq = report_single(11, user)  # 复用实时统计口径
    avg = conn.execute(
        """SELECT AVG(a.numeric_value) v FROM ans_answer a JOIN ans_sheet s ON s.id=a.sheet_id
           WHERE a.question_id=12 AND s.status IN ('SUBMITTED','AUDITED')""").fetchone()["v"]
    quota_rows = conn.execute(
        """SELECT c.id, c.target_count, c.done_count FROM qnr_quota_cell c
           JOIN qnr_quota q ON q.id=c.quota_id WHERE q.qnr_id=1 ORDER BY c.id""").fetchall()
    conn.close()
    header = ["题目", "选项/指标", "数量", "占比%"]
    rows = [[q11["title"], i["label"], i["count"], i["pct"]] for i in freq["data"]["items"]]
    rows.append([q12["title"], "平均分", round(avg, 2) if avg is not None else "-", ""])
    if format == "xlsx":
        qh = ["配额格", "目标", "完成", "进度%"]
        qr = [[f"配额格#{c['id']}", c["target_count"], c["done_count"],
               round(c["done_count"] / c["target_count"] * 100, 1) if c["target_count"] else 0]
              for c in quota_rows]
        return _xlsx_response([("单题统计", header, rows), ("配额进度", qh, qr)], "统计报表")
    return _csv_response(header, rows, "统计报表")


@app.get("/api/export/spss", tags=["export"])
def export_spss(projectId: int = 1, user: dict = Depends(require_auth)):
    """SPSS .sav 真实格式导出（P0 行69：变量标签=题干、值标签=选项文本；行数=已审 AUDITED 答卷数）"""
    guard = _perm_fail(user, ("groupAdmin", "orgAdmin", "domainAdmin"))
    if guard:
        return guard
    try:
        import pandas as pd
        import pyreadstat
    except ImportError:
        return fail("5000", "服务器未安装 pyreadstat/pandas（requirements.txt 已声明）")
    conn = db()
    proj = conn.execute("SELECT * FROM prj_project WHERE id=?", (projectId,)).fetchone()
    if not proj:
        conn.close()
        return fail("4044", f"项目 {projectId} 不存在")
    qnr_id = proj["questionnaire_id"]
    sheets = conn.execute(
        """SELECT s.id, s.sample_id, s.qnr_version, sp.cust_name, u.agent_no
           FROM ans_sheet s
           JOIN smp_sample sp ON sp.id = s.sample_id
           LEFT JOIN sys_user u ON u.id = s.agent_id
           WHERE s.project_id=? AND s.status='AUDITED' ORDER BY s.id""", (projectId,)).fetchall()
    qdefs = []
    for q in conn.execute("SELECT * FROM qnr_question WHERE qnr_id=? ORDER BY q_no", (qnr_id,)):
        opts = [{"id": o["id"], "text": o["opt_text"]} for o in conn.execute(
            "SELECT * FROM qnr_option WHERE question_id=? ORDER BY opt_no", (q["id"],))]
        qdefs.append({"id": q["id"], "title": q["title"], "type": q["q_type"], "options": opts})
    conn.close()
    # 列：样本信息 + 每题一列（单选按选项序号 1..k 编码；数值/文本原值）
    cols = ["sample_id", "cust_name", "agent_no", "qnr_version"]
    data = {c: [] for c in cols}
    qcols, vlabels = [], {}
    for q in qdefs:
        name = f"Q{q['id']}"
        cols.append(name)
        qcols.append((name, q))
        data[name] = []
        if q["type"] == "single":
            vlabels[name] = {float(i + 1): o["text"] for i, o in enumerate(q["options"])}
    for sh in sheets:
        data["sample_id"].append(sh["sample_id"])
        data["cust_name"].append(sh["cust_name"])
        data["agent_no"].append(sh["agent_no"] or "")
        data["qnr_version"].append(sh["qnr_version"])
        conn = db()
        answers = {a["question_id"]: a for a in conn.execute(
            "SELECT * FROM ans_answer WHERE sheet_id=?", (sh["id"],))}
        conn.close()
        for name, q in qcols:
            a = answers.get(q["id"])
            if not a:
                data[name].append(None)
                continue
            if q["type"] == "single":
                ids = json.loads(a["option_ids"] or "[]")
                idx = next((i + 1 for i, o in enumerate(q["options"]) if o["id"] in ids), None)
                data[name].append(float(idx) if idx else None)
            elif q["type"] == "number":
                data[name].append(a["numeric_value"])
            else:
                data[name].append(a["answer_text"] or "")
    df = pd.DataFrame(data, columns=cols)
    labels = {"sample_id": "样本ID", "cust_name": "客户姓名", "agent_no": "坐席工号",
              "qnr_version": "问卷版本"}
    labels.update({name: q["title"] for name, q in qcols})
    import tempfile
    with tempfile.NamedTemporaryFile(suffix=".sav", delete=False) as tf:
        tmp = tf.name
    pyreadstat.write_sav(df, tmp, column_labels=labels, variable_value_labels=(vlabels or None))
    with open(tmp, "rb") as f:
        payload = f.read()
    os.remove(tmp)  # 空库也输出合法 .sav 结构（0 行）
    return Response(
        content=payload, media_type="application/octet-stream",
        headers={"Content-Disposition": f"attachment; filename*=UTF-8''{quote('NK3C_数据_' + str(proj['project_code']) + '.sav')}"})


# ============================================================================
# 项目 / 问卷 / 样本 管理（project/questionnaire/sample 三 tag 的 demo 实现）
# ============================================================================
def _require_admin(user, roles=("groupAdmin", "orgAdmin", "domainAdmin")):
    if not any(r in (user["roles"] or "") for r in roles):
        return fail("4032", f"需要督导及以上权限（{'/'.join(roles)}）")
    return None


@app.get("/api/project", tags=["project"])
def project_list(user: dict = Depends(require_auth)):
    """项目总览：状态/问卷版本/样本统计/配额进度"""
    conn = db()
    rows = []
    for p in conn.execute("SELECT * FROM prj_project ORDER BY id"):
        qnr = conn.execute("SELECT * FROM qnr_questionnaire WHERE id=?", (p["questionnaire_id"],)).fetchone()
        sm = conn.execute("""SELECT COUNT(*) total, SUM(CASE WHEN status='IDLE' THEN 1 ELSE 0 END) idle
                             FROM smp_sample WHERE project_id=?""", (p["id"],)).fetchone()
        quota = conn.execute("""SELECT c.target_count t, c.done_count d FROM qnr_quota_cell c
                                JOIN qnr_quota q ON q.id=c.quota_id WHERE q.qnr_id=?""",
                             (p["questionnaire_id"],)).fetchall()
        rows.append({"projectId": p["id"], "projectCode": p["project_code"], "name": p["project_name"],
                     "status": p["status"], "questionnaireId": qnr["id"] if qnr else None,
                     "qnrVersion": qnr["version"] if qnr else None, "qnrStatus": qnr["status"] if qnr else None,
                     "samples": {"total": sm["total"] or 0, "idle": sm["idle"] or 0},
                     "quota": {"done": sum(x["d"] for x in quota), "target": sum(x["t"] for x in quota)}})
    conn.close()
    return ok(rows)


class ProjectIn(BaseModel):
    name: str


@app.post("/api/project", tags=["project"])
def project_create(body: ProjectIn, user: dict = Depends(require_auth)):
    """新建项目（DRAFT）+ 空白问卷（Draft）；仅督导及以上"""
    guard = _require_admin(user)
    if guard:
        return guard
    conn = db()
    pid = conn.execute("SELECT COALESCE(MAX(id),0)+1 FROM prj_project").fetchone()[0]
    nid = conn.execute("SELECT COALESCE(MAX(id),0)+1 FROM qnr_questionnaire").fetchone()[0]
    conn.execute("INSERT INTO prj_project VALUES(?,?,?,?,?)",
                 (pid, f"P2026-{pid:03d}", body.name, "DRAFT", nid))
    conn.execute("INSERT INTO qnr_questionnaire VALUES(?,?, 'Draft','DRAFT')", (nid, body.name))
    conn.close()
    return ok({"projectId": pid, "questionnaireId": nid}, f"项目「{body.name}」已创建（草稿）")


class QuestionIn(BaseModel):
    text: str
    qType: str = "single"          # single / number / text
    options: Optional[list] = None  # single 的选项文本
    min: Optional[float] = None
    max: Optional[float] = None


@app.post("/api/project/{pid}/questions", tags=["questionnaire"])
def add_question(pid: int, body: QuestionIn, user: dict = Depends(require_auth)):
    """加题（含选项）：仅 DRAFT 项目可编辑（发布后问卷版本冻结）"""
    guard = _require_admin(user)
    if guard:
        return guard
    conn = db()
    p = conn.execute("SELECT * FROM prj_project WHERE id=?", (pid,)).fetchone()
    if not p:
        conn.close()
        return fail("4044", f"项目 {pid} 不存在")
    if p["status"] != "DRAFT":
        conn.close()
        return fail("4031", f"项目 {pid} 已是 {p['status']}，问卷不可编辑（发布后版本冻结）")
    if body.qType not in ("single", "number", "text"):
        conn.close()
        return fail("4004", "qType 须为 single/number/text")
    if body.qType == "single" and not body.options:
        conn.close()
        return fail("4004", "single 题须提供 options")
    qid = conn.execute("SELECT COALESCE(MAX(id),100)+1 FROM qnr_question").fetchone()[0]
    qno = conn.execute("SELECT COALESCE(MAX(q_no),0)+1 FROM qnr_question WHERE qnr_id=?",
                       (p["questionnaire_id"],)).fetchone()[0]
    conn.execute("INSERT INTO qnr_question VALUES(?,?,?,?,?,?,?,?)",
                 (qid, p["questionnaire_id"], qno, body.qType, body.text, 1,
                  body.min if body.qType == "number" else None,
                  body.max if body.qType == "number" else None))
    if body.qType == "single":
        for i, txt in enumerate(body.options, 1):
            oid = conn.execute("SELECT COALESCE(MAX(id),100)+1 FROM qnr_option").fetchone()[0]
            conn.execute("INSERT INTO qnr_option VALUES(?,?,?,?,?,?,NULL)", (oid, qid, i, txt, f"V{i}", "NEXT"))
    conn.close()
    return ok({"questionId": qid}, f"题目 {qid} 已加入「{body.text[:12]}…」")


class QuotaCellIn(BaseModel):
    questionId: int
    inList: list
    target: int


class QuotaIn(BaseModel):
    name: str = "配额"
    cells: list  # 元素经 QuotaCellIn 校验（见下）


@app.put("/api/project/{pid}/quota", tags=["questionnaire"])
def set_quota(pid: int, body: QuotaIn, user: dict = Depends(require_auth)):
    """设置配额（整组替换）：仅 DRAFT；格子选项必须属于本项目问卷题目"""
    guard = _require_admin(user)
    if guard:
        return guard
    conn = db()
    p = conn.execute("SELECT * FROM prj_project WHERE id=?", (pid,)).fetchone()
    if not p:
        conn.close()
        return fail("4044", f"项目 {pid} 不存在")
    if p["status"] != "DRAFT":
        conn.close()
        return fail("4031", f"项目 {pid} 已是 {p['status']}，配额不可编辑")
    try:
        cells = [QuotaCellIn(**c) for c in body.cells]
    except Exception as e:
        conn.close()
        return fail("4004", f"配额格格式错误：{e}")
    for c in cells:
        q = conn.execute("SELECT * FROM qnr_question WHERE id=? AND qnr_id=?",
                         (c.questionId, p["questionnaire_id"])).fetchone()
        if not q:
            conn.close()
            return fail("4004", f"题目 {c.questionId} 不属于本项目问卷")
        if q["q_type"] != "single" or not c.inList:
            conn.close()
            return fail("4004", "配额格仅支持 single 题")
        for oid in c.inList:
            if not conn.execute("SELECT 1 FROM qnr_option WHERE id=? AND question_id=?", (oid, c.questionId)).fetchone():
                conn.close()
                return fail("4004", f"选项 {oid} 不属于题目 {c.questionId}")
        if c.target < 1:
            conn.close()
            return fail("4004", "target 须 ≥1")
    old = conn.execute("SELECT id FROM qnr_quota WHERE qnr_id=?", (p["questionnaire_id"],)).fetchone()
    if old:
        conn.execute("DELETE FROM qnr_quota_cell WHERE quota_id=?", (old["id"],))
        conn.execute("DELETE FROM qnr_quota WHERE id=?", (old["id"],))
    qn_id = conn.execute("SELECT COALESCE(MAX(id),0)+1 FROM qnr_quota").fetchone()[0]
    conn.execute("INSERT INTO qnr_quota VALUES(?,?,?,?)",
                 (qn_id, p["questionnaire_id"], body.name, sum(c.target for c in cells)))
    for c in cells:
        cid = conn.execute("SELECT COALESCE(MAX(id),0)+1 FROM qnr_quota_cell").fetchone()[0]
        conn.execute("INSERT INTO qnr_quota_cell VALUES(?,?,?,?,0)",
                     (cid, qn_id, json.dumps([{"questionId": c.questionId, "in": c.inList}]), c.target))
    conn.close()
    return ok({"cells": len(cells), "target": sum(c.target for c in cells)}, "配额已设置")


@app.post("/api/project/{pid}/publish", tags=["questionnaire"])
def publish_project(pid: int, user: dict = Depends(require_auth)):
    """发布：问卷 v1.0 PUBLISHED + 项目 RUNNING（须≥1 题；配额格引用合法在设置时已校验）"""
    guard = _require_admin(user)
    if guard:
        return guard
    conn = db()
    p = conn.execute("SELECT * FROM prj_project WHERE id=?", (pid,)).fetchone()
    if not p:
        conn.close()
        return fail("4044", f"项目 {pid} 不存在")
    if p["status"] != "DRAFT":
        conn.close()
        return fail("4031", f"项目 {pid} 已是 {p['status']}，无需重复发布")
    n = conn.execute("SELECT COUNT(*) c FROM qnr_question WHERE qnr_id=?", (p["questionnaire_id"],)).fetchone()["c"]
    if n == 0:
        conn.close()
        return fail("4004", "问卷至少需要 1 道题")
    conn.execute("UPDATE qnr_questionnaire SET version='v1.0', status='PUBLISHED' WHERE id=?", (p["questionnaire_id"],))
    conn.execute("UPDATE prj_project SET status='RUNNING' WHERE id=?", (pid,))
    conn.close()
    return ok({"projectId": pid, "questions": n}, f"项目 {pid} 已发布并投入运行（问卷 v1.0，{n} 题）")


class SamplesIn(BaseModel):
    names: list  # ["客户A", "客户B", ...]
    phones: Optional[list] = None  # 可选：与 names 等长的号码列表（缺省按 ID 派生）


@app.post("/api/project/{pid}/samples", tags=["sample"])
def import_samples(pid: int, body: SamplesIn, user: dict = Depends(require_auth)):
    """批量导入样本（IDLE，电话 15x 随机段；半年原则字段为空=可立即拨打）"""
    guard = _require_admin(user)
    if guard:
        return guard
    conn = db()
    p = conn.execute("SELECT * FROM prj_project WHERE id=?", (pid,)).fetchone()
    if not p:
        conn.close()
        return fail("4044", f"项目 {pid} 不存在")
    if not body.names:
        conn.close()
        return fail("4004", "names 不能为空")
    if body.phones and len(body.phones) != len(body.names):
        conn.close()
        return fail("4004", "phones 与 names 长度不一致")
    first = conn.execute("SELECT COALESCE(MAX(id),900000)+1 FROM smp_sample").fetchone()[0]
    made, skipped = [], 0
    for i, nm in enumerate(body.names):
        phone = str(body.phones[i]) if body.phones else f"137{(first + i) % 100000000:08d}"
        dup = conn.execute(
            """SELECT 1 FROM smp_phone p JOIN smp_sample s ON s.id=p.sample_id
               WHERE s.project_id=? AND p.phone_no=?""", (pid, phone)).fetchone()
        if dup:  # 同项目内查重：跳过（生产为域内查重，按配置跳过/标记）
            skipped += 1
            continue
        sid = first + i
        sk = conn.execute("SELECT COALESCE(MAX(shuffle_key),0)+1 FROM smp_sample").fetchone()[0]
        conn.execute("INSERT INTO smp_sample VALUES(?,?,?,?,?,?,?,?,?)",
                     (sid, pid, str(nm), "未知", "IDLE", None, 0, None, sk))
        conn.execute("INSERT INTO smp_phone VALUES(?,?,?,1,1)", (sid, sid, phone))
        made.append(sid)
    conn.close()
    msg = f"已导入 {len(made)} 个样本到项目 {pid}" + (f"，跳过重复 {skipped} 条" if skipped else "")
    return ok({"sampleIds": made, "count": len(made), "skipped": skipped}, msg)


@app.get("/api/project/{pid}", tags=["project"])
def project_detail(pid: int, user: dict = Depends(require_auth)):
    """项目详情：题目+选项、配额格、样本分状态统计、答卷版本分布（管理端只读聚合）"""
    conn = db()
    p = conn.execute("SELECT * FROM prj_project WHERE id=?", (pid,)).fetchone()
    if not p:
        conn.close()
        return fail("4044", f"项目 {pid} 不存在")
    qnr = conn.execute("SELECT * FROM qnr_questionnaire WHERE id=?", (p["questionnaire_id"],)).fetchone()
    qs = []
    for q in conn.execute("SELECT * FROM qnr_question WHERE qnr_id=? ORDER BY q_no", (p["questionnaire_id"],)):
        opts = [{"optionId": o["id"], "text": o["opt_text"]} for o in conn.execute(
            "SELECT * FROM qnr_option WHERE question_id=? ORDER BY opt_no", (q["id"],))]
        qs.append({"questionId": q["id"], "qNo": q["q_no"], "type": q["q_type"], "title": q["title"],
                   "min": q["min_value"], "max": q["max_value"], "options": opts})
    cells = [{"cellId": c["id"], "conditions": json.loads(c["conditions_json"]),
              "target": c["target_count"], "done": c["done_count"]}
             for c in conn.execute(
                 """SELECT * FROM qnr_quota_cell WHERE quota_id IN
                    (SELECT id FROM qnr_quota WHERE qnr_id=?)""", (p["questionnaire_id"],))]
    sm = {r["status"]: r["c"] for r in conn.execute(
        "SELECT status, COUNT(*) c FROM smp_sample WHERE project_id=? GROUP BY status", (pid,))}
    versions = {r["qnr_version"]: r["c"] for r in conn.execute(
        "SELECT qnr_version, COUNT(*) c FROM ans_sheet WHERE project_id=? GROUP BY qnr_version", (pid,))}
    conn.close()
    return ok({"projectId": p["id"], "name": p["project_name"], "status": p["status"],
               "questionnaireId": qnr["id"], "qnrVersion": qnr["version"], "qnrStatus": qnr["status"],
               "questions": qs, "quotaCells": cells,
               "samplesByStatus": sm, "sheetVersions": versions})


def _bump_version(v):
    m = re.match(r"v(\d+)\.(\d+)", v or "")
    return f"v{m.group(1)}.{int(m.group(2)) + 1}" if m else "v1.1"


class ReviseIn(BaseModel):
    addQuestions: list  # [{text, qType, options?, min?, max?}]


@app.post("/api/project/{pid}/revise", tags=["questionnaire"])
def project_revise(pid: int, body: ReviseIn, user: dict = Depends(require_auth)):
    """修订 RUNNING 项目的问卷：加题并升版本 v1.x→v1.x+1（已答答卷保留旧版本快照）【S4 中途可调】"""
    guard = _require_admin(user)
    if guard:
        return guard
    conn = db()
    try:
        conn.execute("BEGIN IMMEDIATE")
        p = conn.execute("SELECT * FROM prj_project WHERE id=?", (pid,)).fetchone()
        if not p:
            conn.execute("ROLLBACK")
            return fail("4044", f"项目 {pid} 不存在")
        if p["status"] != "RUNNING":
            conn.execute("ROLLBACK")
            return fail("4031", f"仅 RUNNING 项目可修订（当前 {p['status']}；DRAFT 请用加题端点）")
        qnr = conn.execute("SELECT * FROM qnr_questionnaire WHERE id=?", (p["questionnaire_id"],)).fetchone()
        new_v = _bump_version(qnr["version"])
        added = []
        for spec in body.addQuestions:
            qt = spec.get("qType", "single")
            if qt not in ("single", "number", "text"):
                conn.execute("ROLLBACK")
                return fail("4004", "qType 须为 single/number/text")
            if qt == "single" and not spec.get("options"):
                conn.execute("ROLLBACK")
                return fail("4004", "single 题须提供 options")
            qid = conn.execute("SELECT COALESCE(MAX(id),100)+1 FROM qnr_question").fetchone()[0]
            qno = conn.execute("SELECT COALESCE(MAX(q_no),0)+1 FROM qnr_question WHERE qnr_id=?",
                               (p["questionnaire_id"],)).fetchone()[0]
            conn.execute("INSERT INTO qnr_question VALUES(?,?,?,?,?,?,?,?)",
                         (qid, p["questionnaire_id"], qno, qt, spec["text"], 1,
                          spec.get("min") if qt == "number" else None,
                          spec.get("max") if qt == "number" else None))
            if qt == "single":
                for i, txt in enumerate(spec["options"], 1):
                    oid = conn.execute("SELECT COALESCE(MAX(id),100)+1 FROM qnr_option").fetchone()[0]
                    conn.execute("INSERT INTO qnr_option VALUES(?,?,?,?,?,?,NULL)", (oid, qid, i, txt, f"V{i}", "NEXT"))
            added.append(qid)
        conn.execute("UPDATE qnr_questionnaire SET version=? WHERE id=?", (new_v, p["questionnaire_id"]))
        conn.execute("COMMIT")
        return ok({"projectId": pid, "oldVersion": qnr["version"], "newVersion": new_v,
                   "addedQuestionIds": added},
                  f"问卷已修订 {qnr['version']} → {new_v}；已答答卷保留 {qnr['version']} 快照，新话务按 {new_v} 执行")
    except Exception:
        conn.execute("ROLLBACK")
        raise
    finally:
        conn.close()


class ProjStatusIn(BaseModel):
    action: str  # pause / resume / finish


PRJ_FLOW = {"pause": ("RUNNING", "PAUSED"), "resume": ("PAUSED", "RUNNING"), "finish": ("RUNNING", "FINISHED")}


@app.post("/api/project/{pid}/status", tags=["project"])
def project_status(pid: int, body: ProjStatusIn, user: dict = Depends(require_auth)):
    """项目状态机：pause（暂停派样）/ resume / finish（终态，不可再变更）；非法跳转 4031"""
    guard = _require_admin(user)
    if guard:
        return guard
    if body.action not in PRJ_FLOW:
        return fail("4004", "action 须为 pause/resume/finish")
    conn = db()
    try:
        conn.execute("BEGIN IMMEDIATE")
        p = conn.execute("SELECT * FROM prj_project WHERE id=?", (pid,)).fetchone()
        if not p:
            conn.execute("ROLLBACK")
            return fail("4044", f"项目 {pid} 不存在")
        src, dst = PRJ_FLOW[body.action]
        if p["status"] != src:
            conn.execute("ROLLBACK")
            return fail("4031", f"项目当前 {p['status']}，{body.action} 仅允许 {src} → {dst}")
        conn.execute("UPDATE prj_project SET status=? WHERE id=?", (dst, pid))
        conn.execute("COMMIT")
        return ok({"projectId": pid, "status": dst},
                  {"pause": "项目已暂停（派样挂起，进行中话务不受影响）",
                   "resume": "项目已恢复运行",
                   "finish": "项目已结束（终态，不可再变更）"}[body.action])
    except Exception:
        conn.execute("ROLLBACK")
        raise
    finally:
        conn.close()


# ============================================================================
# IVR 呼入流程引擎（S4/S5：任意级跳转/留言/转人工；demo 为按键模拟，生产对接语音网关）
# ============================================================================
IVR_CALLS = {}  # sid -> session（演示用内存；生产为 IVR 会话状态机集群）


class IvrIn(BaseModel):
    key: Optional[str] = None      # 按键（0-9 * #）
    message: Optional[str] = None  # voicemail 留言内容（demo 用文本模拟录音）


class FlowIn(BaseModel):
    name: str = "呼入流程"
    flow: dict


def _ivr_validate(flow):
    """设计器保存校验：entry 存在、id 唯一、类型/跳转/分支完备"""
    if not isinstance(flow, dict) or not isinstance(flow.get("nodes"), list) or not flow["nodes"]:
        return "nodes 须为非空数组"
    ids = [n.get("id") for n in flow["nodes"]]
    if any(not i for i in ids) or len(ids) != len(set(ids)):
        return "节点 id 缺失或重复"
    idset = set(ids)
    if flow.get("entry") not in idset:
        return "entry 必须指向存在的节点"
    types = ("play", "menu", "question", "voicemail", "transfer", "end")
    for n in flow["nodes"]:
        t = n.get("type")
        if t not in types:
            return f"节点 {n.get('id')}：type 须为 {'/'.join(types)}"
        if not (n.get("text") or "").strip():
            return f"节点 {n['id']}：播报文本不能为空"
        targets = []
        if t == "menu":
            if not n.get("branches"):
                return f"节点 {n['id']}：menu 至少一个按键分支"
            targets += list(n["branches"].values())
        elif t == "question":
            if not n.get("options"):
                return f"节点 {n['id']}：question 至少一个按键选项"
            if not n.get("tag"):
                return f"节点 {n['id']}：question 须有 tag（答案归类标识）"
        if t in ("play", "question", "voicemail", "transfer") and not n.get("next"):
            return f"节点 {n['id']}：{t} 节点须有 next（menu 的出口即按键分支）"
        if n.get("next"):
            targets.append(n["next"])
        for tg in targets:
            if tg not in idset:
                return f"节点 {n['id']}：跳转目标 {tg} 不存在"
    return None


def _ivr_finalize(s):
    """通话落库：ivr_call_log（路径/按键答案/结局）；转人工自动生成工单（呼入转工单闭环）"""
    if "留言" in s["answers"]:
        s["outcome"] = s["outcome"] or "VOICEMAIL"
    if any(k in s["answers"] for k in ("Q11", "Q12")):
        s["outcome"] = s["outcome"] or "SURVEY"
    s["outcome"] = s["outcome"] or "INFO"
    s["end"] = now_iso()
    conn = db()
    cur = conn.execute(
        "INSERT INTO ivr_call_log(caller_no, start_time, end_time, outcome, path_json, answers_json) VALUES(?,?,?,?,?,?)",
        (s["callerNo"], s["start"], s["end"], s["outcome"],
         json.dumps(s["path"], ensure_ascii=False), json.dumps(s["answers"], ensure_ascii=False)))
    if s["outcome"] and s["outcome"].startswith("TRANSFER"):
        conn.execute(
            """INSERT INTO wko_ticket(project_id, call_id, caller_no, subject, detail, status,
               priority, created_at) VALUES(1,?,?,?,?,?,'HIGH',?)""",
            (cur.lastrowid, s["callerNo"], "IVR转人工来电", "呼入菜单按键0转人工；通话轨迹：" +
             " → ".join(s["path"]), "PENDING", s["end"]))
    conn.close()


def _ivr_enter(nodes, node, s):
    """进入节点：play/transfer 自动流转；menu/question/voicemail 停下等按键；end 落库"""
    while True:
        s["transcript"].append({"node": node["id"], "type": node["type"], "text": node["text"]})
        s["path"].append(node["id"])
        if node["type"] == "play" and node.get("next"):
            node = nodes[node["next"]]
            continue
        if node["type"] == "transfer":
            s["outcome"] = "TRANSFER:" + node.get("queue", "MANUAL")
            s["transcript"][-1]["event"] = "转人工队列"
            node = nodes[node["next"]]
            continue
        if node["type"] == "end":
            s["done"] = True
            _ivr_finalize(s)
            return node
        return node  # menu / question / voicemail：等待按键


def _ivr_flow_nodes():
    conn = db()
    flow = json.loads(conn.execute("SELECT flow_json FROM ivr_flow WHERE id=1").fetchone()["flow_json"])
    conn.close()
    return flow, {n["id"]: n for n in flow["nodes"]}


@app.get("/api/ivr/flow", tags=["ivr"])
def ivr_flow_get(user: dict = Depends(require_auth)):
    conn = db()
    r = conn.execute("SELECT name, flow_json, updated_at FROM ivr_flow WHERE id=1").fetchone()
    conn.close()
    return ok({"name": r["name"], "flow": json.loads(r["flow_json"]), "updatedAt": r["updated_at"]})


@app.put("/api/ivr/flow", tags=["ivr"])
def ivr_flow_put(body: FlowIn, user: dict = Depends(require_auth)):
    """保存流程（设计器发布）：先静态校验，通过即生效（下一通呼入走新流程）【S4 中途可调】"""
    if not any(r in (user["roles"] or "") for r in ("groupAdmin", "orgAdmin", "domainAdmin")):
        return fail("4032", "保存流程需要督导及以上权限（groupAdmin）")
    err = _ivr_validate(body.flow)
    if err:
        return fail("4004", f"流程校验失败：{err}")
    conn = db()
    conn.execute("UPDATE ivr_flow SET name=?, flow_json=?, updated_at=? WHERE id=1",
                 (body.name, json.dumps(body.flow, ensure_ascii=False), now_iso()))
    conn.close()
    return ok(True, "流程已保存并即时生效（下一通呼入按新流程走线）")


@app.post("/api/ivr/call", tags=["ivr"])
def ivr_call_start(body: Optional[dict] = None, user: dict = Depends(require_auth)):
    """模拟呼入进入 IVR（生产：语音网关事件触发）"""
    flow, nodes = _ivr_flow_nodes()
    sid = uuid.uuid4().hex[:12]
    s = {"sid": sid, "callerNo": (body or {}).get("callerNo") or ("139" + uuid.uuid4().hex[:8]),
         "transcript": [], "path": [], "answers": {}, "outcome": None,
         "start": now_iso(), "done": False, "current": flow["entry"]}
    IVR_CALLS[sid] = s
    node = _ivr_enter(nodes, nodes[flow["entry"]], s)
    s["current"] = node["id"]
    return ok({"sessionId": sid, "callerNo": s["callerNo"], "node": node,
               "transcript": s["transcript"], "answers": s["answers"],
               "done": s["done"], "outcome": s["outcome"]}, "呼入已接入")


@app.post("/api/ivr/call/{sid}/input", tags=["ivr"])
def ivr_call_input(sid: str, body: IvrIn, user: dict = Depends(require_auth)):
    """按键输入 → 流程走线（menu 分流 / question 记答案 / voicemail 留言）"""
    s = IVR_CALLS.get(sid)
    if not s:
        return fail("4044", "会话不存在（可能已被重置）")
    if s["done"]:
        return fail("4091", "通话已结束")
    flow, nodes = _ivr_flow_nodes()
    node = nodes[s["current"]]
    key = (body.key or "").strip()
    nxt, invalid = None, False
    if node["type"] == "menu":
        nxt = node["branches"].get(key)
        invalid = nxt is None
    elif node["type"] == "question":
        val = node["options"].get(key)
        if val is None:
            invalid = True
        else:
            s["answers"][node.get("tag") or node["id"]] = val
            nxt = node["next"]
    elif node["type"] == "voicemail":
        if key == "#" or body.message is not None:
            s["answers"]["留言"] = body.message if body.message is not None else "(语音留言)"
            nxt = node["next"]
        else:
            invalid = True
    else:
        invalid = True
    if invalid:
        s["transcript"].append({"node": node["id"], "type": "INVALID", "text": f"按键 {key or '(空)'} 无效，请重听"})
        return ok({"sessionId": sid, "node": node, "transcript": s["transcript"], "answers": s["answers"],
                   "done": False, "outcome": None, "invalidKey": True}, "按键无效，已重播当前节点")
    node = _ivr_enter(nodes, nodes[nxt], s)
    s["current"] = node["id"]
    return ok({"sessionId": sid, "node": node, "transcript": s["transcript"], "answers": s["answers"],
               "done": s["done"], "outcome": s["outcome"]})


@app.post("/api/ivr/call/{sid}/hangup", tags=["ivr"])
def ivr_call_hangup(sid: str, user: dict = Depends(require_auth)):
    """主叫挂断：未走完的通话按 ABANDONED 落库"""
    s = IVR_CALLS.get(sid)
    if not s:
        return fail("4044", "会话不存在（可能已被重置）")
    if s["done"]:
        return ok({"sessionId": sid, "outcome": s["outcome"], "answers": s["answers"]}, "通话已结束")
    s["done"] = True
    if not s["outcome"]:
        s["outcome"] = "ABANDONED"
    _ivr_finalize(s)
    return ok({"sessionId": sid, "outcome": s["outcome"], "answers": s["answers"]}, "主叫挂断，通话已落库")


@app.get("/api/ivr/logs", tags=["ivr"])
def ivr_logs(limit: int = 20, user: dict = Depends(require_auth)):
    conn = db()
    rows = conn.execute("SELECT * FROM ivr_call_log ORDER BY id DESC LIMIT ?",
                        (max(1, min(limit, 50)),)).fetchall()
    conn.close()
    return ok([dict(r) for r in rows])


# ============================================================================
# 工单管理（workorder tag；P0 行96 呼入转工单：IVR 自动落单 → 受理 → 办结 → 归档）
# ============================================================================
WK_NEXT = {"PENDING": "ACCEPTED", "ACCEPTED": "RESOLVED", "RESOLVED": "CLOSED"}


@app.get("/api/workorder", tags=["workorder"])
def workorder_list(status: Optional[str] = None, user: dict = Depends(require_auth)):
    conn = db()
    if status:
        rows = conn.execute(
            """SELECT w.*, u.user_name AS agent_name FROM wko_ticket w
               LEFT JOIN sys_user u ON u.id=w.assigned_agent_id
               WHERE w.status=? ORDER BY w.id DESC""", (status,)).fetchall()
    else:
        rows = conn.execute(
            """SELECT w.*, u.user_name AS agent_name FROM wko_ticket w
               LEFT JOIN sys_user u ON u.id=w.assigned_agent_id ORDER BY w.id DESC""").fetchall()
    conn.close()
    return ok({"total": len(rows), "rows": [dict(r) for r in rows]})


@app.get("/api/workorder/{tid}", tags=["workorder"])
def workorder_detail(tid: int, user: dict = Depends(require_auth)):
    """工单详情 + 回访闭环链路：工单 → 回访样本 → 回访话务/答卷（audit 状态）"""
    conn = db()
    w = conn.execute(
        """SELECT w.*, u.user_name AS agent_name FROM wko_ticket w
           LEFT JOIN sys_user u ON u.id=w.assigned_agent_id WHERE w.id=?""", (tid,)).fetchone()
    if not w:
        conn.close()
        return fail("4044", "工单不存在")
    revisit = None
    if w["revisit_sample_id"]:
        sm = conn.execute("SELECT * FROM smp_sample WHERE id=?", (w["revisit_sample_id"],)).fetchone()
        if sm:
            sheet = conn.execute("SELECT id, status, qnr_version FROM ans_sheet WHERE sample_id=? ORDER BY id DESC LIMIT 1",
                                 (sm["id"],)).fetchone()
            revisit = {"sampleId": sm["id"], "custName": sm["cust_name"], "status": sm["status"],
                       "sheet": dict(sheet) if sheet else None}
    conn.close()
    d = dict(w)
    d["revisit"] = revisit
    return ok(d)


@app.post("/api/workorder/{tid}/accept", tags=["workorder"])
def workorder_accept(tid: int, user: dict = Depends(require_auth)):
    """坐席认领工单：PENDING → ACCEPTED（记录受理人与时间）"""
    conn = db()
    try:
        conn.execute("BEGIN IMMEDIATE")
        w = conn.execute("SELECT * FROM wko_ticket WHERE id=?", (tid,)).fetchone()
        if not w:
            conn.execute("ROLLBACK")
            return fail("4044", "工单不存在")
        if w["status"] != "PENDING":
            conn.execute("ROLLBACK")
            return fail("4031", f"工单当前 {w['status']}，仅 PENDING 可受理")
        conn.execute("UPDATE wko_ticket SET status='ACCEPTED', assigned_agent_id=?, accepted_at=? WHERE id=?",
                     (user["id"], now_iso(), tid))
        conn.execute("COMMIT")
        push_wall()
        return ok({"ticketId": tid, "status": "ACCEPTED", "agent": user["user_name"]}, "工单已受理")
    except Exception:
        conn.execute("ROLLBACK")
        raise
    finally:
        conn.close()


class WkoRemark(BaseModel):
    remark: Optional[str] = None


WKO_PREV = {"RESOLVED": "ACCEPTED", "CLOSED": "RESOLVED"}


def _wko_advance(tid, target, user, remark):
    """工单状态机推进：RESOLVED（受理人/督导）、CLOSED（督导归档）"""
    if target == "CLOSED" and not any(r in (user["roles"] or "") for r in ("groupAdmin", "orgAdmin", "domainAdmin")):
        return fail("4032", "归档需要督导及以上权限（groupAdmin）")
    conn = db()
    try:
        conn.execute("BEGIN IMMEDIATE")
        w = conn.execute("SELECT * FROM wko_ticket WHERE id=?", (tid,)).fetchone()
        if not w:
            conn.execute("ROLLBACK")
            return fail("4044", "工单不存在")
        if w["status"] != WKO_PREV[target]:
            conn.execute("ROLLBACK")
            return fail("4031", f"工单当前 {w['status']}，不能直接置 {target}")
        ts = now_iso()
        col = "resolved_at" if target == "RESOLVED" else "closed_at"
        revisit = w["revisit_sample_id"]
        if target == "CLOSED" and not revisit:  # 归档 → 自动生成回访样本进项目1（P0 行96 回访闭环）
            sid = conn.execute("SELECT COALESCE(MAX(id),900000)+1 FROM smp_sample").fetchone()[0]
            sk = conn.execute("SELECT COALESCE(MAX(shuffle_key),0)+1 FROM smp_sample").fetchone()[0]
            conn.execute("INSERT INTO smp_sample VALUES(?,?,?,?,?,?,?,?,?)",
                         (sid, 1, f"回访-工单#{tid}", "未知", "IDLE", None, 0, None, sk))
            conn.execute("INSERT INTO smp_phone VALUES(?,?,?,1,1)", (sid, sid, w["caller_no"]))
            revisit = sid
        conn.execute(f"UPDATE wko_ticket SET status=?, remark=?, {col}=?, revisit_sample_id=? WHERE id=?",
                     (target, remark or w["remark"] or "", ts, revisit, tid))
        conn.execute("COMMIT")
        push_wall()
        return ok({"ticketId": tid, "status": target, "revisitSampleId": revisit},
                  f"工单已{ '办结' if target=='RESOLVED' else '归档' }" +
                  (f"，已生成回访样本 #{revisit} 进入项目1样本池" if target == "CLOSED" and revisit else ""))
    except Exception:
        conn.execute("ROLLBACK")
        raise
    finally:
        conn.close()


@app.post("/api/workorder/{tid}/resolve", tags=["workorder"])
def workorder_resolve(tid: int, body: WkoRemark, user: dict = Depends(require_auth)):
    return _wko_advance(tid, "RESOLVED", user, body.remark)


@app.post("/api/workorder/{tid}/close", tags=["workorder"])
def workorder_close(tid: int, body: WkoRemark, user: dict = Depends(require_auth)):
    return _wko_advance(tid, "CLOSED", user, body.remark)


@app.post("/api/sys/reset", tags=["sys"])
def reset_demo(user: dict = Depends(require_auth)):
    """一键重置演示数据（恢复种子样本池/配额/话务；需要 domainAdmin）"""
    if "domainAdmin" not in user["roles"].split(","):
        return fail("4032", "重置需要 domainAdmin 权限（演示账号 admin / 123456）")
    init_db(force=True)
    TOKENS.clear()
    IVR_CALLS.clear()
    hub.broadcast("reset", {"message": "演示数据已重置，请重新签入"})
    return ok(True, "演示数据已重置：样本池/配额/话务恢复种子状态，所有会话已失效")
