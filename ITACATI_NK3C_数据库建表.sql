-- ============================================================================
--  南康 ITACATI（电访专家）/ NK3C（云联络专家）类 CATI 系统
--  数据库建表脚本（开发蓝本）
-- ----------------------------------------------------------------------------
--  目标库     : MySQL 8.0+（InnoDB / utf8mb4）
--  移植说明   : Oracle / SQL Server 使用等价类型迁移：
--               BIGINT UNSIGNED -> NUMBER(19)/BIGINT
--               DATETIME(3)     -> DATE/TIMESTAMP(3)
--               TINYINT BOOLEAN -> NUMBER(1)/BIT
--               JSON            -> CLOB/NVARCHAR(MAX)（或独立子表）
--  ID 约定    : 与 NK3C 公开规范一致——单主键统一命名 id，18 位精度；
--               传给前端时序列化为 String（JS Number 仅 16 位精度）
--  关联文档   : 《南康科技_ITACATI_NK3C_开发文档.md》
--               《ITACATI_功能细化与全链路深度分析.md》（表-菜单对照见其附录）
-- ============================================================================

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

CREATE DATABASE IF NOT EXISTS nk3c DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
USE nk3c;

-- ============================================================================
-- 01 组织权限域（分权分域：域 domain → 组织 org → 组 group → 员工 user）
--    对应菜单：M02 组织权限；权限模型：domainAdmin/orgAdmin/groupAdmin/phoneAdmin
-- ============================================================================

-- 域（最高隔离单元：分公司/子公司独立部署、数据逻辑分开）
CREATE TABLE org_domain (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键',
  domain_code   VARCHAR(32)  NOT NULL COMMENT '域编码（唯一）',
  domain_name   VARCHAR(128) NOT NULL COMMENT '域名称',
  db_tag        VARCHAR(64)  DEFAULT NULL COMMENT '对应物理库标识（多层数据存储/分发项目用）',
  status        TINYINT      NOT NULL DEFAULT 1 COMMENT '1启用 0停用',
  remark        VARCHAR(255) DEFAULT NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_domain_code (domain_code)
) ENGINE=InnoDB COMMENT='域（分权分域顶层）';

-- 组织（域内部门层级）
CREATE TABLE org_org (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键',
  domain_id     BIGINT UNSIGNED NOT NULL COMMENT '所属域',
  parent_id     BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '上级组织（0=根）',
  org_code      VARCHAR(32)  NOT NULL COMMENT '组织编码',
  org_name      VARCHAR(128) NOT NULL COMMENT '组织名称',
  sort_no       INT          NOT NULL DEFAULT 0,
  status        TINYINT      NOT NULL DEFAULT 1,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_org_domain (domain_id),
  KEY idx_org_parent (parent_id)
) ENGINE=InnoDB COMMENT='组织';

-- 组（坐席组/访问员组/技能组；样本可按组分配）
CREATE TABLE org_group (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键',
  domain_id     BIGINT UNSIGNED NOT NULL COMMENT '所属域',
  org_id        BIGINT UNSIGNED NOT NULL COMMENT '所属组织',
  group_code    VARCHAR(32)  NOT NULL COMMENT '组编码',
  group_name    VARCHAR(128) NOT NULL COMMENT '组名称',
  group_type    VARCHAR(16)  NOT NULL DEFAULT 'AGENT' COMMENT 'AGENT坐席组/INTERVIEWER访问员组/SKILL技能组/QC质检组',
  skill_tags    VARCHAR(255) DEFAULT NULL COMMENT '技能标签（方言等，逗号分隔，影响派样）',
  status        TINYINT      NOT NULL DEFAULT 1,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_group_org (org_id),
  KEY idx_group_domain (domain_id)
) ENGINE=InnoDB COMMENT='组（坐席/访问员/技能组）';

-- 用户（一线坐席 phoneAdmin / 组管理员 / 组织管理员 / 域管理员）
CREATE TABLE sys_user (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '主键',
  domain_id     BIGINT UNSIGNED NOT NULL COMMENT '所属域',
  org_id        BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '所属组织',
  group_id      BIGINT UNSIGNED DEFAULT NULL COMMENT '所属组',
  user_no       VARCHAR(32)  NOT NULL COMMENT '员工编号/坐席工号（工号段内取号，唯一）',
  user_name     VARCHAR(64)  NOT NULL COMMENT '姓名',
  login_name    VARCHAR(64)  NOT NULL COMMENT '登录名（唯一）',
  password_hash VARCHAR(128) NOT NULL COMMENT '密码散列（Shiro 加盐）',
  agent_no      VARCHAR(16)  DEFAULT NULL COMMENT 'CTI 软电话签入工号（NKZXAgent 使用）',
  ext_skills    VARCHAR(255) DEFAULT NULL COMMENT '个人技能（继承组技能之外）',
  status        TINYINT      NOT NULL DEFAULT 1 COMMENT '1启用 0停用 2锁定',
  last_login_at DATETIME(3)  DEFAULT NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_user_login (login_name),
  UNIQUE KEY uk_user_no (domain_id, user_no),
  KEY idx_user_group (group_id)
) ENGINE=InnoDB COMMENT='用户';

-- 角色 / 授权（资源=菜单+按钮+权限，权限按 Code 判断）
CREATE TABLE sys_role (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id     BIGINT UNSIGNED NOT NULL COMMENT '所属域（0=全局角色）',
  role_code     VARCHAR(64)  NOT NULL COMMENT '角色码：domainAdmin/orgAdmin/groupAdmin/phoneAdmin/...',
  role_name     VARCHAR(64)  NOT NULL,
  remark        VARCHAR(255) DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_role (domain_id, role_code)
) ENGINE=InnoDB COMMENT='角色';

CREATE TABLE sys_user_role (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id       BIGINT UNSIGNED NOT NULL,
  role_id       BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_ur (user_id, role_id)
) ENGINE=InnoDB COMMENT='用户-角色';

-- 资源树：类型 MENU=菜单 BUTTON=按钮/操作 API=接口 PERM=权限（橙色，按 Code 判断）
CREATE TABLE sys_resource (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  parent_id     BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '上级资源（0=根）',
  resource_type VARCHAR(16)  NOT NULL COMMENT 'MENU/BUTTON/API/PERM',
  resource_code VARCHAR(64)  NOT NULL COMMENT '资源码（前端按 Code 控权，如 project:create）',
  resource_name VARCHAR(64)  NOT NULL COMMENT '名称（菜单显示名）',
  url           VARCHAR(255) DEFAULT NULL COMMENT '前端路由/页面路径（小写）',
  icon          VARCHAR(64)  DEFAULT NULL,
  sort_no       INT          NOT NULL DEFAULT 0,
  status        TINYINT      NOT NULL DEFAULT 1,
  PRIMARY KEY (id),
  UNIQUE KEY uk_res_code (resource_code),
  KEY idx_res_parent (parent_id)
) ENGINE=InnoDB COMMENT='资源树（菜单/按钮/权限）';

CREATE TABLE sys_role_resource (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  role_id       BIGINT UNSIGNED NOT NULL,
  resource_id   BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_rr (role_id, resource_id)
) ENGINE=InnoDB COMMENT='角色-资源授权';

CREATE TABLE sys_login_log (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id       BIGINT UNSIGNED NOT NULL,
  login_at      DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  ip            VARCHAR(64)  DEFAULT NULL,
  result        TINYINT      NOT NULL DEFAULT 1 COMMENT '1成功 0失败',
  detail        VARCHAR(255) DEFAULT NULL,
  PRIMARY KEY (id),
  KEY idx_login_user (user_id, login_at)
) ENGINE=InnoDB COMMENT='登录日志';

CREATE TABLE sys_op_log (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id       BIGINT UNSIGNED NOT NULL,
  op_at         DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  module        VARCHAR(64)  NOT NULL COMMENT '模块码（如 M03 项目管理）',
  action        VARCHAR(64)  NOT NULL COMMENT '动作码（如 project:start）',
  biz_id        VARCHAR(64)  DEFAULT NULL COMMENT '业务主键',
  detail        TEXT         DEFAULT NULL COMMENT '变更前后 JSON',
  ip            VARCHAR(64)  DEFAULT NULL,
  PRIMARY KEY (id),
  KEY idx_op_user (user_id, op_at),
  KEY idx_op_module (module, op_at)
) ENGINE=InnoDB COMMENT='操作审计日志';

-- ============================================================================
-- 02 项目域（对应菜单：M03 项目管理；支持父子项目/分发项目）
-- ============================================================================

