-- NK3C 核心表（SQLite 方言；MySQL 版见 migrations/mysql）
-- 种子 ID 为开发演示小 ID（生产以 Snowflake 应用层生成）
CREATE TABLE sys_user(
  id INTEGER PRIMARY KEY, login_name TEXT UNIQUE, password TEXT, user_name TEXT,
  agent_no TEXT, roles TEXT, status INTEGER DEFAULT 1);
CREATE TABLE sys_param(param_code TEXT PRIMARY KEY, param_value TEXT);
CREATE TABLE prj_project(id INTEGER PRIMARY KEY, project_code TEXT, project_name TEXT,
  status TEXT DEFAULT 'DRAFT', questionnaire_id INTEGER);
CREATE TABLE qnr_questionnaire(id INTEGER PRIMARY KEY, title TEXT, version TEXT, status TEXT);
CREATE TABLE qnr_question(id INTEGER PRIMARY KEY, qnr_id INTEGER, q_no INTEGER, q_type TEXT,
  title TEXT, required INTEGER DEFAULT 1, min_value REAL, max_value REAL);
CREATE TABLE qnr_option(id INTEGER PRIMARY KEY, question_id INTEGER, opt_no INTEGER,
  opt_text TEXT, opt_value TEXT, jump TEXT, jump_target INTEGER);
CREATE TABLE qnr_quota(id INTEGER PRIMARY KEY, qnr_id INTEGER, quota_name TEXT, total_target INTEGER);
CREATE TABLE qnr_quota_cell(id INTEGER PRIMARY KEY, quota_id INTEGER, conditions_json TEXT,
  target_count INTEGER DEFAULT 0, done_count INTEGER DEFAULT 0);
CREATE TABLE smp_sample(id INTEGER PRIMARY KEY, project_id INTEGER, cust_name TEXT, gender TEXT,
  status TEXT DEFAULT 'IDLE', ext_json TEXT, attempts INTEGER DEFAULT 0,
  last_connected_at TEXT, shuffle_key INTEGER, owner_agent_id INTEGER);
CREATE TABLE smp_phone(id INTEGER PRIMARY KEY, sample_id INTEGER, phone_no TEXT,
  sort_no INTEGER DEFAULT 1, valid_flag INTEGER DEFAULT 1);
CREATE TABLE smp_blacklist(id INTEGER PRIMARY KEY, phone_no TEXT UNIQUE, scope TEXT DEFAULT 'GLOBAL', reason TEXT);
CREATE TABLE smp_status_code(code TEXT PRIMARY KEY, name TEXT, category TEXT,
  closes_call INTEGER, reopen_sample INTEGER, hit_black_flag INTEGER);
CREATE TABLE cti_call_record(id INTEGER PRIMARY KEY, project_id INTEGER, sample_id INTEGER,
  agent_id INTEGER, agent_no TEXT, caller_no TEXT, called_no TEXT, status TEXT,
  begin_time TEXT, connect_time TEXT, end_time TEXT, result_code TEXT);
CREATE TABLE ans_sheet(id INTEGER PRIMARY KEY, call_id INTEGER UNIQUE, project_id INTEGER,
  sample_id INTEGER, agent_id INTEGER, qnr_id INTEGER, qnr_version TEXT,
  status TEXT DEFAULT 'DOING', audit_remark TEXT);
CREATE TABLE ans_answer(id INTEGER PRIMARY KEY, sheet_id INTEGER, question_id INTEGER,
  option_ids TEXT, answer_text TEXT, numeric_value REAL, answered_at TEXT,
  UNIQUE(sheet_id, question_id));
CREATE TABLE wko_ticket(id INTEGER PRIMARY KEY, project_id INTEGER, call_id INTEGER,
  caller_no TEXT, subject TEXT, detail TEXT, status TEXT DEFAULT 'PENDING',
  assigned_agent_id INTEGER, priority TEXT DEFAULT 'NORMAL', remark TEXT,
  created_at TEXT, accepted_at TEXT, resolved_at TEXT, closed_at TEXT, revisit_sample_id INTEGER);
