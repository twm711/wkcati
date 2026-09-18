# -*- coding: utf-8 -*-
"""
数据库层并发测试：派样锁（FOR UPDATE SKIP LOCKED）与配额原子扣减。
对应测试用例集：并发一致性 4.4#1（双坐席并发派样无重复）、4.4#2（配额原子扣减）。

运行前置（三选一）：
  1. 设置环境变量指向真实 MySQL 并先执行《ITACATI_NK3C_数据库建表.sql》：
     TEST_MYSQL_HOST / TEST_MYSQL_PORT / TEST_MYSQL_USER / TEST_MYSQL_PASS / TEST_MYSQL_DB
  2. docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=nk3c mysql:8.0 后按 1 设置
  3. 未设置环境变量时本文件全部用例自动跳过
"""
import os
import threading

import pytest

pytestmark = pytest.mark.skipif(
    not os.environ.get("TEST_MYSQL_HOST"),
    reason="未设置 TEST_MYSQL_* 环境变量，跳过数据库用例",
)

HOST = os.environ.get("TEST_MYSQL_HOST", "127.0.0.1")
PORT = int(os.environ.get("TEST_MYSQL_PORT", "3306"))
USER = os.environ.get("TEST_MYSQL_USER", "root")
PASS = os.environ.get("TEST_MYSQL_PASS", "nk3c")
DB = os.environ.get("TEST_MYSQL_DB", "nk3c")

SQL_PICK = """
SELECT s.id FROM smp_sample s
JOIN smp_phone p ON p.sample_id = s.id AND p.valid_flag = 1
WHERE s.project_id = %s AND s.status = 'IDLE'
  AND NOT EXISTS (SELECT 1 FROM smp_blacklist b
                  WHERE b.domain_id = s.domain_id AND b.phone_no = p.phone_no
                    AND (b.scope='GLOBAL' OR (b.scope='PROJECT' AND b.project_id=s.project_id)))
  AND s.call_attempts < 3
ORDER BY s.shuffle_key
LIMIT %s FOR UPDATE SKIP LOCKED
"""


@pytest.fixture(scope="module")
def conn():
    pymysql = pytest.importorskip("pymysql", reason="需要 pip install pymysql")
    c = pymysql.connect(host=HOST, port=PORT, user=USER, password=PASS, database=DB, autocommit=False)
    yield c
    c.close()


@pytest.fixture()
def clean_pool(conn):
    """准备 5 条互斥派样测试样本，结束后清理"""
    cur = conn.cursor()
    cur.execute("""
      INSERT INTO smp_sample (id, domain_id, project_id, cust_name, status, shuffle_key, call_attempts)
      VALUES (900001,1,9001,'并发A','IDLE',1,0),(900002,1,9001,'并发B','IDLE',2,0),
             (900003,1,9001,'并发C','IDLE',3,0),(900004,1,9001,'并发D','IDLE',4,0),
             (900005,1,9001,'并发E','IDLE',5,0)
      ON DUPLICATE KEY UPDATE status='IDLE', call_attempts=0
    """)
    for sid in range(900001, 900006):
        cur.execute("INSERT IGNORE INTO smp_phone (sample_id, phone_no, sort_no) VALUES (%s,'1380000%04d',1)", (sid, sid % 10000))
    cur.execute("INSERT IGNORE INTO prj_project (id, domain_id, project_code, project_name, project_type, status, owner_id)"
                " VALUES (9001,1,'T-CONC','并发测试项目','SURVEY','RUNNING',1)")
    conn.commit()
    yield 9001
    cur.execute("DELETE FROM smp_phone WHERE sample_id BETWEEN 900001 AND 900005")
    cur.execute("DELETE FROM smp_sample WHERE id BETWEEN 900001 AND 900005")
    cur.execute("DELETE FROM prj_project WHERE id=9001")
    conn.commit()


class TestDispatchLock:
    def test_two_agents_no_duplicate(self, conn, clean_pool):
        """P0（4.4#1）：两个连接并发取样，结果集无交集"""
        got = [[] for _ in range(2)]
        barrier = threading.Barrier(2)

        def pick(idx):
            import pymysql.cursors
            c2 = pymysql.connect(host=HOST, port=PORT, user=USER, password=PASS, database=DB, autocommit=False)
            try:
                barrier.wait()                      # 两个事务同时起跑
                cur = c2.cursor()
                cur.execute(SQL_PICK, (clean_pool, 2))
                got[idx].extend(r[0] for r in cur.fetchall())
                conn.rollback() if False else None  # 占位：保持事务至读取完成
            finally:
                c2.rollback()
                c2.close()

        t1, t2 = threading.Thread(target=pick, args=(0,)), threading.Thread(target=pick, args=(1,))
        t1.start(); t2.start(); t1.join(); t2.join()
        assert set(got[0]).isdisjoint(set(got[1])), f"出现重复派样: {got}"
        assert len(got[0]) + len(got[1]) == 5

    def test_quota_cell_atomic(self, conn, clean_pool):
        """P0（4.4#2）：并发扣减配额格，done_count 不得超过 target_count"""
        cur = conn.cursor()
        cur.execute("""INSERT INTO qnr_quota (id, qnr_id, quota_name, total_target)
                       VALUES (9001,1,'并发配额',10) ON DUPLICATE KEY UPDATE total_target=10""")
        cur.execute("""INSERT INTO qnr_quota_cell (id, quota_id, conditions_json, target_count, done_count)
                       VALUES (9001,9001,'[]',10,0) ON DUPLICATE KEY UPDATE done_count=0""")
        conn.commit()
        ok_count, lock = [0], threading.Lock()

        def consume():
            c2 = pymysql.connect(host=HOST, port=PORT, user=USER, password=PASS, database=DB, autocommit=True)
            c = c2.cursor()
            for _ in range(20):
                n = c.execute("UPDATE qnr_quota_cell SET done_count=done_count+1 WHERE id=9001 AND done_count<target_count")
                if n:
                    with lock:
                        ok_count[0] += 1
            c2.close()

        ts = [threading.Thread(target=consume) for _ in range(8)]
        for t in ts: t.start()
        for t in ts: t.join()
        cur.execute("SELECT done_count, target_count FROM qnr_quota_cell WHERE id=9001")
        done, target = cur.fetchone()
        cur.execute("DELETE FROM qnr_quota_cell WHERE id=9001")
        cur.execute("DELETE FROM qnr_quota WHERE id=9001")
        conn.commit()
        assert done == 10 and ok_count[0] == 10