CREATE TABLE prj_project (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '项目ID',
  domain_id          BIGINT UNSIGNED NOT NULL COMMENT '所属域',
  project_code       VARCHAR(32)  NOT NULL COMMENT '项目编号（唯一）',
  project_name       VARCHAR(128) NOT NULL COMMENT '项目名称',
  project_type       VARCHAR(16)  NOT NULL COMMENT 'SURVEY调查/MARKETING营销/CALLBACK回访/INBOUND呼入',
  parent_project_id  BIGINT UNSIGNED DEFAULT NULL COMMENT '母项目ID（父子项目/分发项目）',
  questionnaire_id   BIGINT UNSIGNED DEFAULT NULL COMMENT '绑定问卷',
  strategy_id        BIGINT UNSIGNED DEFAULT NULL COMMENT '绑定呼叫策略',
  start_time         DATETIME(3)  DEFAULT NULL COMMENT '计划开始',
  end_time           DATETIME(3)  DEFAULT NULL COMMENT '计划结束',
  daily_begin        TIME         DEFAULT NULL COMMENT '每日可呼开始（项目时间段）',
  daily_end          TIME         DEFAULT NULL COMMENT '每日可呼结束',
  quota_type         VARCHAR(16)  DEFAULT 'NONE' COMMENT 'NONE无/SINGLE单重/CROSS交叉',
  status             VARCHAR(16)  NOT NULL DEFAULT 'DRAFT' COMMENT 'DRAFT草稿/TESTING试访/RUNNING执行中/PAUSED暂停/FINISHED已结束/ARCHIVED已归档',
  is_template        TINYINT      NOT NULL DEFAULT 0 COMMENT '1=项目模板',
  owner_id           BIGINT UNSIGNED NOT NULL COMMENT '项目经理',
  remark             VARCHAR(500) DEFAULT NULL,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_prj_code (domain_id, project_code),
  KEY idx_prj_parent (parent_project_id),
  KEY idx_prj_status (status),
  CONSTRAINT chk_prj_status CHECK (status IN ('DRAFT','TESTING','RUNNING','PAUSED','FINISHED','ARCHIVED'))
) ENGINE=InnoDB COMMENT='项目';

-- 分发项目（母项目 → 子数据库/子公司）
CREATE TABLE prj_distribution (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  project_id         BIGINT UNSIGNED NOT NULL COMMENT '母项目',
  target_db_tag      VARCHAR(64)  NOT NULL COMMENT '目标子库标识（org_domain.db_tag）',
  sub_project_id     BIGINT UNSIGNED DEFAULT NULL COMMENT '生成的子项目ID',
  dist_status        VARCHAR(16)  NOT NULL DEFAULT 'PENDING' COMMENT 'PENDING/DISTRIBUTED/FAILED',
  dispatched_at      DATETIME(3)  DEFAULT NULL,
  dispatched_by      BIGINT UNSIGNED DEFAULT NULL,
  PRIMARY KEY (id),
  KEY idx_dist_project (project_id)
) ENGINE=InnoDB COMMENT='项目分发记录';

-- 项目-坐席组分配
CREATE TABLE prj_project_group (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  project_id         BIGINT UNSIGNED NOT NULL,
  group_id           BIGINT UNSIGNED NOT NULL COMMENT '承接访问的坐席/访问员组',
  priority           INT          NOT NULL DEFAULT 0 COMMENT '派样优先级',
  PRIMARY KEY (id),
  UNIQUE KEY uk_pg (project_id, group_id)
) ENGINE=InnoDB COMMENT='项目-坐席组分配';

-- 排班（项目时间段 × 组 × 日期班次）
CREATE TABLE prj_schedule (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  project_id         BIGINT UNSIGNED NOT NULL,
  group_id           BIGINT UNSIGNED NOT NULL,
  shift_date         DATE         NOT NULL COMMENT '班次日期',
  begin_time         TIME         NOT NULL,
  end_time           TIME         NOT NULL,
  plan_agents        INT          NOT NULL DEFAULT 0 COMMENT '计划坐席数',
  PRIMARY KEY (id),
  KEY idx_sch (project_id, shift_date)
) ENGINE=InnoDB COMMENT='项目排班表';

-- ============================================================================
-- 03 问卷域（对应菜单：M04 问卷管理；20 种题型见文档第 9 章）
-- ============================================================================

CREATE TABLE qnr_questionnaire (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '问卷ID',
  domain_id          BIGINT UNSIGNED NOT NULL,
  title              VARCHAR(255) NOT NULL COMMENT '问卷标题',
  version            VARCHAR(16)  NOT NULL DEFAULT 'v1.0' COMMENT '版本号（发布一次递增）',
  status             VARCHAR(16)  NOT NULL DEFAULT 'DRAFT' COMMENT 'DRAFT/TESTING/PUBLISHED/OFFLINE',
  est_minutes        DECIMAL(5,1) NOT NULL DEFAULT 0 COMMENT '预估时长（分钟）',
  page_style         JSON         DEFAULT NULL COMMENT '页面风格（字体/配色，支持整体与单题设置）',
  err_msg_style      JSON         DEFAULT NULL COMMENT '自定义报错提示文案配置',
  created_by         BIGINT UNSIGNED NOT NULL,
  published_at       DATETIME(3)  DEFAULT NULL,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_qnr_domain (domain_id, status)
) ENGINE=InnoDB COMMENT='问卷（脚本）';

CREATE TABLE qnr_question (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '题目ID',
  qnr_id             BIGINT UNSIGNED NOT NULL COMMENT '所属问卷',
  q_no               INT          NOT NULL COMMENT '题号（顺序号）',
  q_type             VARCHAR(16)  NOT NULL COMMENT 'single/multi/sort/text/number/date/scale/assign/matrix/matrix3d/order_multi/dummy/calc/info/screen/repeat/appt/endstat 等 20 种',
  title              VARCHAR(1000) NOT NULL COMMENT '题干',
  required           TINYINT      NOT NULL DEFAULT 1 COMMENT '必答',
  random_flag        TINYINT      NOT NULL DEFAULT 0 COMMENT '选项随机/轮换/倒序',
  classify_flag      TINYINT      NOT NULL DEFAULT 0 COMMENT '选项分类展示',
  min_value          DECIMAL(12,4) DEFAULT NULL COMMENT '数值题下限/分配题总和校验',
  max_value          DECIMAL(12,4) DEFAULT NULL,
  max_len            INT          DEFAULT NULL COMMENT '开放题最大字数',
  scale_max          INT          DEFAULT NULL COMMENT '刻度题最大刻度',
  rows_json          JSON         DEFAULT NULL COMMENT '矩阵题行定义',
  cols_json          JSON         DEFAULT NULL COMMENT '矩阵题列定义',
  formula            TEXT         DEFAULT NULL COMMENT '辅助计算题公式（哑题/计算题）',
  media_json         JSON         DEFAULT NULL COMMENT '题干多媒体（图片/音频，ITACAPI 共用引擎）',
  font_style         JSON         DEFAULT NULL COMMENT '单题字体设置',
  sort_no            INT          NOT NULL DEFAULT 0,
  status             TINYINT      NOT NULL DEFAULT 1,
  PRIMARY KEY (id),
  UNIQUE KEY uk_q (qnr_id, q_no),
  KEY idx_q_qnr (qnr_id)
) ENGINE=InnoDB COMMENT='题目';

CREATE TABLE qnr_option (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '选项ID',
  question_id        BIGINT UNSIGNED NOT NULL COMMENT '所属题目',
  opt_no             INT          NOT NULL COMMENT '选项序号',
  opt_text           VARCHAR(500) NOT NULL COMMENT '选项文本',
  opt_value          VARCHAR(64)  DEFAULT NULL COMMENT '选项编码（导出变量值）',
  jump_type          VARCHAR(16)  NOT NULL DEFAULT 'NEXT' COMMENT 'NEXT下一题/QID跳指定题/END结束访问',
  jump_question_id   BIGINT UNSIGNED DEFAULT NULL COMMENT '跳转目标题目',
  exclusive_flag     TINYINT      NOT NULL DEFAULT 0 COMMENT '互斥（排他项）',
  associate_expr     VARCHAR(500) DEFAULT NULL COMMENT '选项关联表达式（随前题过滤显示）',
  media_json         JSON         DEFAULT NULL COMMENT '选项图片/多媒体',
  PRIMARY KEY (id),
  KEY idx_opt_q (question_id)
) ENGINE=InnoDB COMMENT='选项';

-- 高级逻辑：显示/跳转/校验（多维条件约束）
CREATE TABLE qnr_logic (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  qnr_id             BIGINT UNSIGNED NOT NULL,
  src_question_id    BIGINT UNSIGNED NOT NULL COMMENT '逻辑主体题目',
  logic_type         VARCHAR(16)  NOT NULL COMMENT 'SHOW条件显示/JUMP条件跳转/VALIDATE校验/CALC计算',
  expr_text          TEXT         NOT NULL COMMENT '表达式（如 Q3>7 AND Q4 CONTAINS 2）',
  target_question_id BIGINT UNSIGNED DEFAULT NULL COMMENT '目标题（JUMP 用）',
  err_msg            VARCHAR(255) DEFAULT NULL COMMENT '校验失败提示（自定义报错）',
  sort_no            INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_logic_qnr (qnr_id)
) ENGINE=InnoDB COMMENT='问卷逻辑规则';

