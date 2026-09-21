-- +goose Up
-- 演示/验收种子：生产部署可通过独立初始化流程覆盖；使用 INSERT IGNORE 保证重复执行幂等。
INSERT IGNORE INTO prj_project(id,project_code,project_name,status,questionnaire_id,tenant_id,group_id) VALUES
(1,'P2026-001','客户满意度调查_2026','RUNNING',1,1,1),
(2,'P2026-002','产品偏好调查_2026','RUNNING',2,1,1);
INSERT IGNORE INTO qnr_questionnaire(id,title,version,status) VALUES
(1,'客户满意度调查_2026','v1.0','PUBLISHED'),
(2,'产品偏好调查_2026','v1.0','PUBLISHED');
INSERT IGNORE INTO qnr_question(id,qnr_id,q_no,q_type,title,required,min_value,max_value) VALUES
(11,1,1,'single','您的性别是？',1,NULL,NULL),
(12,1,2,'number','总体而言您打几分？（0-10）',1,0,10),
(21,2,1,'single','您的年龄段？',1,NULL,NULL),
(22,2,2,'single','是否愿意推荐给朋友？',1,NULL,NULL);
INSERT IGNORE INTO qnr_option(id,question_id,opt_no,opt_text,opt_value,jump,jump_target) VALUES
(111,11,1,'男','M','NEXT',NULL),(112,11,2,'女','F','NEXT',NULL),
(211,21,1,'18-30岁','A','NEXT',NULL),(212,21,2,'31-45岁','B','NEXT',NULL),(213,21,3,'46岁以上','C','NEXT',NULL),
(221,22,1,'愿意','Y','NEXT',NULL),(222,22,2,'不愿意','N','NEXT',NULL);
INSERT IGNORE INTO qnr_quota(id,qnr_id,quota_name,total_target) VALUES(1,1,'性别配额',4),(2,2,'年龄段配额',5);
INSERT IGNORE INTO qnr_quota_cell(id,quota_id,conditions_json,target_count,done_count) VALUES
(11,1,'[{"questionId":11,"in":[111]}]',2,0),(12,1,'[{"questionId":11,"in":[112]}]',2,0),
(21,2,'[{"questionId":21,"in":[211]}]',2,0),(22,2,'[{"questionId":21,"in":[212]}]',2,0),(23,2,'[{"questionId":21,"in":[213]}]',1,0);
INSERT IGNORE INTO smp_sample(id,project_id,cust_name,gender,status,attempts,shuffle_key) VALUES
(101,1,'张一','男','IDLE',0,1),(102,1,'李二','女','SUCCESS',1,2),(103,1,'王三','男','IDLE',0,3),(104,1,'赵四','女','IDLE',1,4),(105,1,'孙五','男','IDLE',3,5),(106,1,'周六','女','IDLE',1,6),
(201,2,'钱七','男','IDLE',0,1),(202,2,'吴八','女','SUCCESS',1,2),(203,2,'郑九','男','IDLE',0,3),(205,2,'冯十','女','IDLE',3,5),(206,2,'陈一','女','IDLE',1,6);
INSERT IGNORE INTO smp_phone(id,sample_id,phone_no,sort_no,valid_flag) VALUES
(101,101,'13800000101',1,1),(102,102,'13800000102',1,1),(103,103,'13800000103',1,1),(104,104,'13800000104',1,1),(105,105,'13800000105',1,1),(106,106,'13800000106',1,1),
(201,201,'13900000201',1,1),(202,202,'13900000202',1,1),(203,203,'13900000203',1,1),(205,205,'13900000205',1,1),(206,206,'13900000206',1,1);
INSERT IGNORE INTO smp_blacklist(id,phone_no,scope,reason) VALUES(1,'13800000103','GLOBAL','拒访'),(2,'13900000203','GLOBAL','拒访');
INSERT IGNORE INTO smp_status_code(code,name,category,closes_call,reopen_sample,hit_black_flag) VALUES
('SUCCESS','访问成功','SUCCESS',1,0,0),('PARTIAL','部分完成','NEUTRAL',1,1,0),('QUFAIL','甄别不合格','FAIL',1,0,0),('REFUSE','拒访','FAIL',1,1,1),('BREAKOFF','中途挂断','FAIL',1,1,0),('APPOINT','预约回拨','APPOINT',0,0,0),('NA','无人接听','FAIL',0,1,0),('BUSY','占线','FAIL',0,1,0),('INVALID','空号','FAIL',1,0,1),('FAX','传真/数据线','FAIL',1,0,0);
INSERT IGNORE INTO ivr_flow(id,name,flow_json,updated_at) VALUES(1,'默认呼入流程','{"entry":"welcome","nodes":[{"id":"welcome","type":"play","text":"欢迎致电 NK3C 客户服务中心","next":"menu"},{"id":"menu","type":"menu","text":"1键参与满意度调研；2键语音留言；0键转人工","branches":{"1":"q1","2":"vm","0":"transfer"}},{"id":"q1","type":"question","text":"请问您的性别？1键男，2键女","options":{"1":"男","2":"女"},"tag":"Q11","next":"q2"},{"id":"q2","type":"question","text":"总体而言您打几分？请按0到9键","options":{"0":"0","1":"1","2":"2","3":"3","4":"4","5":"5","6":"6","7":"7","8":"8","9":"9"},"tag":"Q12","next":"bye"},{"id":"vm","type":"voicemail","text":"请在滴声后留言，按井号键结束","next":"bye"},{"id":"transfer","type":"transfer","text":"正在为您转接人工坐席，请稍候","queue":"MANUAL","next":"bye"},{"id":"bye","type":"end","text":"感谢来电，再见"}]}','2026-09-18T00:00:00+00:00');

-- +goose Down
DELETE FROM ivr_flow WHERE id=1;
DELETE FROM smp_status_code WHERE code IN ('SUCCESS','PARTIAL','QUFAIL','REFUSE','BREAKOFF','APPOINT','NA','BUSY','INVALID','FAX');
DELETE FROM smp_blacklist WHERE id IN (1,2);
DELETE FROM smp_phone WHERE id IN (101,102,103,104,105,106,201,202,203,205,206);
DELETE FROM smp_sample WHERE id IN (101,102,103,104,105,106,201,202,203,205,206);
DELETE FROM qnr_quota_cell WHERE id IN (11,12,21,22,23);
DELETE FROM qnr_quota WHERE id IN (1,2);
DELETE FROM qnr_option WHERE id IN (111,112,211,212,213,221,222);
DELETE FROM qnr_question WHERE id IN (11,12,21,22);
DELETE FROM qnr_questionnaire WHERE id IN (1,2);
DELETE FROM prj_project WHERE id IN (1,2);