CREATE TABLE ivr_flow(id INTEGER PRIMARY KEY, name TEXT, flow_json TEXT, updated_at TEXT);
CREATE TABLE ivr_call_log(id INTEGER PRIMARY KEY, caller_no TEXT, start_time TEXT,
  end_time TEXT, outcome TEXT, path_json TEXT, answers_json TEXT);

-- ── 种子 ──
INSERT INTO sys_user VALUES(1,'admin','123456','系统管理员',NULL,'domainAdmin,orgAdmin,groupAdmin,phoneAdmin',1);
INSERT INTO sys_user VALUES(2,'agent01','123456','坐席演示','1020','phoneAdmin',1);
INSERT INTO sys_user VALUES(3,'sup01','123456','督导演示',NULL,'groupAdmin',1);
INSERT INTO sys_param VALUES('halfyear.days','180');
INSERT INTO sys_param VALUES('redial.max','3');
INSERT INTO sys_param VALUES('predict.abandon.max','3.0');

INSERT INTO prj_project VALUES(1,'P2026-001','客户满意度调查_2026','RUNNING',1);
INSERT INTO qnr_questionnaire VALUES(1,'客户满意度调查_2026','v1.0','PUBLISHED');
INSERT INTO qnr_question VALUES(11,1,1,'single','您的性别是？',1,NULL,NULL);
INSERT INTO qnr_question VALUES(12,1,2,'number','总体而言您打几分？（0-10）',1,0,10);
INSERT INTO qnr_option VALUES(111,11,1,'男','M','NEXT',NULL);
INSERT INTO qnr_option VALUES(112,11,2,'女','F','NEXT',NULL);
INSERT INTO qnr_quota VALUES(1,1,'性别配额',4);
INSERT INTO qnr_quota_cell VALUES(11,1,'[{"questionId":11,"in":[111]}]',2,0);
INSERT INTO qnr_quota_cell VALUES(12,1,'[{"questionId":11,"in":[112]}]',2,0);

INSERT INTO prj_project VALUES(2,'P2026-002','产品偏好调查_2026','RUNNING',2);
INSERT INTO qnr_questionnaire VALUES(2,'产品偏好调查_2026','v1.0','PUBLISHED');
INSERT INTO qnr_question VALUES(21,2,1,'single','您的年龄段？',1,NULL,NULL);
INSERT INTO qnr_question VALUES(22,2,2,'single','是否愿意推荐给朋友？',2,NULL,NULL);
INSERT INTO qnr_option VALUES(211,21,1,'18-30岁','A','NEXT',NULL);
INSERT INTO qnr_option VALUES(212,21,2,'31-45岁','B','NEXT',NULL);
INSERT INTO qnr_option VALUES(213,21,3,'46岁以上','C','NEXT',NULL);
INSERT INTO qnr_option VALUES(221,22,1,'愿意','Y','NEXT',NULL);
INSERT INTO qnr_option VALUES(222,22,2,'不愿意','N','NEXT',NULL);
INSERT INTO qnr_quota VALUES(2,2,'年龄段配额',5);
INSERT INTO qnr_quota_cell VALUES(21,2,'[{"questionId":21,"in":[211]}]',2,0);
INSERT INTO qnr_quota_cell VALUES(22,2,'[{"questionId":21,"in":[212]}]',2,0);
INSERT INTO qnr_quota_cell VALUES(23,2,'[{"questionId":21,"in":[213]}]',1,0);