-- 配额（单重/交叉；执行期实时扣减）
CREATE TABLE qnr_quota (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  qnr_id             BIGINT UNSIGNED NOT NULL COMMENT '配额挂接问卷',
  quota_name         VARCHAR(128) NOT NULL,
  total_target       INT          NOT NULL DEFAULT 0 COMMENT '样本总量',
  PRIMARY KEY (id),
  KEY idx_quota_qnr (qnr_id)
) ENGINE=InnoDB COMMENT='配额组';

CREATE TABLE qnr_quota_cell (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  quota_id           BIGINT UNSIGNED NOT NULL,
  conditions_json    JSON         NOT NULL COMMENT '命中条件（如 [{qid:Q2,in:[男]},{qid:Q4,in:[APP]}]）',
  target_count       INT          NOT NULL DEFAULT 0 COMMENT '该格目标量',
  done_count         INT          NOT NULL DEFAULT 0 COMMENT '已完成（原子累加）',
  overflow_flag      TINYINT      NOT NULL DEFAULT 0 COMMENT '超配额是否放行',
  PRIMARY KEY (id),
  KEY idx_qc (quota_id)
) ENGINE=InnoDB COMMENT='配额单元（交叉格）';

CREATE TABLE qnr_template (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  name               VARCHAR(128) NOT NULL COMMENT '问卷模板名',
  content_json       JSON         NOT NULL COMMENT '模板结构',
  PRIMARY KEY (id)
) ENGINE=InnoDB COMMENT='问卷模板';

CREATE TABLE qnr_bank (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  category           VARCHAR(64)  DEFAULT NULL COMMENT '题库分类',
  title              VARCHAR(500) NOT NULL COMMENT '题目文本',
  content_json       JSON         NOT NULL COMMENT '完整题目结构（拖选使用）',
  usage_count        INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_bank (domain_id, category)
) ENGINE=InnoDB COMMENT='常用题库';

-- 问卷导入导出（doc/xml/sur 等多格式）
CREATE TABLE qnr_import_export_log (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  qnr_id             BIGINT UNSIGNED DEFAULT NULL,
  op_type            VARCHAR(16)  NOT NULL COMMENT 'IMPORT/EXPORT/PRINT',
  file_format        VARCHAR(16)  NOT NULL COMMENT 'DOC/XML/SUR/PDF',
  file_path          VARCHAR(500) DEFAULT NULL,
  operator_id        BIGINT UNSIGNED NOT NULL,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id)
) ENGINE=InnoDB COMMENT='问卷导入导出日志';

-- ============================================================================
-- 04 样本域（对应菜单：M05 样本管理）
--    规则：黑名单、半年原则、样本打散、一人多号、自定义状态码、分级分配
-- ============================================================================

CREATE TABLE smp_sample (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '样本ID',
  domain_id          BIGINT UNSIGNED NOT NULL,
  project_id         BIGINT UNSIGNED DEFAULT NULL COMMENT '进入的项目（池内未分配为 NULL）',
  import_batch_no    VARCHAR(32)  DEFAULT NULL COMMENT '导入批次',
  cust_name          VARCHAR(64)  DEFAULT NULL COMMENT '客户/被访者姓名',
  gender             VARCHAR(8)   DEFAULT NULL COMMENT '性别',
  age                INT          DEFAULT NULL,
  province           VARCHAR(32)  DEFAULT NULL,
  city               VARCHAR(32)  DEFAULT NULL,
  region_tag         VARCHAR(64)  DEFAULT NULL COMMENT '区域/分层标签（抽样层）',
  level              VARCHAR(16)  DEFAULT 'NORMAL' COMMENT '样本分级（VIP/NORMAL/...）',
  status             VARCHAR(16)  NOT NULL DEFAULT 'IDLE' COMMENT 'IDLE待呼/ASSIGNED已派/INCALL访问中/SUCCESS成功/FAIL失败/APPOINT预约/BANNED禁呼/RECYCLED回收',
  owner_group_id     BIGINT UNSIGNED DEFAULT NULL COMMENT '当前分配访问员组',
  owner_agent_id     BIGINT UNSIGNED DEFAULT NULL COMMENT '当前占用坐席（派样锁）',
  assigned_at        DATETIME(3)  DEFAULT NULL,
  last_called_at     DATETIME(3)  DEFAULT NULL COMMENT '最后呼叫时间（半年原则判断基准）',
  call_attempts      INT          NOT NULL DEFAULT 0 COMMENT '已呼叫次数（重拨上限判断）',
  ext_json           JSON         DEFAULT NULL COMMENT '自定义字段值（对应 smp_field_def）',
  shuffle_key        INT          NOT NULL DEFAULT 0 COMMENT '打散排序键（样本打散）',
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_smp_project (project_id, status),
  KEY idx_smp_group (owner_group_id, status),
  KEY idx_smp_batch (import_batch_no),
  KEY idx_smp_lastcall (last_called_at)
) ENGINE=InnoDB COMMENT='样本';

-- 一人多号（同一样本多号码，按 sort_no 顺序呼叫）
CREATE TABLE smp_phone (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  sample_id          BIGINT UNSIGNED NOT NULL,
  phone_no           VARCHAR(32)  NOT NULL COMMENT '号码',
  phone_type         VARCHAR(16)  DEFAULT 'MOBILE' COMMENT 'MOBILE/LANDLINE/OFFICE',
  sort_no            INT          NOT NULL DEFAULT 1 COMMENT '呼叫顺序（一人多号排序）',
  valid_flag         TINYINT      NOT NULL DEFAULT 1 COMMENT '0=空号/传真等系统自动标记',
  last_result_code   VARCHAR(32)  DEFAULT NULL COMMENT '该号最近一次结果码',
  PRIMARY KEY (id),
  KEY idx_phone_sample (sample_id),
  KEY idx_phone_no (phone_no)
) ENGINE=InnoDB COMMENT='样本电话号码（一人多号）';

-- 自定义样本字段定义（自定义字段 + 字典表）
CREATE TABLE smp_field_def (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  field_code         VARCHAR(64)  NOT NULL COMMENT '字段编码（映射 ext_json.key）',
  field_name         VARCHAR(64)  NOT NULL,
  field_type         VARCHAR(16)  NOT NULL DEFAULT 'TEXT' COMMENT 'TEXT/NUMBER/DATE/DICT',
  dict_code          VARCHAR(64)  DEFAULT NULL COMMENT '关联字典（field_type=DICT）',
  required           TINYINT      NOT NULL DEFAULT 0,
  sort_no            INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_field (domain_id, field_code)
) ENGINE=InnoDB COMMENT='样本自定义字段定义';

CREATE TABLE smp_dict (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  dict_code          VARCHAR(64)  NOT NULL COMMENT '字典编码',
  item_value         VARCHAR(64)  NOT NULL COMMENT '项值',
  item_label         VARCHAR(128) NOT NULL COMMENT '项显示名',
  parent_id          BIGINT UNSIGNED DEFAULT NULL COMMENT '上级项（级联字典）',
  sort_no            INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_dict (domain_id, dict_code)
) ENGINE=InnoDB COMMENT='数据字典';

CREATE TABLE smp_blacklist (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  phone_no           VARCHAR(32)  NOT NULL COMMENT '禁呼号码',
  scope              VARCHAR(16)  NOT NULL DEFAULT 'GLOBAL' COMMENT 'GLOBAL全局/PROJECT项目级',
  project_id         BIGINT UNSIGNED DEFAULT NULL,
  reason             VARCHAR(255) DEFAULT NULL COMMENT '原因（拒访/投诉/法规）',
  created_by         BIGINT UNSIGNED NOT NULL,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_black (domain_id, phone_no, scope, project_id)
) ENGINE=InnoDB COMMENT='黑名单';

-- 电话结束状态码（每个电话结束状态可自定义）
CREATE TABLE smp_status_code (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  code               VARCHAR(32)  NOT NULL COMMENT '状态码（如 NA/BUSY/SUCCESS）',
  name               VARCHAR(64)  NOT NULL COMMENT '名称（无人接听/占线/成功）',
  category           VARCHAR(16)  NOT NULL COMMENT 'SUCCESS成功/FAIL失败/NEUTRAL中性/APPOINT预约',
  closed_flag        TINYINT      NOT NULL DEFAULT 1 COMMENT '1=闭环（样本不再回池）；0=可重拨',
  allow_redial       TINYINT      NOT NULL DEFAULT 0 COMMENT '允许重拨',
  hit_black_flag     TINYINT      NOT NULL DEFAULT 0 COMMENT '命中后自动加入黑名单（如空号/拒访）',
  sort_no            INT          NOT NULL DEFAULT 0,
  status             TINYINT      NOT NULL DEFAULT 1,
  PRIMARY KEY (id),
  UNIQUE KEY uk_status_code (domain_id, code)
) ENGINE=InnoDB COMMENT='电话结束状态码表';

-- 样本分配/回收流水
CREATE TABLE smp_assign_log (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  sample_id          BIGINT UNSIGNED NOT NULL,
  project_id         BIGINT UNSIGNED NOT NULL,
  group_id           BIGINT UNSIGNED DEFAULT NULL,
  agent_id           BIGINT UNSIGNED DEFAULT NULL,
  action             VARCHAR(16)  NOT NULL COMMENT 'ASSIGN分配/RETURN回收/REDIAL重拨池/BAN禁呼',
  assigned_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  returned_at        DATETIME(3)  DEFAULT NULL,
  PRIMARY KEY (id),
  KEY idx_assign (sample_id, assigned_at),
  KEY idx_assign_project (project_id, assigned_at)
) ENGINE=InnoDB COMMENT='样本分配流水';

-- ============================================================================
-- 05 呼叫策略域（对应菜单：M06 呼叫策略）
--    模式：PREDICTIVE智能预测/PREVIEW预览/AUTO自动/MIXED混合；VIP 语音方案
-- ============================================================================

CREATE TABLE strat_strategy (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '策略ID',
  domain_id          BIGINT UNSIGNED NOT NULL,
  project_id         BIGINT UNSIGNED NOT NULL COMMENT '所属项目',
  strategy_name      VARCHAR(128) NOT NULL,
  call_mode          VARCHAR(16)  NOT NULL DEFAULT 'PREVIEW' COMMENT 'PREDICTIVE/PREVIEW/AUTO/MIXED',
  work_order_mode    TINYINT      NOT NULL DEFAULT 0 COMMENT '工单模式（NK3C 呼入工单联动）',
  max_abandon_rate   DECIMAL(5,2) NOT NULL DEFAULT 3.00 COMMENT '呼损率上限 %（预测外呼控制目标）',
  dial_ratio         DECIMAL(6,2) NOT NULL DEFAULT 1.50 COMMENT '拨号倍率（预测算法初值，运行期自调整）',
  redial_max         INT          NOT NULL DEFAULT 3 COMMENT '同一号码最大重拨次数',
  redial_interval_min INT         NOT NULL DEFAULT 60 COMMENT '重拨最小间隔（分钟）',
  caller_no          VARCHAR(32)  DEFAULT NULL COMMENT '项目主叫号码显示',
  per_agent_caller   TINYINT      NOT NULL DEFAULT 0 COMMENT '1=允许为访问员单独设置主叫号码',
  vip_mode           TINYINT      NOT NULL DEFAULT 0 COMMENT 'VIP 语音方案（复述确认/合成语音）',
  status             TINYINT      NOT NULL DEFAULT 1,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_strat_project (project_id)
) ENGINE=InnoDB COMMENT='呼叫策略';

CREATE TABLE strat_time_window (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  strategy_id        BIGINT UNSIGNED NOT NULL,
  weekdays           VARCHAR(16)  NOT NULL DEFAULT '1,2,3,4,5' COMMENT '允许呼叫的星期（1=周一）',
  begin_time         TIME         NOT NULL,
  end_time           TIME         NOT NULL,
  PRIMARY KEY (id),
  KEY idx_tw (strategy_id)
) ENGINE=InnoDB COMMENT='策略允许呼叫时段';

CREATE TABLE strat_caller_assign (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  strategy_id        BIGINT UNSIGNED NOT NULL,
  agent_id           BIGINT UNSIGNED NOT NULL COMMENT '访问员',
  caller_no          VARCHAR(32)  NOT NULL COMMENT '该坐席外呼主显号码',
  PRIMARY KEY (id),
  UNIQUE KEY uk_caller (strategy_id, agent_id)
) ENGINE=InnoDB COMMENT='坐席主叫号码分配';

-- ============================================================================
-- 06 话务 CTI 域（对应菜单：M07 执行管理 / M08 实时监控）
-- ============================================================================

CREATE TABLE cti_call_record (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '话务记录ID',
  domain_id          BIGINT UNSIGNED NOT NULL,
  project_id         BIGINT UNSIGNED DEFAULT NULL COMMENT '项目（呼入可为空）',
  strategy_id        BIGINT UNSIGNED DEFAULT NULL,
  sample_id          BIGINT UNSIGNED DEFAULT NULL COMMENT '关联样本（呼入可按主叫匹配）',
  agent_id           BIGINT UNSIGNED DEFAULT NULL COMMENT '应答坐席（无人接听类为 NULL）',
  call_direction     VARCHAR(8)   NOT NULL DEFAULT 'OUT' COMMENT 'OUT外呼/IN呼入',
  caller_no          VARCHAR(32)  NOT NULL COMMENT '主叫',
  called_no          VARCHAR(32)  NOT NULL COMMENT '被叫',
  begin_time         DATETIME(3)  DEFAULT NULL COMMENT '呼叫发起',
  connect_time       DATETIME(3)  DEFAULT NULL COMMENT '接通时间',
  end_time           DATETIME(3)  DEFAULT NULL COMMENT '结束时间',
  ring_duration_sec  INT          NOT NULL DEFAULT 0 COMMENT '振铃时长',
  talk_duration_sec  INT          NOT NULL DEFAULT 0 COMMENT '通话时长',
  result_code        VARCHAR(32)  DEFAULT NULL COMMENT '结束状态码（smp_status_code.code）',
  transfer_flag      TINYINT      NOT NULL DEFAULT 0 COMMENT '是否转接',
  listened_flag      TINYINT      NOT NULL DEFAULT 0 COMMENT '是否被监听过',
  recording_id       BIGINT UNSIGNED DEFAULT NULL COMMENT '录音ID',
  call_seq           VARCHAR(48)  DEFAULT NULL COMMENT 'CTI 平台话务唯一标识（CTI Proxy 映射）',
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_call_project (project_id, begin_time),
  KEY idx_call_agent (agent_id, begin_time),
  KEY idx_call_sample (sample_id),
  KEY idx_call_result (result_code)
) ENGINE=InnoDB COMMENT='话务记录（每通电话一条）';

CREATE TABLE cti_recording (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  call_id            BIGINT UNSIGNED NOT NULL COMMENT '话务记录',
  file_path          VARCHAR(500) NOT NULL COMMENT '录音文件路径/对象存储键',
  file_format        VARCHAR(8)   NOT NULL DEFAULT 'wav' COMMENT 'wav/mp3',
  file_size          BIGINT       NOT NULL DEFAULT 0,
  duration_sec       INT          NOT NULL DEFAULT 0,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_rec_call (call_id)
) ENGINE=InnoDB COMMENT='通话录音';

-- 坐席状态流水（签入/示闲/通话/后处理/示忙/签出）
CREATE TABLE cti_agent_state_log (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  agent_id           BIGINT UNSIGNED NOT NULL,
  state              VARCHAR(16)  NOT NULL COMMENT 'LOGIN签入/READY示闲/BUSY通话/ACW后处理/AUX示忙/LOGOUT签出',
  reason             VARCHAR(64)  DEFAULT NULL COMMENT '示忙原因码',
  begin_time         DATETIME(3)  NOT NULL,
  end_time           DATETIME(3)  DEFAULT NULL,
  PRIMARY KEY (id),
  KEY idx_asl_agent (agent_id, begin_time)
) ENGINE=InnoDB COMMENT='坐席状态流水';

-- 督导实时操作（监听/插话/强制示忙/示闲/挂断/签出/发消息）
CREATE TABLE cti_monitor_event (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  call_id            BIGINT UNSIGNED DEFAULT NULL COMMENT '关联话务（监听/插话/挂断）',
  monitor_id         BIGINT UNSIGNED NOT NULL COMMENT '督导',
  target_agent_id    BIGINT UNSIGNED DEFAULT NULL COMMENT '目标坐席',
  action             VARCHAR(16)  NOT NULL COMMENT 'LISTEN监听/BARGE插话/FORCEBUSY/FORCEIDLE/FORCEHANGUP/FORCELOGOUT/MESSAGE发消息',
  message_text       VARCHAR(500) DEFAULT NULL COMMENT 'MESSAGE 内容',
  event_time         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_me_target (target_agent_id, event_time)
) ENGINE=InnoDB COMMENT='督导实时操作事件';

-- ============================================================================
-- 07 答卷域（对应菜单：M09 答卷管理；审核/编码/预约）
-- ============================================================================