INSERT INTO smp_sample VALUES(101,1,'张一','男','IDLE',NULL,0,NULL,1, NULL);
INSERT INTO smp_phone VALUES(101,101,'13800000101',1,1);
INSERT INTO smp_sample VALUES(102,1,'李二','女','SUCCESS',NULL,1,'2026-09-01T08:00:00+00:00',2, NULL);
INSERT INTO smp_phone VALUES(102,102,'13800000102',1,1);
INSERT INTO smp_sample VALUES(103,1,'王三','男','IDLE',NULL,0,NULL,3, NULL);
INSERT INTO smp_phone VALUES(103,103,'13800000103',1,1);
INSERT INTO smp_sample VALUES(104,1,'赵四','女','IDLE',NULL,1,'2026-08-20T08:00:00+00:00',4, NULL);
INSERT INTO smp_phone VALUES(104,104,'13800000104',1,1);
INSERT INTO smp_sample VALUES(105,1,'孙五','男','IDLE',NULL,3,NULL,5, NULL);
INSERT INTO smp_phone VALUES(105,105,'13800000105',1,1);
INSERT INTO smp_sample VALUES(106,1,'周六','女','IDLE',NULL,1,NULL,6, NULL);
INSERT INTO smp_phone VALUES(106,106,'13800000106',1,1);
INSERT INTO smp_blacklist VALUES(1,'13800000103','GLOBAL','拒访');
INSERT INTO smp_sample VALUES(201,2,'钱七','男','IDLE',NULL,0,NULL,1, NULL);
INSERT INTO smp_phone VALUES(201,201,'13900000201',1,1);
INSERT INTO smp_sample VALUES(202,2,'吴八','女','SUCCESS',NULL,1,'2026-09-01T08:00:00+00:00',2, NULL);
INSERT INTO smp_phone VALUES(202,202,'13900000202',1,1);
INSERT INTO smp_sample VALUES(203,2,'郑九','男','IDLE',NULL,0,NULL,3, NULL);
INSERT INTO smp_phone VALUES(203,203,'13900000203',1,1);
INSERT INTO smp_sample VALUES(205,2,'冯十','女','IDLE',NULL,3,NULL,5, NULL);
INSERT INTO smp_phone VALUES(205,205,'13900000205',1,1);
INSERT INTO smp_sample VALUES(206,2,'陈一','女','IDLE',NULL,1,NULL,6, NULL);
INSERT INTO smp_phone VALUES(206,206,'13900000206',1,1);
INSERT INTO smp_blacklist VALUES(2,'13900000203','GLOBAL','拒访');

INSERT INTO smp_status_code VALUES('SUCCESS','访问成功','SUCCESS',1,0,0);
INSERT INTO smp_status_code VALUES('PARTIAL','部分完成','NEUTRAL',1,1,0);
INSERT INTO smp_status_code VALUES('QUFAIL','甄别不合格','FAIL',1,0,0);
INSERT INTO smp_status_code VALUES('REFUSE','拒访','FAIL',1,1,1);
INSERT INTO smp_status_code VALUES('BREAKOFF','中途挂断','FAIL',1,1,0);
INSERT INTO smp_status_code VALUES('APPOINT','预约回拨','APPOINT',0,0,0);
INSERT INTO smp_status_code VALUES('NA','无人接听','FAIL',0,1,0);
INSERT INTO smp_status_code VALUES('BUSY','占线','FAIL',0,1,0);
INSERT INTO smp_status_code VALUES('INVALID','空号','FAIL',1,0,1);
INSERT INTO smp_status_code VALUES('FAX','传真/数据线','FAIL',1,0,0);

INSERT INTO ivr_flow VALUES(1,'默认呼入流程','{"entry":"welcome","nodes":[{"id":"welcome","type":"play","text":"欢迎致电 NK3C 客户服务中心","next":"menu"},{"id":"menu","type":"menu","text":"1键参与满意度调研；2键语音留言；0键转人工","branches":{"1":"q1","2":"vm","0":"transfer"}},{"id":"q1","type":"question","text":"请问您的性别？1键男，2键女","options":{"1":"男","2":"女"},"tag":"Q11","next":"q2"},{"id":"q2","type":"question","text":"总体而言您打几分？请按0到9键","options":{"0":"0","1":"1","2":"2","3":"3","4":"4","5":"5","6":"6","7":"7","8":"8","9":"9"},"tag":"Q12","next":"bye"},{"id":"vm","type":"voicemail","text":"请在滴声后留言，按井号键结束","next":"bye"},{"id":"transfer","type":"transfer","text":"正在为您转接人工坐席，请稍候","queue":"MANUAL","next":"bye"},{"id":"bye","type":"end","text":"感谢来电，再见"}]}','2026-09-18T00:00:00+00:00');