CREATE TABLE ans_sheet (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '答卷ID',
  domain_id          BIGINT UNSIGNED NOT NULL,
  call_id            BIGINT UNSIGNED NOT NULL COMMENT '来源话务',
  project_id         BIGINT UNSIGNED NOT NULL,
  sample_id          BIGINT UNSIGNED NOT NULL,
  agent_id           BIGINT UNSIGNED NOT NULL COMMENT '作答坐席',
  qnr_id             BIGINT UNSIGNED NOT NULL COMMENT '问卷（含版本）',
  qnr_version        VARCHAR(16)  DEFAULT NULL COMMENT '作答时问卷版本（中途调整问卷的关键）',
  begin_time         DATETIME(3)  NOT NULL,
  submit_time        DATETIME(3)  DEFAULT NULL,
  duration_sec       INT          NOT NULL DEFAULT 0 COMMENT '答卷用时',
  status             VARCHAR(16)  NOT NULL DEFAULT 'DOING' COMMENT 'DOING作答中/SUBMITTED已提交/AUDITED审核通过/REJECTED驳回/VOID作废',
  audit_by           BIGINT UNSIGNED DEFAULT NULL,
  audit_at           DATETIME(3)  DEFAULT NULL,
  audit_remark       VARCHAR(500) DEFAULT NULL,
  audit_score        DECIMAL(6,2) DEFAULT NULL COMMENT '审核指标得分（NK3C 答卷审核指标）',
  PRIMARY KEY (id),
  KEY idx_sheet_project (project_id, status),
  KEY idx_sheet_call (call_id),
  KEY idx_sheet_agent (agent_id, submit_time)
) ENGINE=InnoDB COMMENT='答卷（一通成功电话一份）';

CREATE TABLE ans_answer (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  sheet_id           BIGINT UNSIGNED NOT NULL,
  question_id        BIGINT UNSIGNED NOT NULL COMMENT '题目（qnr_question.id）',
  q_no               INT          NOT NULL COMMENT '题号快照',
  answer_text        TEXT         DEFAULT NULL COMMENT '开放题文本',
  option_ids         JSON         DEFAULT NULL COMMENT '选中选项 id 数组',
  option_values      JSON         DEFAULT NULL COMMENT '选项编码快照（导出用）',
  numeric_value      DECIMAL(12,4) DEFAULT NULL COMMENT '数字/刻度/分配值',
  sort_values        JSON         DEFAULT NULL COMMENT '排序值',
  matrix_values      JSON         DEFAULT NULL COMMENT '矩阵/三维表格值',
  answered_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_ans (sheet_id, question_id),
  KEY idx_ans_question (question_id)
) ENGINE=InnoDB COMMENT='答卷明细（每题一条）';

-- 开放题编码（机编候选 + 人工赋码）
CREATE TABLE ans_code (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  sheet_id           BIGINT UNSIGNED NOT NULL,
  question_id        BIGINT UNSIGNED NOT NULL,
  code               VARCHAR(32)  NOT NULL COMMENT '码值',
  code_name          VARCHAR(128) DEFAULT NULL COMMENT '码含义',
  code_source        VARCHAR(16)  NOT NULL DEFAULT 'MANUAL' COMMENT 'AUTO机器编码/MANUAL人工',
  coder_id           BIGINT UNSIGNED DEFAULT NULL,
  coded_at           DATETIME(3)  DEFAULT NULL,
  PRIMARY KEY (id),
  KEY idx_code (sheet_id, question_id)
) ENGINE=InnoDB COMMENT='开放题编码';

-- 预约回拨
CREATE TABLE ans_appointment (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  project_id         BIGINT UNSIGNED NOT NULL,
  sample_id          BIGINT UNSIGNED NOT NULL,
  agent_id           BIGINT UNSIGNED DEFAULT NULL COMMENT '预约给谁（可空=任意坐席）',
  phone_no           VARCHAR(32)  DEFAULT NULL COMMENT '指定回拨号码',
  appt_time          DATETIME(3)  NOT NULL COMMENT '预约时间',
  status             VARCHAR(16)  NOT NULL DEFAULT 'WAITING' COMMENT 'WAITING/DONE/CANCELED/EXPIRED',
  remark             VARCHAR(255) DEFAULT NULL,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  done_at            DATETIME(3)  DEFAULT NULL,
  PRIMARY KEY (id),
  KEY idx_appt_time (appt_time, status)
) ENGINE=InnoDB COMMENT='预约回拨';

-- ============================================================================
-- 08 质检域（对应菜单：M12 质检管理）
-- ============================================================================

CREATE TABLE qc_template (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  name               VARCHAR(128) NOT NULL COMMENT '评分表名',
  total_score        DECIMAL(6,2) NOT NULL DEFAULT 100,
  status             TINYINT      NOT NULL DEFAULT 1,
  PRIMARY KEY (id)
) ENGINE=InnoDB COMMENT='质检评分表模板';

CREATE TABLE qc_template_item (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  template_id        BIGINT UNSIGNED NOT NULL,
  item_name          VARCHAR(128) NOT NULL COMMENT '评分项（开场白规范/业务准确...）',
  max_score          DECIMAL(6,2) NOT NULL,
  weight             DECIMAL(6,2) NOT NULL DEFAULT 0,
  defect_flag        TINYINT      NOT NULL DEFAULT 0 COMMENT '1=致命缺陷项（一票否决）',
  sort_no            INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_qti (template_id)
) ENGINE=InnoDB COMMENT='评分表条目';

CREATE TABLE qc_task (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  task_name          VARCHAR(128) NOT NULL,
  project_id         BIGINT UNSIGNED DEFAULT NULL,
  sample_rule        VARCHAR(16)  NOT NULL DEFAULT 'RATE' COMMENT 'RATE按比例/AGENT指定坐席/MANUAL手工',
  rate               DECIMAL(5,2) DEFAULT NULL COMMENT '抽检比例 %',
  target_scope_json  JSON         DEFAULT NULL COMMENT '指定坐席/时间范围',
  status             VARCHAR(16)  NOT NULL DEFAULT 'OPEN' COMMENT 'OPEN/CLOSED',
  created_by         BIGINT UNSIGNED NOT NULL,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id)
) ENGINE=InnoDB COMMENT='质检任务';

CREATE TABLE qc_result (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  task_id            BIGINT UNSIGNED NOT NULL,
  call_id            BIGINT UNSIGNED NOT NULL COMMENT '被质检话务（录音质检）',
  sheet_id           BIGINT UNSIGNED DEFAULT NULL COMMENT '关联答卷（答卷质检）',
  workorder_id       BIGINT UNSIGNED DEFAULT NULL COMMENT '关联工单（工单质检）',
  qc_type            VARCHAR(16)  NOT NULL DEFAULT 'RECORDING' COMMENT 'RECORDING录音/SHEET答卷/WORKORDER工单',
  qc_user_id         BIGINT UNSIGNED NOT NULL,
  score              DECIMAL(6,2) NOT NULL DEFAULT 0,
  defect_json        JSON         DEFAULT NULL COMMENT '缺陷明细',
  qualification      TINYINT      NOT NULL DEFAULT 1 COMMENT '1合格 0不合格',
  appeal_status      VARCHAR(16)  DEFAULT NULL COMMENT '申诉状态 NONE/APPEALING/RESOLVED',
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_qcr_task (task_id),
  KEY idx_qcr_agent_call (call_id)
) ENGINE=InnoDB COMMENT='质检结果';

-- ============================================================================
-- 09 工单域（对应菜单：M13 工单管理；NK3C 呼入业务）
-- ============================================================================

CREATE TABLE wfm_template (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  name               VARCHAR(128) NOT NULL COMMENT '工作流模板名（如 投诉处理流）',
  version            INT          NOT NULL DEFAULT 1,
  status             TINYINT      NOT NULL DEFAULT 1,
  PRIMARY KEY (id)
) ENGINE=InnoDB COMMENT='工单工作流模板';

CREATE TABLE wfm_node (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  template_id        BIGINT UNSIGNED NOT NULL,
  node_code          VARCHAR(32)  NOT NULL COMMENT '节点编码（ACCEPT受理/HANDLE处理/VERIFY回访/CLOSE归档）',
  node_name          VARCHAR(64)  NOT NULL,
  node_type          VARCHAR(16)  NOT NULL DEFAULT 'TASK' COMMENT 'TASK任务/APPROVAL审批/AUTO回访自动生成',
  assign_rule        VARCHAR(32)  DEFAULT NULL COMMENT '分派规则（ROUND_ROBIN/BY_GROUP/MANUAL）',
  sla_hours          INT          DEFAULT NULL COMMENT '时限（超时升级）',
  next_json          JSON         DEFAULT NULL COMMENT '流转条件与下一节点',
  sort_no            INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_wfnode (template_id)
) ENGINE=InnoDB COMMENT='工作流节点';

CREATE TABLE wfm_field_def (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  template_id        BIGINT UNSIGNED NOT NULL,
  field_code         VARCHAR(64)  NOT NULL,
  field_name         VARCHAR(64)  NOT NULL,
  field_type         VARCHAR(16)  NOT NULL DEFAULT 'TEXT' COMMENT 'TEXT/NUMBER/DATE/DICT/ATTACH',
  required           TINYINT      NOT NULL DEFAULT 0,
  layout_json        JSON         DEFAULT NULL COMMENT '字段布局（位置/宽度）',
  sort_no            INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_wfld (template_id)
) ENGINE=InnoDB COMMENT='工单自定义字段定义';

CREATE TABLE wfm_workorder (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '工单ID',
  domain_id          BIGINT UNSIGNED NOT NULL,
  wo_no              VARCHAR(32)  NOT NULL COMMENT '工单号（唯一）',
  template_id        BIGINT UNSIGNED NOT NULL,
  project_id         BIGINT UNSIGNED DEFAULT NULL COMMENT '呼入项目（可关联）',
  call_id            BIGINT UNSIGNED DEFAULT NULL COMMENT '来源话务',
  source_channel     VARCHAR(16)  NOT NULL DEFAULT 'PHONE' COMMENT 'PHONE/WECHAT/WEIBO/SMS/EMAIL/WEBCHAT',
  cust_phone         VARCHAR(32)  DEFAULT NULL,
  cust_name          VARCHAR(64)  DEFAULT NULL,
  title              VARCHAR(255) NOT NULL,
  content            TEXT         DEFAULT NULL,
  priority           VARCHAR(8)   NOT NULL DEFAULT 'MID' COMMENT 'HIGH/MID/LOW',
  current_node       VARCHAR(32)  DEFAULT NULL COMMENT '当前节点编码',
  assignee_id        BIGINT UNSIGNED DEFAULT NULL COMMENT '当前处理人',
  status             VARCHAR(16)  NOT NULL DEFAULT 'OPEN' COMMENT 'OPEN/PROCESSING/WAITING回访/CLOSED/CANCELED',
  sla_due            DATETIME(3)  DEFAULT NULL COMMENT '超时点',
  created_by         BIGINT UNSIGNED NOT NULL,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  closed_at          DATETIME(3)  DEFAULT NULL,
  updated_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_wo_no (wo_no),
  KEY idx_wo_status (domain_id, status),
  KEY idx_wo_phone (cust_phone)
) ENGINE=InnoDB COMMENT='工单';

CREATE TABLE wfm_field_value (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  workorder_id       BIGINT UNSIGNED NOT NULL,
  field_id           BIGINT UNSIGNED NOT NULL,
  value_text         TEXT         DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_wfv (workorder_id, field_id)
) ENGINE=InnoDB COMMENT='工单字段值';

CREATE TABLE wfm_action_log (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  workorder_id       BIGINT UNSIGNED NOT NULL,
  node_code          VARCHAR(32)  DEFAULT NULL,
  action             VARCHAR(32)  NOT NULL COMMENT 'CREATE/TRANSFER/COMMENT/URGE/CLOSE/REOPEN',
  operator_id        BIGINT UNSIGNED NOT NULL,
  remark             VARCHAR(500) DEFAULT NULL,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_woa (workorder_id, created_at)
) ENGINE=InnoDB COMMENT='工单操作流水';

-- ============================================================================
-- 10 IVR 域（对应菜单：M14 IVR 管理；任意级数/回合跳转 + 语音留言）
-- ============================================================================

CREATE TABLE ivr_flow (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  name               VARCHAR(128) NOT NULL COMMENT '流程名（如 办证结果查询流）',
  inbound_no         VARCHAR(32)  DEFAULT NULL COMMENT '绑定的呼入号码',
  version            INT          NOT NULL DEFAULT 1,
  status             VARCHAR(16)  NOT NULL DEFAULT 'DRAFT' COMMENT 'DRAFT/PUBLISHED/OFFLINE',
  PRIMARY KEY (id)
) ENGINE=InnoDB COMMENT='IVR 流程';

CREATE TABLE ivr_node (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  flow_id            BIGINT UNSIGNED NOT NULL,
  node_code          VARCHAR(32)  NOT NULL COMMENT '节点编码',
  node_type          VARCHAR(16)  NOT NULL COMMENT 'PLAY播报/MENU按键菜单/QUERY外部查询接口/RECORD留言/TRANSFER转人工/COND条件/TIMECHECK时段',
  node_name          VARCHAR(64)  NOT NULL,
  param_json         JSON         DEFAULT NULL COMMENT '参数（播报音频/按键位表/查询接口/转接技能组）',
  next_json          JSON         DEFAULT NULL COMMENT '分支路由（按键1->节点X ...）',
  sort_no            INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_ivrn (flow_id)
) ENGINE=InnoDB COMMENT='IVR 节点';

CREATE TABLE ivr_audio (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  name               VARCHAR(128) NOT NULL,
  file_path          VARCHAR(500) NOT NULL,
  tts_text           TEXT         DEFAULT NULL COMMENT 'TTS 合成文本',
  duration_sec       INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
) ENGINE=InnoDB COMMENT='IVR 语音文件';

CREATE TABLE ivr_message (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  flow_id            BIGINT UNSIGNED NOT NULL,
  caller_no          VARCHAR(32)  NOT NULL COMMENT '留言来电号码',
  audio_path         VARCHAR(500) NOT NULL,
  duration_sec       INT          NOT NULL DEFAULT 0,
  status             VARCHAR(16)  NOT NULL DEFAULT 'NEW' COMMENT 'NEW/LISTENED/TO_WORKORDER',
  workorder_id       BIGINT UNSIGNED DEFAULT NULL COMMENT '转工单',
  received_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_ivrm (flow_id, received_at)
) ENGINE=InnoDB COMMENT='IVR 留言';

-- ============================================================================
-- 11 导出域（对应菜单：M11 数据导出）Excel / SPSS / Quantum / Txt
-- ============================================================================

CREATE TABLE exp_task (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL,
  project_id         BIGINT UNSIGNED DEFAULT NULL,
  export_type        VARCHAR(16)  NOT NULL COMMENT 'DATA答卷数据/RECORDING录音/ACCOUNT账号/QUOTA配额条件',
  file_format        VARCHAR(16)  NOT NULL COMMENT 'XLSX/SPSS/QUANTUM/TXT/ZIP',
  filter_json        JSON         DEFAULT NULL COMMENT '范围条件（结果码/时间段/坐席）',
  include_dict       TINYINT      NOT NULL DEFAULT 1 COMMENT '是否携带变量字典（SPSS 元数据）',
  status             VARCHAR(16)  NOT NULL DEFAULT 'PENDING' COMMENT 'PENDING/RUNNING/DONE/FAILED',
  file_path          VARCHAR(500) DEFAULT NULL,
  approved_by        BIGINT UNSIGNED DEFAULT NULL COMMENT '导出审批人（个保合规）',
  created_by         BIGINT UNSIGNED NOT NULL,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  finished_at        DATETIME(3)  DEFAULT NULL,
  PRIMARY KEY (id),
  KEY idx_exp (domain_id, status)
) ENGINE=InnoDB COMMENT='导出任务';

-- ============================================================================
-- 12 统计域（对应菜单：M10 统计报表；实时部分用 redis/内存，落地部分如下）
-- ============================================================================

CREATE TABLE rpt_day_project (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  stat_date          DATE         NOT NULL,
  project_id         BIGINT UNSIGNED NOT NULL,
  dial_count         INT          NOT NULL DEFAULT 0 COMMENT '呼出总数',
  connect_count      INT          NOT NULL DEFAULT 0 COMMENT '接通数',
  success_count      INT          NOT NULL DEFAULT 0 COMMENT '成功答卷数',
  fail_count         INT          NOT NULL DEFAULT 0,
  appoint_count      INT          NOT NULL DEFAULT 0,
  total_talk_sec     BIGINT       NOT NULL DEFAULT 0,
  abandon_count      INT          NOT NULL DEFAULT 0 COMMENT '呼损（接通无坐席放弃）',
  PRIMARY KEY (id),
  UNIQUE KEY uk_rdp (stat_date, project_id)
) ENGINE=InnoDB COMMENT='项目日统计（预聚合）';

CREATE TABLE rpt_day_agent (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  stat_date          DATE         NOT NULL,
  agent_id           BIGINT UNSIGNED NOT NULL,
  project_id         BIGINT UNSIGNED NOT NULL,
  dial_count         INT          NOT NULL DEFAULT 0,
  connect_count      INT          NOT NULL DEFAULT 0,
  success_count      INT          NOT NULL DEFAULT 0,
  talk_sec           BIGINT       NOT NULL DEFAULT 0,
  online_sec         BIGINT       NOT NULL DEFAULT 0 COMMENT '签入时长',
  busy_sec           BIGINT       NOT NULL DEFAULT 0,
  qc_score           DECIMAL(6,2) DEFAULT NULL COMMENT '当日质检均分',
  PRIMARY KEY (id),
  UNIQUE KEY uk_rda (stat_date, agent_id, project_id)
) ENGINE=InnoDB COMMENT='坐席日统计（绩效考核）';

-- 单题/交叉统计从 ans_answer 实时聚合；大表用如下汇总表加速
CREATE TABLE rpt_question_stat (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  project_id         BIGINT UNSIGNED NOT NULL,
  question_id        BIGINT UNSIGNED NOT NULL,
  option_value       VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '选项编码（开放题为码值）',
  hit_count          INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_rqs (project_id, question_id, option_value)
) ENGINE=InnoDB COMMENT='单题频数汇总';

-- ============================================================================
-- 13 系统参数（半年原则天数、清理策略、许可证等）
-- ============================================================================

CREATE TABLE sys_param (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id          BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0=全局',
  param_code         VARCHAR(64)  NOT NULL,
  param_value        VARCHAR(500) NOT NULL,
  remark             VARCHAR(255) DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_param (domain_id, param_code)
) ENGINE=InnoDB COMMENT='系统参数';

-- ============================================================================
-- 14 常用视图
-- ============================================================================

-- 项目执行进度（项目监控菜单实时首页数据源）
CREATE OR REPLACE VIEW v_project_progress AS
SELECT p.id AS project_id, p.project_code, p.project_name, p.status,
       COUNT(s.id) AS sample_total,
       SUM(s.status = 'SUCCESS') AS sample_success,
       SUM(s.status = 'FAIL')    AS sample_fail,
       SUM(s.status IN ('IDLE','RECYCLED')) AS sample_left,
       SUM(s.status = 'APPOINT') AS sample_appoint,
       ROUND(IFNULL(SUM(s.status = 'SUCCESS') / NULLIF(COUNT(s.id),0) * 100, 0), 2) AS success_rate_pct
FROM prj_project p
LEFT JOIN smp_sample s ON s.project_id = p.id
GROUP BY p.id, p.project_code, p.project_name, p.status;

-- 配额完成进度
CREATE OR REPLACE VIEW v_quota_progress AS
SELECT q.id AS quota_id, q.qnr_id, q.quota_name, q.total_target,
       SUM(c.target_count) AS cell_target_sum,
       SUM(c.done_count)   AS cell_done_sum,
       ROUND(IFNULL(SUM(c.done_count) / NULLIF(SUM(c.target_count),0) * 100, 0), 2) AS done_pct
FROM qnr_quota q
LEFT JOIN qnr_quota_cell c ON c.quota_id = q.id
GROUP BY q.id, q.qnr_id, q.quota_name, q.total_target;

-- 坐席当日实时效率（执行总览）
CREATE OR REPLACE VIEW v_agent_today AS
SELECT a.id AS agent_id, a.user_no, a.user_name, g.group_name,
       IFNULL(SUM(r.dial_count), 0)    AS dial_count,
       IFNULL(SUM(r.connect_count), 0) AS connect_count,
       IFNULL(SUM(r.success_count), 0) AS success_count,
       IFNULL(SUM(r.talk_sec), 0)      AS talk_sec,
       IFNULL(SUM(r.online_sec), 0)    AS online_sec
FROM sys_user a
LEFT JOIN org_group g ON g.id = a.group_id
LEFT JOIN rpt_day_agent r ON r.agent_id = a.id AND r.stat_date = CURDATE()
WHERE a.status = 1
GROUP BY a.id, a.user_no, a.user_name, g.group_name;

-- ============================================================================
-- 15 初始化数据：菜单资源树 / 角色 / 权限 / 状态码 / 参数 / 演示组织
--    （与《开发文档》第 6.16 节菜单树一一对应）
-- ============================================================================

-- 一级菜单（M01~M15）
INSERT INTO sys_resource (id, parent_id, resource_type, resource_code, resource_name, url, sort_no) VALUES
(101, 0, 'MENU', 'm01.workspace',   '工作台',    '/workspace',        1),
(102, 0, 'MENU', 'm02.org',         '组织权限',  '/org',              2),
(103, 0, 'MENU', 'm03.project',     '项目管理',  '/project',          3),
(104, 0, 'MENU', 'm04.questionnaire','问卷管理', '/questionnaire',    4),
(105, 0, 'MENU', 'm05.sample',      '样本管理',  '/sample',           5),
(106, 0, 'MENU', 'm06.strategy',    '呼叫策略',  '/strategy',         6),
(107, 0, 'MENU', 'm07.execution',   '执行管理',  '/execution',        7),
(108, 0, 'MENU', 'm08.monitor',     '实时监控',  '/monitor',          8),
(109, 0, 'MENU', 'm09.answer',      '答卷管理',  '/answer',           9),
(110, 0, 'MENU', 'm10.report',      '统计报表',  '/report',           10),
(111, 0, 'MENU', 'm11.export',      '数据导出',  '/export',           11),
(112, 0, 'MENU', 'm12.qc',          '质检管理',  '/qc',               12),
(113, 0, 'MENU', 'm13.workorder',   '工单管理',  '/workorder',        13),
(114, 0, 'MENU', 'm14.ivr',         'IVR管理',   '/ivr',              14),
(115, 0, 'MENU', 'm15.system',      '系统维护',  '/system',           15);

-- 二级菜单（核心模块全量，其余模块按同一模式扩展）
INSERT INTO sys_resource (id, parent_id, resource_type, resource_code, resource_name, url, sort_no) VALUES
-- 102 组织权限
(1021,102,'MENU','m02.domain','域管理','/org/domain',1),
(1022,102,'MENU','m02.orgmgmt','组织管理','/org/org',2),
(1023,102,'MENU','m02.group','组管理','/org/group',3),
(1024,102,'MENU','m02.user','员工管理','/org/user',4),
(1025,102,'MENU','m02.role','角色与资源授权','/org/role',5),
(1026,102,'MENU','m02.oplog','操作日志','/org/oplog',6),
-- 103 项目管理
(1031,103,'MENU','m03.list','项目列表','/project/list',1),
(1032,103,'MENU','m03.wizard','新建项目','/project/wizard',2),
(1033,103,'MENU','m03.dist','分发管理','/project/distribution',3),
(1034,103,'MENU','m03.schedule','项目排班','/project/schedule',4),
(1035,103,'MENU','m03.archive','项目归档','/project/archive',5),
-- 104 问卷管理
(1041,104,'MENU','m04.list','问卷列表','/questionnaire/list',1),
(1042,104,'MENU','m04.designer','问卷设计器','/questionnaire/designer',2),
(1043,104,'MENU','m04.logic','逻辑与配额','/questionnaire/logic',3),
(1044,104,'MENU','m04.validate','校验调试','/questionnaire/validate',4),
(1045,104,'MENU','m04.bank','题库管理','/questionnaire/bank',5),
(1046,104,'MENU','m04.template','模板管理','/questionnaire/template',6),
-- 105 样本管理
(1051,105,'MENU','m05.pool','样本池','/sample/pool',1),
(1052,105,'MENU','m05.import','样本导入','/sample/import',2),
(1053,105,'MENU','m05.random','随机抽样','/sample/random-gen',3),
(1054,105,'MENU','m05.assign','样本分配','/sample/assign',4),
(1055,105,'MENU','m05.field','字段与字典','/sample/field',5),
(1056,105,'MENU','m05.black','黑名单/半年原则','/sample/blacklist',6),
(1057,105,'MENU','m05.statuscode','样本状态码','/sample/status-code',7),
-- 106 呼叫策略
(1061,106,'MENU','m06.list','外呼策略列表','/strategy/list',1),
(1062,106,'MENU','m06.predict','预测拨号参数','/strategy/predictive',2),
(1063,106,'MENU','m06.redial','重拨规则','/strategy/redial',3),
(1064,106,'MENU','m06.window','时段与排班','/strategy/window',4),
(1065,106,'MENU','m06.caller','主叫号码管理','/strategy/caller',5),
(1066,106,'MENU','m06.inbound','呼入路由','/strategy/inbound',6),
-- 107 执行管理
(1071,107,'MENU','m07.board','执行总览','/execution/board',1),
(1072,107,'MENU','m07.agent','坐席状态','/execution/agent-state',2),
(1073,107,'MENU','m07.dispatch','派样监控','/execution/dispatch',3),
(1074,107,'MENU','m07.appoint','预约与回拨','/execution/appointment',4),
-- 108 实时监控
(1081,108,'MENU','m08.project','项目监控','/monitor/project',1),
(1082,108,'MENU','m08.wall','坐席监控墙','/monitor/wall',2),
(1083,108,'MENU','m08.alarm','报警提醒','/monitor/alarm',3),
-- 109 答卷管理
(1091,109,'MENU','m09.list','答卷列表','/answer/list',1),
(1092,109,'MENU','m09.audit','答卷审核','/answer/audit',2),
(1093,109,'MENU','m09.code','答卷编码','/answer/coding',3),
(1094,109,'MENU','m09.recording','录音管理','/answer/recording',4),
-- 110 统计报表
(1101,110,'MENU','m10.traffic','话务统计','/report/traffic',1),
(1102,110,'MENU','m10.sample','样本统计','/report/sample',2),
(1103,110,'MENU','m10.single','单题统计','/report/single',3),
(1104,110,'MENU','m10.cross','交叉统计','/report/cross',4),
(1105,110,'MENU','m10.quota','配额统计','/report/quota',5),
(1106,110,'MENU','m10.fee','话费统计','/report/fee',6),
(1107,110,'MENU','m10.perf','坐席绩效','/report/performance',7),
-- 111 数据导出
(1111,111,'MENU','m11.data','答卷数据导出','/export/data',1),
(1112,111,'MENU','m11.rec','录音批量导出','/export/recording',2),
(1113,111,'MENU','m11.task','导出任务','/export/task',3),
-- 112 质检管理
(1121,112,'MENU','m12.task','质检任务','/qc/task',1),
(1122,112,'MENU','m12.rec','录音质检','/qc/recording',2),
(1123,112,'MENU','m12.sheet','答卷质检','/qc/sheet',3),
(1124,112,'MENU','m12.tpl','评分表管理','/qc/template',4),
(1125,112,'MENU','m12.report','质检报告','/qc/report',5),
-- 113 工单管理
(1131,113,'MENU','m13.list','工单列表','/workorder/list',1),
(1132,113,'MENU','m13.field','字段与布局','/workorder/field',2),
(1133,113,'MENU','m13.flow','工作流模板','/workorder/flow',3),
(1134,113,'MENU','m13.assign','工单分派','/workorder/assign',4),
(1135,113,'MENU','m13.kb','知识库','/workorder/knowledge',5),
-- 114 IVR
(1141,114,'MENU','m14.flow','流程列表','/ivr/flow',1),
(1142,114,'MENU','m14.designer','流程设计器','/ivr/designer',2),
(1143,114,'MENU','m14.audio','语音文件','/ivr/audio',3),
(1144,114,'MENU','m14.msg','留言管理','/ivr/message',4),
-- 115 系统维护
(1151,115,'MENU','m15.param','参数配置','/system/param',1),
(1152,115,'MENU','m15.dict','数据字典','/system/dict',2),
(1153,115,'MENU','m15.backup','备份恢复','/system/backup',3),
(1154,115,'MENU','m15.license','许可证管理','/system/license',4),
(1155,115,'MENU','m15.api','接口管理','/system/api',5);

-- 权限型资源（PERM，按 Code 判断——对应 NK3C 公开的 4 类权限）
INSERT INTO sys_resource (id, parent_id, resource_type, resource_code, resource_name, sort_no) VALUES
(901, 0, 'PERM', 'domainAdmin', '域管理员权限', 1),
(902, 0, 'PERM', 'orgAdmin',    '组织管理员权限', 2),
(903, 0, 'PERM', 'groupAdmin',  '组管理员（督导）权限', 3),
(904, 0, 'PERM', 'phoneAdmin',  '一线员工（坐席）权限', 4);

-- 角色
INSERT INTO sys_role (id, domain_id, role_code, role_name) VALUES
(1, 0, 'domainAdmin', '域管理员'),
(2, 0, 'orgAdmin',    '组织管理员（项目经理）'),
(3, 0, 'groupAdmin',  '组管理员（督导/质检）'),
(4, 0, 'phoneAdmin',  '一线员工（坐席）');

-- 角色授权示例：域管理员=全部菜单；坐席=仅工作台/执行
INSERT INTO sys_role_resource (role_id, resource_id)
SELECT 1, id FROM sys_resource WHERE resource_type = 'MENU';
INSERT INTO sys_role_resource (role_id, resource_id) VALUES (4, 101), (4, 1071), (4, 1074);
INSERT INTO sys_role_resource (role_id, resource_id) VALUES (3, 101), (3, 108), (3, 109), (3, 112);

-- 演示域/组织/组/用户
INSERT INTO org_domain (id, domain_code, domain_name, db_tag) VALUES (1, 'D001', '演示域（总部）', 'nk3c_main');
INSERT INTO org_org    (id, domain_id, parent_id, org_code, org_name) VALUES (1, 1, 0, 'ORG01', '调查业务部');
INSERT INTO org_group  (id, domain_id, org_id, group_code, group_name, group_type, skill_tags) VALUES
(1, 1, 1, 'G-AG01', '呼出一组', 'AGENT', 'mandarin'),
(2, 1, 1, 'G-SK-CT', '粤语技能组', 'SKILL', 'cantonese');
INSERT INTO sys_user (id, domain_id, org_id, group_id, user_no, user_name, login_name, password_hash, agent_no) VALUES
(1, 1, 1, NULL, 'A0001', '系统管理员', 'admin', '$2a$10$CHANGE_ME_IN_PRODUCTION', NULL),
(2, 1, 1, 1,    '1020', '坐席演示',  'agent01', '$2a$10$CHANGE_ME_IN_PRODUCTION', '1020');
INSERT INTO sys_user_role (user_id, role_id) VALUES (1, 1), (2, 4);

-- 电话结束状态码（行业通用码表，可直接扩充）
INSERT INTO smp_status_code (domain_id, code, name, category, closed_flag, allow_redial, hit_black_flag, sort_no) VALUES
(1, 'SUCCESS',  '访问成功',     'SUCCESS',  1, 0, 0, 1),
(1, 'PARTIAL',  '部分完成',     'NEUTRAL',  1, 1, 0, 2),
(1, 'QUFAIL',   '甄别不合格',   'FAIL',     1, 0, 0, 3),
(1, 'REFUSE',   '拒访',         'FAIL',     1, 0, 1, 4),
(1, 'BREAKOFF', '中途挂断',     'FAIL',     1, 1, 0, 5),
(1, 'APPOINT',  '预约回拨',     'APPOINT',  0, 0, 0, 6),
(1, 'NA',       '无人接听',     'FAIL',     0, 1, 0, 7),
(1, 'BUSY',     '占线',         'FAIL',     0, 1, 0, 8),
(1, 'INVALID',  '空号',         'FAIL',     1, 0, 1, 9),
(1, 'FAX',      '传真/数据线',  'FAIL',     1, 0, 0, 10),
(1, 'WRONGNO',  '号码错误',     'FAIL',     1, 0, 0, 11),
(1, 'NOTIN',    '不符合条件',   'FAIL',     1, 0, 0, 12),
(1, 'LANGUAGE', '语言不通',     'FAIL',     1, 0, 0, 13);

-- 系统参数（半年原则 = 180 天）
INSERT INTO sys_param (domain_id, param_code, param_value, remark) VALUES
(1, 'halfyear.days',        '180',    '半年原则：N 天内已访样本禁呼'),
(1, 'redial.max',           '3',      '默认重拨上限'),
(1, 'redial.interval.min',  '60',     '默认重拨间隔（分钟）'),
(1, 'predict.abandon.max',  '3.0',    '预测外呼呼损率上限（%）'),
(1, 'recording.retention.days', '365','录音留存天数'),
(1, 'sample.lock.seconds',  '120',    '派样锁超时秒数（坐席断线回收）');

SET FOREIGN_KEY_CHECKS = 1;

-- ============================================================================
-- 附：核心查询样例
-- ----------------------------------------------------------------------------
-- 1) 半年原则过滤 + 黑名单排除的取号（派样服务核心 SQL）：
-- SELECT s.id
-- FROM smp_sample s
-- JOIN smp_phone p ON p.sample_id = s.id AND p.valid_flag = 1
-- WHERE s.project_id = @pid AND s.status = 'IDLE'
--   AND (s.last_called_at IS NULL
--        OR s.last_called_at < NOW() - INTERVAL (SELECT CAST(param_value AS UNSIGNED)
--            FROM sys_param WHERE param_code='halfyear.days' AND domain_id=s.domain_id) DAY)
--   AND NOT EXISTS (SELECT 1 FROM smp_blacklist b
--                   WHERE b.domain_id = s.domain_id AND b.phone_no = p.phone_no
--                     AND (b.scope='GLOBAL' OR (b.scope='PROJECT' AND b.project_id=s.project_id)))
--   AND s.call_attempts < (SELECT CAST(param_value AS UNSIGNED)
--            FROM sys_param WHERE param_code='redial.max' AND domain_id=s.domain_id)
-- ORDER BY s.shuffle_key
-- LIMIT 1 FOR UPDATE SKIP LOCKED;   -- 8.0 跳过锁：多派样线程不互相阻塞
--
-- 2) 配额原子扣减（命中判定通过后）：
-- UPDATE qnr_quota_cell SET done_count = done_count + 1
-- WHERE id = @cell AND done_count < target_count;   -- 影响行数=0 即超配额，回滚放行逻辑
--
-- 3) 单题频数（实时统计）：
-- SELECT o.option_values, COUNT(*) FROM ans_answer
-- WHERE question_id = @qid AND EXISTS (SELECT 1 FROM ans_sheet
--   WHERE ans_sheet.id = ans_answer.sheet_id AND status IN ('SUBMITTED','AUDITED'))
-- GROUP BY o.option_values;
-- ============================================================================
