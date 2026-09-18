-- ============================================================================
--  南康 ITACATI / NK3C 类 CATI 系统 · SQL Server 2019+ 版建表脚本（自动迁移稿）
--  由 MySQL 8.0 版经 sqlglot 转换 + 规则后处理生成（2026-09-17）。
--  执行前核对清单：
--  1) JSON 列：NVARCHAR(MAX)（可加 ISJSON() CHECK 约束）
--  2) 自增：IDENTITY(1,1)（已转换，请抽查）
--  3) 时间：DATETIME2(3) + DEFAULT SYSDATETIME()（已转换）；updated_at 需触发器/应用层维护
--  4) 注释：内联 COMMENT 已转为 /* */；如需扩展属性可再批量生成 MS_Description
--  5) 内联 INDEX：已抽取为文件尾部独立 CREATE INDEX
--  6) 派样 SKIP LOCKED：改写为 WITH (UPDLOCK, ROWLOCK, READPAST)
--  7) 批处理：CREATE DATABASE / USE 后已加 GO
--  8) 视图函数（IFNULL/CURDATE 等）已由转译器按方言替换，请抽查
--  9) 本稿为迁移草稿：正式执行前请在目标库空实例试跑并逐步修正
--  10) MySQL 原版（含完整中文注释与初始化数据）为基准版本
-- ============================================================================
IF DB_ID('nk3c') IS NULL CREATE DATABASE nk3c;
GO

GO
USE nk3c;
GO

/* ============================================================================ */
/* 01 组织权限域（分权分域：域 domain → 组织 org → 组 group → 员工 user） */
/*    对应菜单：M02 组织权限；权限模型：domainAdmin/orgAdmin/groupAdmin/phoneAdmin */
/* ============================================================================ */
/* 域（最高隔离单元：分公司/子公司独立部署、数据逻辑分开） */
CREATE TABLE org_domain (
  id BIGINT NOT NULL IDENTITY(1,1) /* 主键 */,
  domain_code VARCHAR(32) NOT NULL /* 域编码（唯一） */,
  domain_name VARCHAR(128) NOT NULL /* 域名称 */,
  db_tag VARCHAR(64) DEFAULT NULL /* 对应物理库标识（多层数据存储/分发项目用） */,
  status TINYINT NOT NULL DEFAULT 1 /* 1启用 0停用 */,
  remark VARCHAR(255) DEFAULT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  updated_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME() /* updated_at: 需触发器/应用层维护 */,
  PRIMARY KEY (id),
  CONSTRAINT uk_domain_code UNIQUE (
    domain_code
  )
) /* 域（分权分域顶层） */;

/* 组织（域内部门层级） */
CREATE TABLE org_org (
  id BIGINT NOT NULL IDENTITY(1,1) /* 主键 */,
  domain_id BIGINT NOT NULL /* 所属域 */,
  parent_id BIGINT NOT NULL DEFAULT 0 /* 上级组织（0=根） */,
  org_code VARCHAR(32) NOT NULL /* 组织编码 */,
  org_name VARCHAR(128) NOT NULL /* 组织名称 */,
  sort_no INTEGER NOT NULL DEFAULT 0,
  status TINYINT NOT NULL DEFAULT 1,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  updated_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME() /* updated_at: 需触发器/应用层维护 */,
  PRIMARY KEY (id)
) /* 组织 */;

/* 组（坐席组/访问员组/技能组；样本可按组分配） */
CREATE TABLE org_group (
  id BIGINT NOT NULL IDENTITY(1,1) /* 主键 */,
  domain_id BIGINT NOT NULL /* 所属域 */,
  org_id BIGINT NOT NULL /* 所属组织 */,
  group_code VARCHAR(32) NOT NULL /* 组编码 */,
  group_name VARCHAR(128) NOT NULL /* 组名称 */,
  group_type VARCHAR(16) NOT NULL DEFAULT 'AGENT' /* AGENT坐席组/INTERVIEWER访问员组/SKILL技能组/QC质检组 */,
  skill_tags VARCHAR(255) DEFAULT NULL /* 技能标签（方言等，逗号分隔，影响派样） */,
  status TINYINT NOT NULL DEFAULT 1,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  updated_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME() /* updated_at: 需触发器/应用层维护 */,
  PRIMARY KEY (id)
) /* 组（坐席/访问员/技能组） */;

/* 用户（一线坐席 phoneAdmin / 组管理员 / 组织管理员 / 域管理员） */
CREATE TABLE sys_user (
  id BIGINT NOT NULL IDENTITY(1,1) /* 主键 */,
  domain_id BIGINT NOT NULL /* 所属域 */,
  org_id BIGINT NOT NULL DEFAULT 0 /* 所属组织 */,
  group_id BIGINT DEFAULT NULL /* 所属组 */,
  user_no VARCHAR(32) NOT NULL /* 员工编号/坐席工号（工号段内取号，唯一） */,
  user_name VARCHAR(64) NOT NULL /* 姓名 */,
  login_name VARCHAR(64) NOT NULL /* 登录名（唯一） */,
  password_hash VARCHAR(128) NOT NULL /* 密码散列（Shiro 加盐） */,
  agent_no VARCHAR(16) DEFAULT NULL /* CTI 软电话签入工号（NKZXAgent 使用） */,
  ext_skills VARCHAR(255) DEFAULT NULL /* 个人技能（继承组技能之外） */,
  status TINYINT NOT NULL DEFAULT 1 /* 1启用 0停用 2锁定 */,
  last_login_at DATETIME2(3) DEFAULT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  updated_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME() /* updated_at: 需触发器/应用层维护 */,
  PRIMARY KEY (id),
  CONSTRAINT uk_user_login UNIQUE (
    login_name
  ),
  CONSTRAINT uk_user_no UNIQUE (
    domain_id,
    user_no
  )
) /* 用户 */;

/* 角色 / 授权（资源=菜单+按钮+权限，权限按 Code 判断） */
CREATE TABLE sys_role (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL /* 所属域（0=全局角色） */,
  role_code VARCHAR(64) NOT NULL /* 角色码：domainAdmin/orgAdmin/groupAdmin/phoneAdmin/... */,
  role_name VARCHAR(64) NOT NULL,
  remark VARCHAR(255) DEFAULT NULL,
  PRIMARY KEY (id),
  CONSTRAINT uk_role UNIQUE (
    domain_id,
    role_code
  )
) /* 角色 */;

CREATE TABLE sys_user_role (
  id BIGINT NOT NULL IDENTITY,
  user_id BIGINT NOT NULL,
  role_id BIGINT NOT NULL,
  PRIMARY KEY (id),
  CONSTRAINT uk_ur UNIQUE (
    user_id,
    role_id
  )
) /* 用户-角色 */;

/* 资源树：类型 MENU=菜单 BUTTON=按钮/操作 API=接口 PERM=权限（橙色，按 Code 判断） */
CREATE TABLE sys_resource (
  id BIGINT NOT NULL IDENTITY,
  parent_id BIGINT NOT NULL DEFAULT 0 /* 上级资源（0=根） */,
  resource_type VARCHAR(16) NOT NULL /* MENU/BUTTON/API/PERM */,
  resource_code VARCHAR(64) NOT NULL /* 资源码（前端按 Code 控权，如 project:create） */,
  resource_name VARCHAR(64) NOT NULL /* 名称（菜单显示名） */,
  url VARCHAR(255) DEFAULT NULL /* 前端路由/页面路径（小写） */,
  icon VARCHAR(64) DEFAULT NULL,
  sort_no INTEGER NOT NULL DEFAULT 0,
  status TINYINT NOT NULL DEFAULT 1,
  PRIMARY KEY (id),
  CONSTRAINT uk_res_code UNIQUE (
    resource_code
  )
) /* 资源树（菜单/按钮/权限） */;

CREATE TABLE sys_role_resource (
  id BIGINT NOT NULL IDENTITY,
  role_id BIGINT NOT NULL,
  resource_id BIGINT NOT NULL,
  PRIMARY KEY (id),
  CONSTRAINT uk_rr UNIQUE (
    role_id,
    resource_id
  )
) /* 角色-资源授权 */;

CREATE TABLE sys_login_log (
  id BIGINT NOT NULL IDENTITY,
  user_id BIGINT NOT NULL,
  login_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  ip VARCHAR(64) DEFAULT NULL,
  result TINYINT NOT NULL DEFAULT 1 /* 1成功 0失败 */,
  detail VARCHAR(255) DEFAULT NULL,
  PRIMARY KEY (id)
) /* 登录日志 */;

CREATE TABLE sys_op_log (
  id BIGINT NOT NULL IDENTITY,
  user_id BIGINT NOT NULL,
  op_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  module VARCHAR(64) NOT NULL /* 模块码（如 M03 项目管理） */,
  action VARCHAR(64) NOT NULL /* 动作码（如 project:start） */,
  biz_id VARCHAR(64) DEFAULT NULL /* 业务主键 */,
  detail VARCHAR(MAX) DEFAULT NULL /* 变更前后 NVARCHAR(MAX) */,
  ip VARCHAR(64) DEFAULT NULL,
  PRIMARY KEY (id)
) /* 操作审计日志 */;

/* ============================================================================ */
/* 02 项目域（对应菜单：M03 项目管理；支持父子项目/分发项目） */
/* ============================================================================ */
CREATE TABLE prj_project (
  id BIGINT NOT NULL IDENTITY(1,1) /* 项目ID */,
  domain_id BIGINT NOT NULL /* 所属域 */,
  project_code VARCHAR(32) NOT NULL /* 项目编号（唯一） */,
  project_name VARCHAR(128) NOT NULL /* 项目名称 */,
  project_type VARCHAR(16) NOT NULL /* SURVEY调查/MARKETING营销/CALLBACK回访/INBOUND呼入 */,
  parent_project_id BIGINT DEFAULT NULL /* 母项目ID（父子项目/分发项目） */,
  questionnaire_id BIGINT DEFAULT NULL /* 绑定问卷 */,
  strategy_id BIGINT DEFAULT NULL /* 绑定呼叫策略 */,
  start_time DATETIME2(3) DEFAULT NULL /* 计划开始 */,
  end_time DATETIME2(3) DEFAULT NULL /* 计划结束 */,
  daily_begin TIME DEFAULT NULL /* 每日可呼开始（项目时间段） */,
  daily_end TIME DEFAULT NULL /* 每日可呼结束 */,
  quota_type VARCHAR(16) DEFAULT 'NONE' /* NONE无/SINGLE单重/CROSS交叉 */,
  status VARCHAR(16) NOT NULL DEFAULT 'DRAFT' /* DRAFT草稿/TESTING试访/RUNNING执行中/PAUSED暂停/FINISHED已结束/ARCHIVED已归档 */,
  is_template TINYINT NOT NULL DEFAULT 0 /* 1=项目模板 */,
  owner_id BIGINT NOT NULL /* 项目经理 */,
  remark VARCHAR(500) DEFAULT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  updated_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME() /* updated_at: 需触发器/应用层维护 */,
  PRIMARY KEY (id),
  CONSTRAINT uk_prj_code UNIQUE (
    domain_id,
    project_code
  ),
  CONSTRAINT chk_prj_status CHECK (status IN ('DRAFT', 'TESTING', 'RUNNING', 'PAUSED', 'FINISHED', 'ARCHIVED'))
) /* 项目 */;

/* 分发项目（母项目 → 子数据库/子公司） */
CREATE TABLE prj_distribution (
  id BIGINT NOT NULL IDENTITY,
  project_id BIGINT NOT NULL /* 母项目 */,
  target_db_tag VARCHAR(64) NOT NULL /* 目标子库标识（org_domain.db_tag） */,
  sub_project_id BIGINT DEFAULT NULL /* 生成的子项目ID */,
  dist_status VARCHAR(16) NOT NULL DEFAULT 'PENDING' /* PENDING/DISTRIBUTED/FAILED */,
  dispatched_at DATETIME2(3) DEFAULT NULL,
  dispatched_by BIGINT DEFAULT NULL,
  PRIMARY KEY (id)
) /* 项目分发记录 */;

/* 项目-坐席组分配 */
CREATE TABLE prj_project_group (
  id BIGINT NOT NULL IDENTITY,
  project_id BIGINT NOT NULL,
  group_id BIGINT NOT NULL /* 承接访问的坐席/访问员组 */,
  priority INTEGER NOT NULL DEFAULT 0 /* 派样优先级 */,
  PRIMARY KEY (id),
  CONSTRAINT uk_pg UNIQUE (
    project_id,
    group_id
  )
) /* 项目-坐席组分配 */;

/* 排班（项目时间段 × 组 × 日期班次） */
CREATE TABLE prj_schedule (
  id BIGINT NOT NULL IDENTITY,
  project_id BIGINT NOT NULL,
  group_id BIGINT NOT NULL,
  shift_date DATE NOT NULL /* 班次日期 */,
  begin_time TIME NOT NULL,
  end_time TIME NOT NULL,
  plan_agents INTEGER NOT NULL DEFAULT 0 /* 计划坐席数 */,
  PRIMARY KEY (id)
) /* 项目排班表 */;

/* ============================================================================ */
/* 03 问卷域（对应菜单：M04 问卷管理；20 种题型见文档第 9 章） */
/* ============================================================================ */
CREATE TABLE qnr_questionnaire (
  id BIGINT NOT NULL IDENTITY(1,1) /* 问卷ID */,
  domain_id BIGINT NOT NULL,
  title VARCHAR(255) NOT NULL /* 问卷标题 */,
  version VARCHAR(16) NOT NULL DEFAULT 'v1.0' /* 版本号（发布一次递增） */,
  status VARCHAR(16) NOT NULL DEFAULT 'DRAFT' /* DRAFT/TESTING/PUBLISHED/OFFLINE */,
  est_minutes NUMERIC(5, 1) NOT NULL DEFAULT 0 /* 预估时长（分钟） */,
  page_style NVARCHAR(MAX) DEFAULT NULL /* 页面风格（字体/配色，支持整体与单题设置） */,
  err_msg_style NVARCHAR(MAX) DEFAULT NULL /* 自定义报错提示文案配置 */,
  created_by BIGINT NOT NULL,
  published_at DATETIME2(3) DEFAULT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  updated_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME() /* updated_at: 需触发器/应用层维护 */,
  PRIMARY KEY (id)
) /* 问卷（脚本） */;

CREATE TABLE qnr_question (
  id BIGINT NOT NULL IDENTITY(1,1) /* 题目ID */,
  qnr_id BIGINT NOT NULL /* 所属问卷 */,
  q_no INTEGER NOT NULL /* 题号（顺序号） */,
  q_type VARCHAR(16) NOT NULL /* single/multi/sort/text/number/date/scale/assign/matrix/matrix3d/order_multi/dummy/calc/info/screen/repeat/appt/endstat 等 20 种 */,
  title VARCHAR(1000) NOT NULL /* 题干 */,
  required TINYINT NOT NULL DEFAULT 1 /* 必答 */,
  random_flag TINYINT NOT NULL DEFAULT 0 /* 选项随机/轮换/倒序 */,
  classify_flag TINYINT NOT NULL DEFAULT 0 /* 选项分类展示 */,
  min_value NUMERIC(12, 4) DEFAULT NULL /* 数值题下限/分配题总和校验 */,
  max_value NUMERIC(12, 4) DEFAULT NULL,
  max_len INTEGER DEFAULT NULL /* 开放题最大字数 */,
  scale_max INTEGER DEFAULT NULL /* 刻度题最大刻度 */,
  rows_json NVARCHAR(MAX) DEFAULT NULL /* 矩阵题行定义 */,
  cols_json NVARCHAR(MAX) DEFAULT NULL /* 矩阵题列定义 */,
  formula VARCHAR(MAX) DEFAULT NULL /* 辅助计算题公式（哑题/计算题） */,
  media_json NVARCHAR(MAX) DEFAULT NULL /* 题干多媒体（图片/音频，ITACAPI 共用引擎） */,
  font_style NVARCHAR(MAX) DEFAULT NULL /* 单题字体设置 */,
  sort_no INTEGER NOT NULL DEFAULT 0,
  status TINYINT NOT NULL DEFAULT 1,
  PRIMARY KEY (id),
  CONSTRAINT uk_q UNIQUE (
    qnr_id,
    q_no
  )
) /* 题目 */;

CREATE TABLE qnr_option (
  id BIGINT NOT NULL IDENTITY(1,1) /* 选项ID */,
  question_id BIGINT NOT NULL /* 所属题目 */,
  opt_no INTEGER NOT NULL /* 选项序号 */,
  opt_text VARCHAR(500) NOT NULL /* 选项文本 */,
  opt_value VARCHAR(64) DEFAULT NULL /* 选项编码（导出变量值） */,
  jump_type VARCHAR(16) NOT NULL DEFAULT 'NEXT' /* NEXT下一题/QID跳指定题/END结束访问 */,
  jump_question_id BIGINT DEFAULT NULL /* 跳转目标题目 */,
  exclusive_flag TINYINT NOT NULL DEFAULT 0 /* 互斥（排他项） */,
  associate_expr VARCHAR(500) DEFAULT NULL /* 选项关联表达式（随前题过滤显示） */,
  media_json NVARCHAR(MAX) DEFAULT NULL /* 选项图片/多媒体 */,
  PRIMARY KEY (id)
) /* 选项 */;

/* 高级逻辑：显示/跳转/校验（多维条件约束） */
CREATE TABLE qnr_logic (
  id BIGINT NOT NULL IDENTITY,
  qnr_id BIGINT NOT NULL,
  src_question_id BIGINT NOT NULL /* 逻辑主体题目 */,
  logic_type VARCHAR(16) NOT NULL /* SHOW条件显示/JUMP条件跳转/VALIDATE校验/CALC计算 */,
  expr_text VARCHAR(MAX) NOT NULL /* 表达式（如 Q3>7 AND Q4 CONTAINS 2） */,
  target_question_id BIGINT DEFAULT NULL /* 目标题（JUMP 用） */,
  err_msg VARCHAR(255) DEFAULT NULL /* 校验失败提示（自定义报错） */,
  sort_no INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
) /* 问卷逻辑规则 */;

/* 配额（单重/交叉；执行期实时扣减） */
CREATE TABLE qnr_quota (
  id BIGINT NOT NULL IDENTITY,
  qnr_id BIGINT NOT NULL /* 配额挂接问卷 */,
  quota_name VARCHAR(128) NOT NULL,
  total_target INTEGER NOT NULL DEFAULT 0 /* 样本总量 */,
  PRIMARY KEY (id)
) /* 配额组 */;

CREATE TABLE qnr_quota_cell (
  id BIGINT NOT NULL IDENTITY,
  quota_id BIGINT NOT NULL,
  conditions_json NVARCHAR(MAX) NOT NULL /* 命中条件（如 [{qid:Q2,in:[男]},{qid:Q4,in:[APP]}]） */,
  target_count INTEGER NOT NULL DEFAULT 0 /* 该格目标量 */,
  done_count INTEGER NOT NULL DEFAULT 0 /* 已完成（原子累加） */,
  overflow_flag TINYINT NOT NULL DEFAULT 0 /* 超配额是否放行 */,
  PRIMARY KEY (id)
) /* 配额单元（交叉格） */;

CREATE TABLE qnr_template (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL /* 问卷模板名 */,
  content_json NVARCHAR(MAX) NOT NULL /* 模板结构 */,
  PRIMARY KEY (id)
) /* 问卷模板 */;

CREATE TABLE qnr_bank (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  category VARCHAR(64) DEFAULT NULL /* 题库分类 */,
  title VARCHAR(500) NOT NULL /* 题目文本 */,
  content_json NVARCHAR(MAX) NOT NULL /* 完整题目结构（拖选使用） */,
  usage_count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
) /* 常用题库 */;

/* 问卷导入导出（doc/xml/sur 等多格式） */
CREATE TABLE qnr_import_export_log (
  id BIGINT NOT NULL IDENTITY,
  qnr_id BIGINT DEFAULT NULL,
  op_type VARCHAR(16) NOT NULL /* IMPORT/EXPORT/PRINT */,
  file_format VARCHAR(16) NOT NULL /* DOC/XML/SUR/PDF */,
  file_path VARCHAR(500) DEFAULT NULL,
  operator_id BIGINT NOT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  PRIMARY KEY (id)
) /* 问卷导入导出日志 */;

/* ============================================================================ */
/* 04 样本域（对应菜单：M05 样本管理） */
/*    规则：黑名单、半年原则、样本打散、一人多号、自定义状态码、分级分配 */
/* ============================================================================ */
CREATE TABLE smp_sample (
  id BIGINT NOT NULL IDENTITY(1,1) /* 样本ID */,
  domain_id BIGINT NOT NULL,
  project_id BIGINT DEFAULT NULL /* 进入的项目（池内未分配为 NULL） */,
  import_batch_no VARCHAR(32) DEFAULT NULL /* 导入批次 */,
  cust_name VARCHAR(64) DEFAULT NULL /* 客户/被访者姓名 */,
  gender VARCHAR(8) DEFAULT NULL /* 性别 */,
  age INTEGER DEFAULT NULL,
  province VARCHAR(32) DEFAULT NULL,
  city VARCHAR(32) DEFAULT NULL,
  region_tag VARCHAR(64) DEFAULT NULL /* 区域/分层标签（抽样层） */,
  level VARCHAR(16) DEFAULT 'NORMAL' /* 样本分级（VIP/NORMAL/...） */,
  status VARCHAR(16) NOT NULL DEFAULT 'IDLE' /* IDLE待呼/ASSIGNED已派/INCALL访问中/SUCCESS成功/FAIL失败/APPOINT预约/BANNED禁呼/RECYCLED回收 */,
  owner_group_id BIGINT DEFAULT NULL /* 当前分配访问员组 */,
  owner_agent_id BIGINT DEFAULT NULL /* 当前占用坐席（派样锁） */,
  assigned_at DATETIME2(3) DEFAULT NULL,
  last_called_at DATETIME2(3) DEFAULT NULL /* 最后呼叫时间（半年原则判断基准） */,
  call_attempts INTEGER NOT NULL DEFAULT 0 /* 已呼叫次数（重拨上限判断） */,
  ext_json NVARCHAR(MAX) DEFAULT NULL /* 自定义字段值（对应 smp_field_def） */,
  shuffle_key INTEGER NOT NULL DEFAULT 0 /* 打散排序键（样本打散） */,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  updated_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME() /* updated_at: 需触发器/应用层维护 */,
  PRIMARY KEY (id)
) /* 样本 */;

/* 一人多号（同一样本多号码，按 sort_no 顺序呼叫） */
CREATE TABLE smp_phone (
  id BIGINT NOT NULL IDENTITY,
  sample_id BIGINT NOT NULL,
  phone_no VARCHAR(32) NOT NULL /* 号码 */,
  phone_type VARCHAR(16) DEFAULT 'MOBILE' /* MOBILE/LANDLINE/OFFICE */,
  sort_no INTEGER NOT NULL DEFAULT 1 /* 呼叫顺序（一人多号排序） */,
  valid_flag TINYINT NOT NULL DEFAULT 1 /* 0=空号/传真等系统自动标记 */,
  last_result_code VARCHAR(32) DEFAULT NULL /* 该号最近一次结果码 */,
  PRIMARY KEY (id)
) /* 样本电话号码（一人多号） */;

/* 自定义样本字段定义（自定义字段 + 字典表） */
CREATE TABLE smp_field_def (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  field_code VARCHAR(64) NOT NULL /* 字段编码（映射 ext_json.key） */,
  field_name VARCHAR(64) NOT NULL,
  field_type VARCHAR(16) NOT NULL DEFAULT 'TEXT' /* TEXT/NUMBER/DATE/DICT */,
  dict_code VARCHAR(64) DEFAULT NULL /* 关联字典（field_type=DICT） */,
  required TINYINT NOT NULL DEFAULT 0,
  sort_no INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  CONSTRAINT uk_field UNIQUE (
    domain_id,
    field_code
  )
) /* 样本自定义字段定义 */;

CREATE TABLE smp_dict (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  dict_code VARCHAR(64) NOT NULL /* 字典编码 */,
  item_value VARCHAR(64) NOT NULL /* 项值 */,
  item_label VARCHAR(128) NOT NULL /* 项显示名 */,
  parent_id BIGINT DEFAULT NULL /* 上级项（级联字典） */,
  sort_no INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
) /* 数据字典 */;

CREATE TABLE smp_blacklist (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  phone_no VARCHAR(32) NOT NULL /* 禁呼号码 */,
  scope VARCHAR(16) NOT NULL DEFAULT 'GLOBAL' /* GLOBAL全局/PROJECT项目级 */,
  project_id BIGINT DEFAULT NULL,
  reason VARCHAR(255) DEFAULT NULL /* 原因（拒访/投诉/法规） */,
  created_by BIGINT NOT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  PRIMARY KEY (id),
  CONSTRAINT uk_black UNIQUE (
    domain_id,
    phone_no,
    scope,
    project_id
  )
) /* 黑名单 */;

/* 电话结束状态码（每个电话结束状态可自定义） */
CREATE TABLE smp_status_code (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  code VARCHAR(32) NOT NULL /* 状态码（如 NA/BUSY/SUCCESS） */,
  name VARCHAR(64) NOT NULL /* 名称（无人接听/占线/成功） */,
  category VARCHAR(16) NOT NULL /* SUCCESS成功/FAIL失败/NEUTRAL中性/APPOINT预约 */,
  closed_flag TINYINT NOT NULL DEFAULT 1 /* 1=闭环（样本不再回池）；0=可重拨 */,
  allow_redial TINYINT NOT NULL DEFAULT 0 /* 允许重拨 */,
  hit_black_flag TINYINT NOT NULL DEFAULT 0 /* 命中后自动加入黑名单（如空号/拒访） */,
  sort_no INTEGER NOT NULL DEFAULT 0,
  status TINYINT NOT NULL DEFAULT 1,
  PRIMARY KEY (id),
  CONSTRAINT uk_status_code UNIQUE (
    domain_id,
    code
  )
) /* 电话结束状态码表 */;

/* 样本分配/回收流水 */
CREATE TABLE smp_assign_log (
  id BIGINT NOT NULL IDENTITY,
  sample_id BIGINT NOT NULL,
  project_id BIGINT NOT NULL,
  group_id BIGINT DEFAULT NULL,
  agent_id BIGINT DEFAULT NULL,
  action VARCHAR(16) NOT NULL /* ASSIGN分配/RETURN回收/REDIAL重拨池/BAN禁呼 */,
  assigned_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  returned_at DATETIME2(3) DEFAULT NULL,
  PRIMARY KEY (id)
) /* 样本分配流水 */;

/* ============================================================================ */
/* 05 呼叫策略域（对应菜单：M06 呼叫策略） */
/*    模式：PREDICTIVE智能预测/PREVIEW预览/AUTO自动/MIXED混合；VIP 语音方案 */
/* ============================================================================ */
CREATE TABLE strat_strategy (
  id BIGINT NOT NULL IDENTITY(1,1) /* 策略ID */,
  domain_id BIGINT NOT NULL,
  project_id BIGINT NOT NULL /* 所属项目 */,
  strategy_name VARCHAR(128) NOT NULL,
  call_mode VARCHAR(16) NOT NULL DEFAULT 'PREVIEW' /* PREDICTIVE/PREVIEW/AUTO/MIXED */,
  work_order_mode TINYINT NOT NULL DEFAULT 0 /* 工单模式（NK3C 呼入工单联动） */,
  max_abandon_rate NUMERIC(5, 2) NOT NULL DEFAULT 3.00 /* 呼损率上限 %（预测外呼控制目标） */,
  dial_ratio NUMERIC(6, 2) NOT NULL DEFAULT 1.50 /* 拨号倍率（预测算法初值，运行期自调整） */,
  redial_max INTEGER NOT NULL DEFAULT 3 /* 同一号码最大重拨次数 */,
  redial_interval_min INTEGER NOT NULL DEFAULT 60 /* 重拨最小间隔（分钟） */,
  caller_no VARCHAR(32) DEFAULT NULL /* 项目主叫号码显示 */,
  per_agent_caller TINYINT NOT NULL DEFAULT 0 /* 1=允许为访问员单独设置主叫号码 */,
  vip_mode TINYINT NOT NULL DEFAULT 0 /* VIP 语音方案（复述确认/合成语音） */,
  status TINYINT NOT NULL DEFAULT 1,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  updated_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME() /* updated_at: 需触发器/应用层维护 */,
  PRIMARY KEY (id)
) /* 呼叫策略 */;

CREATE TABLE strat_time_window (
  id BIGINT NOT NULL IDENTITY,
  strategy_id BIGINT NOT NULL,
  weekdays VARCHAR(16) NOT NULL DEFAULT '1,2,3,4,5' /* 允许呼叫的星期（1=周一） */,
  begin_time TIME NOT NULL,
  end_time TIME NOT NULL,
  PRIMARY KEY (id)
) /* 策略允许呼叫时段 */;

CREATE TABLE strat_caller_assign (
  id BIGINT NOT NULL IDENTITY,
  strategy_id BIGINT NOT NULL,
  agent_id BIGINT NOT NULL /* 访问员 */,
  caller_no VARCHAR(32) NOT NULL /* 该坐席外呼主显号码 */,
  PRIMARY KEY (id),
  CONSTRAINT uk_caller UNIQUE (
    strategy_id,
    agent_id
  )
) /* 坐席主叫号码分配 */;

/* ============================================================================ */
/* 06 话务 CTI 域（对应菜单：M07 执行管理 / M08 实时监控） */
/* ============================================================================ */
CREATE TABLE cti_call_record (
  id BIGINT NOT NULL IDENTITY(1,1) /* 话务记录ID */,
  domain_id BIGINT NOT NULL,
  project_id BIGINT DEFAULT NULL /* 项目（呼入可为空） */,
  strategy_id BIGINT DEFAULT NULL,
  sample_id BIGINT DEFAULT NULL /* 关联样本（呼入可按主叫匹配） */,
  agent_id BIGINT DEFAULT NULL /* 应答坐席（无人接听类为 NULL） */,
  call_direction VARCHAR(8) NOT NULL DEFAULT 'OUT' /* OUT外呼/IN呼入 */,
  caller_no VARCHAR(32) NOT NULL /* 主叫 */,
  called_no VARCHAR(32) NOT NULL /* 被叫 */,
  begin_time DATETIME2(3) DEFAULT NULL /* 呼叫发起 */,
  connect_time DATETIME2(3) DEFAULT NULL /* 接通时间 */,
  end_time DATETIME2(3) DEFAULT NULL /* 结束时间 */,
  ring_duration_sec INTEGER NOT NULL DEFAULT 0 /* 振铃时长 */,
  talk_duration_sec INTEGER NOT NULL DEFAULT 0 /* 通话时长 */,
  result_code VARCHAR(32) DEFAULT NULL /* 结束状态码（smp_status_code.code） */,
  transfer_flag TINYINT NOT NULL DEFAULT 0 /* 是否转接 */,
  listened_flag TINYINT NOT NULL DEFAULT 0 /* 是否被监听过 */,
  recording_id BIGINT DEFAULT NULL /* 录音ID */,
  call_seq VARCHAR(48) DEFAULT NULL /* CTI 平台话务唯一标识（CTI Proxy 映射） */,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  PRIMARY KEY (id)
) /* 话务记录（每通电话一条） */;

CREATE TABLE cti_recording (
  id BIGINT NOT NULL IDENTITY,
  call_id BIGINT NOT NULL /* 话务记录 */,
  file_path VARCHAR(500) NOT NULL /* 录音文件路径/对象存储键 */,
  file_format VARCHAR(8) NOT NULL DEFAULT 'wav' /* wav/mp3 */,
  file_size BIGINT NOT NULL DEFAULT 0,
  duration_sec INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  PRIMARY KEY (id)
) /* 通话录音 */;

/* 坐席状态流水（签入/示闲/通话/后处理/示忙/签出） */
CREATE TABLE cti_agent_state_log (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  agent_id BIGINT NOT NULL,
  state VARCHAR(16) NOT NULL /* LOGIN签入/READY示闲/BUSY通话/ACW后处理/AUX示忙/LOGOUT签出 */,
  reason VARCHAR(64) DEFAULT NULL /* 示忙原因码 */,
  begin_time DATETIME2(3) NOT NULL,
  end_time DATETIME2(3) DEFAULT NULL,
  PRIMARY KEY (id)
) /* 坐席状态流水 */;

/* 督导实时操作（监听/插话/强制示忙/示闲/挂断/签出/发消息） */
CREATE TABLE cti_monitor_event (
  id BIGINT NOT NULL IDENTITY,
  call_id BIGINT DEFAULT NULL /* 关联话务（监听/插话/挂断） */,
  monitor_id BIGINT NOT NULL /* 督导 */,
  target_agent_id BIGINT DEFAULT NULL /* 目标坐席 */,
  action VARCHAR(16) NOT NULL /* LISTEN监听/BARGE插话/FORCEBUSY/FORCEIDLE/FORCEHANGUP/FORCELOGOUT/MESSAGE发消息 */,
  message_text VARCHAR(500) DEFAULT NULL /* MESSAGE 内容 */,
  event_time DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  PRIMARY KEY (id)
) /* 督导实时操作事件 */;

/* ============================================================================ */
/* 07 答卷域（对应菜单：M09 答卷管理；审核/编码/预约） */
/* ============================================================================ */
CREATE TABLE ans_sheet (
  id BIGINT NOT NULL IDENTITY(1,1) /* 答卷ID */,
  domain_id BIGINT NOT NULL,
  call_id BIGINT NOT NULL /* 来源话务 */,
  project_id BIGINT NOT NULL,
  sample_id BIGINT NOT NULL,
  agent_id BIGINT NOT NULL /* 作答坐席 */,
  qnr_id BIGINT NOT NULL /* 问卷（含版本） */,
  qnr_version VARCHAR(16) DEFAULT NULL /* 作答时问卷版本（中途调整问卷的关键） */,
  begin_time DATETIME2(3) NOT NULL,
  submit_time DATETIME2(3) DEFAULT NULL,
  duration_sec INTEGER NOT NULL DEFAULT 0 /* 答卷用时 */,
  status VARCHAR(16) NOT NULL DEFAULT 'DOING' /* DOING作答中/SUBMITTED已提交/AUDITED审核通过/REJECTED驳回/VOID作废 */,
  audit_by BIGINT DEFAULT NULL,
  audit_at DATETIME2(3) DEFAULT NULL,
  audit_remark VARCHAR(500) DEFAULT NULL,
  audit_score NUMERIC(6, 2) DEFAULT NULL /* 审核指标得分（NK3C 答卷审核指标） */,
  PRIMARY KEY (id)
) /* 答卷（一通成功电话一份） */;

CREATE TABLE ans_answer (
  id BIGINT NOT NULL IDENTITY,
  sheet_id BIGINT NOT NULL,
  question_id BIGINT NOT NULL /* 题目（qnr_question.id） */,
  q_no INTEGER NOT NULL /* 题号快照 */,
  answer_text VARCHAR(MAX) DEFAULT NULL /* 开放题文本 */,
  option_ids NVARCHAR(MAX) DEFAULT NULL /* 选中选项 id 数组 */,
  option_values NVARCHAR(MAX) DEFAULT NULL /* 选项编码快照（导出用） */,
  numeric_value NUMERIC(12, 4) DEFAULT NULL /* 数字/刻度/分配值 */,
  sort_values NVARCHAR(MAX) DEFAULT NULL /* 排序值 */,
  matrix_values NVARCHAR(MAX) DEFAULT NULL /* 矩阵/三维表格值 */,
  answered_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  PRIMARY KEY (id),
  CONSTRAINT uk_ans UNIQUE (
    sheet_id,
    question_id
  )
) /* 答卷明细（每题一条） */;

/* 开放题编码（机编候选 + 人工赋码） */
CREATE TABLE ans_code (
  id BIGINT NOT NULL IDENTITY,
  sheet_id BIGINT NOT NULL,
  question_id BIGINT NOT NULL,
  code VARCHAR(32) NOT NULL /* 码值 */,
  code_name VARCHAR(128) DEFAULT NULL /* 码含义 */,
  code_source VARCHAR(16) NOT NULL DEFAULT 'MANUAL' /* AUTO机器编码/MANUAL人工 */,
  coder_id BIGINT DEFAULT NULL,
  coded_at DATETIME2(3) DEFAULT NULL,
  PRIMARY KEY (id)
) /* 开放题编码 */;

/* 预约回拨 */
CREATE TABLE ans_appointment (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  project_id BIGINT NOT NULL,
  sample_id BIGINT NOT NULL,
  agent_id BIGINT DEFAULT NULL /* 预约给谁（可空=任意坐席） */,
  phone_no VARCHAR(32) DEFAULT NULL /* 指定回拨号码 */,
  appt_time DATETIME2(3) NOT NULL /* 预约时间 */,
  status VARCHAR(16) NOT NULL DEFAULT 'WAITING' /* WAITING/DONE/CANCELED/EXPIRED */,
  remark VARCHAR(255) DEFAULT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  done_at DATETIME2(3) DEFAULT NULL,
  PRIMARY KEY (id)
) /* 预约回拨 */;

/* ============================================================================ */
/* 08 质检域（对应菜单：M12 质检管理） */
/* ============================================================================ */
CREATE TABLE qc_template (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL /* 评分表名 */,
  total_score NUMERIC(6, 2) NOT NULL DEFAULT 100,
  status TINYINT NOT NULL DEFAULT 1,
  PRIMARY KEY (id)
) /* 质检评分表模板 */;

CREATE TABLE qc_template_item (
  id BIGINT NOT NULL IDENTITY,
  template_id BIGINT NOT NULL,
  item_name VARCHAR(128) NOT NULL /* 评分项（开场白规范/业务准确...） */,
  max_score NUMERIC(6, 2) NOT NULL,
  weight NUMERIC(6, 2) NOT NULL DEFAULT 0,
  defect_flag TINYINT NOT NULL DEFAULT 0 /* 1=致命缺陷项（一票否决） */,
  sort_no INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
) /* 评分表条目 */;

CREATE TABLE qc_task (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  task_name VARCHAR(128) NOT NULL,
  project_id BIGINT DEFAULT NULL,
  sample_rule VARCHAR(16) NOT NULL DEFAULT 'RATE' /* RATE按比例/AGENT指定坐席/MANUAL手工 */,
  rate NUMERIC(5, 2) DEFAULT NULL /* 抽检比例 % */,
  target_scope_json NVARCHAR(MAX) DEFAULT NULL /* 指定坐席/时间范围 */,
  status VARCHAR(16) NOT NULL DEFAULT 'OPEN' /* OPEN/CLOSED */,
  created_by BIGINT NOT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  PRIMARY KEY (id)
) /* 质检任务 */;

CREATE TABLE qc_result (
  id BIGINT NOT NULL IDENTITY,
  task_id BIGINT NOT NULL,
  call_id BIGINT NOT NULL /* 被质检话务（录音质检） */,
  sheet_id BIGINT DEFAULT NULL /* 关联答卷（答卷质检） */,
  workorder_id BIGINT DEFAULT NULL /* 关联工单（工单质检） */,
  qc_type VARCHAR(16) NOT NULL DEFAULT 'RECORDING' /* RECORDING录音/SHEET答卷/WORKORDER工单 */,
  qc_user_id BIGINT NOT NULL,
  score NUMERIC(6, 2) NOT NULL DEFAULT 0,
  defect_json NVARCHAR(MAX) DEFAULT NULL /* 缺陷明细 */,
  qualification TINYINT NOT NULL DEFAULT 1 /* 1合格 0不合格 */,
  appeal_status VARCHAR(16) DEFAULT NULL /* 申诉状态 NONE/APPEALING/RESOLVED */,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  PRIMARY KEY (id)
) /* 质检结果 */;

/* ============================================================================ */
/* 09 工单域（对应菜单：M13 工单管理；NK3C 呼入业务） */
/* ============================================================================ */
CREATE TABLE wfm_template (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL /* 工作流模板名（如 投诉处理流） */,
  version INTEGER NOT NULL DEFAULT 1,
  status TINYINT NOT NULL DEFAULT 1,
  PRIMARY KEY (id)
) /* 工单工作流模板 */;

CREATE TABLE wfm_node (
  id BIGINT NOT NULL IDENTITY,
  template_id BIGINT NOT NULL,
  node_code VARCHAR(32) NOT NULL /* 节点编码（ACCEPT受理/HANDLE处理/VERIFY回访/CLOSE归档） */,
  node_name VARCHAR(64) NOT NULL,
  node_type VARCHAR(16) NOT NULL DEFAULT 'TASK' /* TASK任务/APPROVAL审批/AUTO回访自动生成 */,
  assign_rule VARCHAR(32) DEFAULT NULL /* 分派规则（ROUND_ROBIN/BY_GROUP/MANUAL） */,
  sla_hours INTEGER DEFAULT NULL /* 时限（超时升级） */,
  next_json NVARCHAR(MAX) DEFAULT NULL /* 流转条件与下一节点 */,
  sort_no INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
) /* 工作流节点 */;

CREATE TABLE wfm_field_def (
  id BIGINT NOT NULL IDENTITY,
  template_id BIGINT NOT NULL,
  field_code VARCHAR(64) NOT NULL,
  field_name VARCHAR(64) NOT NULL,
  field_type VARCHAR(16) NOT NULL DEFAULT 'TEXT' /* TEXT/NUMBER/DATE/DICT/ATTACH */,
  required TINYINT NOT NULL DEFAULT 0,
  layout_json NVARCHAR(MAX) DEFAULT NULL /* 字段布局（位置/宽度） */,
  sort_no INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
) /* 工单自定义字段定义 */;

CREATE TABLE wfm_workorder (
  id BIGINT NOT NULL IDENTITY(1,1) /* 工单ID */,
  domain_id BIGINT NOT NULL,
  wo_no VARCHAR(32) NOT NULL /* 工单号（唯一） */,
  template_id BIGINT NOT NULL,
  project_id BIGINT DEFAULT NULL /* 呼入项目（可关联） */,
  call_id BIGINT DEFAULT NULL /* 来源话务 */,
  source_channel VARCHAR(16) NOT NULL DEFAULT 'PHONE' /* PHONE/WECHAT/WEIBO/SMS/EMAIL/WEBCHAT */,
  cust_phone VARCHAR(32) DEFAULT NULL,
  cust_name VARCHAR(64) DEFAULT NULL,
  title VARCHAR(255) NOT NULL,
  content VARCHAR(MAX) DEFAULT NULL,
  priority VARCHAR(8) NOT NULL DEFAULT 'MID' /* HIGH/MID/LOW */,
  current_node VARCHAR(32) DEFAULT NULL /* 当前节点编码 */,
  assignee_id BIGINT DEFAULT NULL /* 当前处理人 */,
  status VARCHAR(16) NOT NULL DEFAULT 'OPEN' /* OPEN/PROCESSING/WAITING回访/CLOSED/CANCELED */,
  sla_due DATETIME2(3) DEFAULT NULL /* 超时点 */,
  created_by BIGINT NOT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  closed_at DATETIME2(3) DEFAULT NULL,
  updated_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME() /* updated_at: 需触发器/应用层维护 */,
  PRIMARY KEY (id),
  CONSTRAINT uk_wo_no UNIQUE (
    wo_no
  )
) /* 工单 */;

CREATE TABLE wfm_field_value (
  id BIGINT NOT NULL IDENTITY,
  workorder_id BIGINT NOT NULL,
  field_id BIGINT NOT NULL,
  value_text VARCHAR(MAX) DEFAULT NULL,
  PRIMARY KEY (id),
  CONSTRAINT uk_wfv UNIQUE (
    workorder_id,
    field_id
  )
) /* 工单字段值 */;

CREATE TABLE wfm_action_log (
  id BIGINT NOT NULL IDENTITY,
  workorder_id BIGINT NOT NULL,
  node_code VARCHAR(32) DEFAULT NULL,
  action VARCHAR(32) NOT NULL /* CREATE/TRANSFER/COMMENT/URGE/CLOSE/REOPEN */,
  operator_id BIGINT NOT NULL,
  remark VARCHAR(500) DEFAULT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  PRIMARY KEY (id)
) /* 工单操作流水 */;

/* ============================================================================ */
/* 10 IVR 域（对应菜单：M14 IVR 管理；任意级数/回合跳转 + 语音留言） */
/* ============================================================================ */
CREATE TABLE ivr_flow (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL /* 流程名（如 办证结果查询流） */,
  inbound_no VARCHAR(32) DEFAULT NULL /* 绑定的呼入号码 */,
  version INTEGER NOT NULL DEFAULT 1,
  status VARCHAR(16) NOT NULL DEFAULT 'DRAFT' /* DRAFT/PUBLISHED/OFFLINE */,
  PRIMARY KEY (id)
) /* IVR 流程 */;

CREATE TABLE ivr_node (
  id BIGINT NOT NULL IDENTITY,
  flow_id BIGINT NOT NULL,
  node_code VARCHAR(32) NOT NULL /* 节点编码 */,
  node_type VARCHAR(16) NOT NULL /* PLAY播报/MENU按键菜单/QUERY外部查询接口/RECORD留言/TRANSFER转人工/COND条件/TIMECHECK时段 */,
  node_name VARCHAR(64) NOT NULL,
  param_json NVARCHAR(MAX) DEFAULT NULL /* 参数（播报音频/按键位表/查询接口/转接技能组） */,
  next_json NVARCHAR(MAX) DEFAULT NULL /* 分支路由（按键1->节点X ...） */,
  sort_no INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
) /* IVR 节点 */;

CREATE TABLE ivr_audio (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL,
  file_path VARCHAR(500) NOT NULL,
  tts_text VARCHAR(MAX) DEFAULT NULL /* TTS 合成文本 */,
  duration_sec INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id)
) /* IVR 语音文件 */;

CREATE TABLE ivr_message (
  id BIGINT NOT NULL IDENTITY,
  flow_id BIGINT NOT NULL,
  caller_no VARCHAR(32) NOT NULL /* 留言来电号码 */,
  audio_path VARCHAR(500) NOT NULL,
  duration_sec INTEGER NOT NULL DEFAULT 0,
  status VARCHAR(16) NOT NULL DEFAULT 'NEW' /* NEW/LISTENED/TO_WORKORDER */,
  workorder_id BIGINT DEFAULT NULL /* 转工单 */,
  received_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  PRIMARY KEY (id)
) /* IVR 留言 */;

/* ============================================================================ */
/* 11 导出域（对应菜单：M11 数据导出）Excel / SPSS / Quantum / Txt */
/* ============================================================================ */
CREATE TABLE exp_task (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL,
  project_id BIGINT DEFAULT NULL,
  export_type VARCHAR(16) NOT NULL /* DATA答卷数据/RECORDING录音/ACCOUNT账号/QUOTA配额条件 */,
  file_format VARCHAR(16) NOT NULL /* XLSX/SPSS/QUANTUM/TXT/ZIP */,
  filter_json NVARCHAR(MAX) DEFAULT NULL /* 范围条件（结果码/时间段/坐席） */,
  include_dict TINYINT NOT NULL DEFAULT 1 /* 是否携带变量字典（SPSS 元数据） */,
  status VARCHAR(16) NOT NULL DEFAULT 'PENDING' /* PENDING/RUNNING/DONE/FAILED */,
  file_path VARCHAR(500) DEFAULT NULL,
  approved_by BIGINT DEFAULT NULL /* 导出审批人（个保合规） */,
  created_by BIGINT NOT NULL,
  created_at DATETIME2(3) NOT NULL DEFAULT SYSDATETIME(),
  finished_at DATETIME2(3) DEFAULT NULL,
  PRIMARY KEY (id)
) /* 导出任务 */;

/* ============================================================================ */
/* 12 统计域（对应菜单：M10 统计报表；实时部分用 redis/内存，落地部分如下） */
/* ============================================================================ */
CREATE TABLE rpt_day_project (
  id BIGINT NOT NULL IDENTITY,
  stat_date DATE NOT NULL,
  project_id BIGINT NOT NULL,
  dial_count INTEGER NOT NULL DEFAULT 0 /* 呼出总数 */,
  connect_count INTEGER NOT NULL DEFAULT 0 /* 接通数 */,
  success_count INTEGER NOT NULL DEFAULT 0 /* 成功答卷数 */,
  fail_count INTEGER NOT NULL DEFAULT 0,
  appoint_count INTEGER NOT NULL DEFAULT 0,
  total_talk_sec BIGINT NOT NULL DEFAULT 0,
  abandon_count INTEGER NOT NULL DEFAULT 0 /* 呼损（接通无坐席放弃） */,
  PRIMARY KEY (id),
  CONSTRAINT uk_rdp UNIQUE (
    stat_date,
    project_id
  )
) /* 项目日统计（预聚合） */;

CREATE TABLE rpt_day_agent (
  id BIGINT NOT NULL IDENTITY,
  stat_date DATE NOT NULL,
  agent_id BIGINT NOT NULL,
  project_id BIGINT NOT NULL,
  dial_count INTEGER NOT NULL DEFAULT 0,
  connect_count INTEGER NOT NULL DEFAULT 0,
  success_count INTEGER NOT NULL DEFAULT 0,
  talk_sec BIGINT NOT NULL DEFAULT 0,
  online_sec BIGINT NOT NULL DEFAULT 0 /* 签入时长 */,
  busy_sec BIGINT NOT NULL DEFAULT 0,
  qc_score NUMERIC(6, 2) DEFAULT NULL /* 当日质检均分 */,
  PRIMARY KEY (id),
  CONSTRAINT uk_rda UNIQUE (
    stat_date,
    agent_id,
    project_id
  )
) /* 坐席日统计（绩效考核） */;

/* 单题/交叉统计从 ans_answer 实时聚合；大表用如下汇总表加速 */
CREATE TABLE rpt_question_stat (
  id BIGINT NOT NULL IDENTITY,
  project_id BIGINT NOT NULL,
  question_id BIGINT NOT NULL,
  option_value VARCHAR(64) NOT NULL DEFAULT '' /* 选项编码（开放题为码值） */,
  hit_count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  CONSTRAINT uk_rqs UNIQUE (
    project_id,
    question_id,
    option_value
  )
) /* 单题频数汇总 */;

/* ============================================================================ */
/* 13 系统参数（半年原则天数、清理策略、许可证等） */
/* ============================================================================ */
CREATE TABLE sys_param (
  id BIGINT NOT NULL IDENTITY,
  domain_id BIGINT NOT NULL DEFAULT 0 /* 0=全局 */,
  param_code VARCHAR(64) NOT NULL,
  param_value VARCHAR(500) NOT NULL,
  remark VARCHAR(255) DEFAULT NULL,
  PRIMARY KEY (id),
  CONSTRAINT uk_param UNIQUE (
    domain_id,
    param_code
  )
) /* 系统参数 */;

/* ============================================================================ */
/* 14 常用视图 */
/* ============================================================================ */
/* 项目执行进度（项目监控菜单实时首页数据源） */
CREATE OR ALTER VIEW v_project_progress AS
SELECT
  p.id AS project_id,
  p.project_code,
  p.project_name,
  p.status,
  COUNT_BIG(s.id) AS sample_total,
  SUM(s.status = 'SUCCESS') AS sample_success,
  SUM(s.status = 'FAIL') AS sample_fail,
  SUM(s.status IN ('IDLE', 'RECYCLED')) AS sample_left,
  SUM(s.status = 'APPOINT') AS sample_appoint,
  ROUND(
    COALESCE(
      CAST(SUM(s.status = 'SUCCESS') AS FLOAT) / NULLIF(NULLIF(COUNT_BIG(s.id), 0), 0) * 100,
      0
    ),
    2
  ) AS success_rate_pct
FROM prj_project AS p
LEFT JOIN smp_sample AS s
  ON s.project_id = p.id
GROUP BY
  p.id,
  p.project_code,
  p.project_name,
  p.status;

/* 配额完成进度 */
CREATE OR ALTER VIEW v_quota_progress AS
SELECT
  q.id AS quota_id,
  q.qnr_id,
  q.quota_name,
  q.total_target,
  SUM(c.target_count) AS cell_target_sum,
  SUM(c.done_count) AS cell_done_sum,
  ROUND(
    COALESCE(
      CAST(SUM(c.done_count) AS FLOAT) / NULLIF(NULLIF(SUM(c.target_count), 0), 0) * 100,
      0
    ),
    2
  ) AS done_pct
FROM qnr_quota AS q
LEFT JOIN qnr_quota_cell AS c
  ON c.quota_id = q.id
GROUP BY
  q.id,
  q.qnr_id,
  q.quota_name,
  q.total_target;

/* 坐席当日实时效率（执行总览） */
CREATE OR ALTER VIEW v_agent_today AS
SELECT
  a.id AS agent_id,
  a.user_no,
  a.user_name,
  g.group_name,
  COALESCE(SUM(r.dial_count), 0) AS dial_count,
  COALESCE(SUM(r.connect_count), 0) AS connect_count,
  COALESCE(SUM(r.success_count), 0) AS success_count,
  COALESCE(SUM(r.talk_sec), 0) AS talk_sec,
  COALESCE(SUM(r.online_sec), 0) AS online_sec
FROM sys_user AS a
LEFT JOIN org_group AS g
  ON g.id = a.group_id
LEFT JOIN rpt_day_agent AS r
  ON r.agent_id = a.id AND r.stat_date = GETDATE()
WHERE
  a.status = 1
GROUP BY
  a.id,
  a.user_no,
  a.user_name,
  g.group_name;

/* ============================================================================ */
/* 15 初始化数据：菜单资源树 / 角色 / 权限 / 状态码 / 参数 / 演示组织 */
/*    （与《开发文档》第 6.16 节菜单树一一对应） */
/* ============================================================================ */
/* 一级菜单（M01~M15） */
INSERT INTO sys_resource (
  id,
  parent_id,
  resource_type,
  resource_code,
  resource_name,
  url,
  sort_no
)
VALUES
  (101, 0, 'MENU', 'm01.workspace', '工作台', '/workspace', 1),
  (102, 0, 'MENU', 'm02.org', '组织权限', '/org', 2),
  (103, 0, 'MENU', 'm03.project', '项目管理', '/project', 3),
  (104, 0, 'MENU', 'm04.questionnaire', '问卷管理', '/questionnaire', 4),
  (105, 0, 'MENU', 'm05.sample', '样本管理', '/sample', 5),
  (106, 0, 'MENU', 'm06.strategy', '呼叫策略', '/strategy', 6),
  (107, 0, 'MENU', 'm07.execution', '执行管理', '/execution', 7),
  (108, 0, 'MENU', 'm08.monitor', '实时监控', '/monitor', 8),
  (109, 0, 'MENU', 'm09.answer', '答卷管理', '/answer', 9),
  (110, 0, 'MENU', 'm10.report', '统计报表', '/report', 10),
  (111, 0, 'MENU', 'm11.export', '数据导出', '/export', 11),
  (112, 0, 'MENU', 'm12.qc', '质检管理', '/qc', 12),
  (113, 0, 'MENU', 'm13.workorder', '工单管理', '/workorder', 13),
  (114, 0, 'MENU', 'm14.ivr', 'IVR管理', '/ivr', 14),
  (115, 0, 'MENU', 'm15.system', '系统维护', '/system', 15);

/* 二级菜单（核心模块全量，其余模块按同一模式扩展） */
INSERT INTO sys_resource (
  id,
  parent_id,
  resource_type,
  resource_code,
  resource_name,
  url,
  sort_no
)
VALUES
  (1021, 102, 'MENU', 'm02.domain', '域管理', '/org/domain', 1),
  (1022, 102, 'MENU', 'm02.orgmgmt', '组织管理', '/org/org', 2),
  (1023, 102, 'MENU', 'm02.group', '组管理', '/org/group', 3),
  (1024, 102, 'MENU', 'm02.user', '员工管理', '/org/user', 4),
  (1025, 102, 'MENU', 'm02.role', '角色与资源授权', '/org/role', 5),
  (1026, 102, 'MENU', 'm02.oplog', '操作日志', '/org/oplog', 6),
  (1031, 103, 'MENU', 'm03.list', '项目列表', '/project/list', 1),
  (1032, 103, 'MENU', 'm03.wizard', '新建项目', '/project/wizard', 2),
  (1033, 103, 'MENU', 'm03.dist', '分发管理', '/project/distribution', 3),
  (1034, 103, 'MENU', 'm03.schedule', '项目排班', '/project/schedule', 4),
  (1035, 103, 'MENU', 'm03.archive', '项目归档', '/project/archive', 5),
  (1041, 104, 'MENU', 'm04.list', '问卷列表', '/questionnaire/list', 1),
  (1042, 104, 'MENU', 'm04.designer', '问卷设计器', '/questionnaire/designer', 2),
  (1043, 104, 'MENU', 'm04.logic', '逻辑与配额', '/questionnaire/logic', 3),
  (1044, 104, 'MENU', 'm04.validate', '校验调试', '/questionnaire/validate', 4),
  (1045, 104, 'MENU', 'm04.bank', '题库管理', '/questionnaire/bank', 5),
  (1046, 104, 'MENU', 'm04.template', '模板管理', '/questionnaire/template', 6),
  (1051, 105, 'MENU', 'm05.pool', '样本池', '/sample/pool', 1),
  (1052, 105, 'MENU', 'm05.import', '样本导入', '/sample/import', 2),
  (1053, 105, 'MENU', 'm05.random', '随机抽样', '/sample/random-gen', 3),
  (1054, 105, 'MENU', 'm05.assign', '样本分配', '/sample/assign', 4),
  (1055, 105, 'MENU', 'm05.field', '字段与字典', '/sample/field', 5),
  (1056, 105, 'MENU', 'm05.black', '黑名单/半年原则', '/sample/blacklist', 6),
  (1057, 105, 'MENU', 'm05.statuscode', '样本状态码', '/sample/status-code', 7),
  (1061, 106, 'MENU', 'm06.list', '外呼策略列表', '/strategy/list', 1),
  (1062, 106, 'MENU', 'm06.predict', '预测拨号参数', '/strategy/predictive', 2),
  (1063, 106, 'MENU', 'm06.redial', '重拨规则', '/strategy/redial', 3),
  (1064, 106, 'MENU', 'm06.window', '时段与排班', '/strategy/window', 4),
  (1065, 106, 'MENU', 'm06.caller', '主叫号码管理', '/strategy/caller', 5),
  (1066, 106, 'MENU', 'm06.inbound', '呼入路由', '/strategy/inbound', 6),
  (1071, 107, 'MENU', 'm07.board', '执行总览', '/execution/board', 1),
  (1072, 107, 'MENU', 'm07.agent', '坐席状态', '/execution/agent-state', 2),
  (1073, 107, 'MENU', 'm07.dispatch', '派样监控', '/execution/dispatch', 3),
  (1074, 107, 'MENU', 'm07.appoint', '预约与回拨', '/execution/appointment', 4),
  (1081, 108, 'MENU', 'm08.project', '项目监控', '/monitor/project', 1),
  (1082, 108, 'MENU', 'm08.wall', '坐席监控墙', '/monitor/wall', 2),
  (1083, 108, 'MENU', 'm08.alarm', '报警提醒', '/monitor/alarm', 3),
  (1091, 109, 'MENU', 'm09.list', '答卷列表', '/answer/list', 1),
  (1092, 109, 'MENU', 'm09.audit', '答卷审核', '/answer/audit', 2),
  (1093, 109, 'MENU', 'm09.code', '答卷编码', '/answer/coding', 3),
  (1094, 109, 'MENU', 'm09.recording', '录音管理', '/answer/recording', 4),
  (1101, 110, 'MENU', 'm10.traffic', '话务统计', '/report/traffic', 1),
  (1102, 110, 'MENU', 'm10.sample', '样本统计', '/report/sample', 2),
  (1103, 110, 'MENU', 'm10.single', '单题统计', '/report/single', 3),
  (1104, 110, 'MENU', 'm10.cross', '交叉统计', '/report/cross', 4),
  (1105, 110, 'MENU', 'm10.quota', '配额统计', '/report/quota', 5),
  (1106, 110, 'MENU', 'm10.fee', '话费统计', '/report/fee', 6),
  (1107, 110, 'MENU', 'm10.perf', '坐席绩效', '/report/performance', 7),
  (1111, 111, 'MENU', 'm11.data', '答卷数据导出', '/export/data', 1),
  (1112, 111, 'MENU', 'm11.rec', '录音批量导出', '/export/recording', 2),
  (1113, 111, 'MENU', 'm11.task', '导出任务', '/export/task', 3),
  (1121, 112, 'MENU', 'm12.task', '质检任务', '/qc/task', 1),
  (1122, 112, 'MENU', 'm12.rec', '录音质检', '/qc/recording', 2),
  (1123, 112, 'MENU', 'm12.sheet', '答卷质检', '/qc/sheet', 3),
  (1124, 112, 'MENU', 'm12.tpl', '评分表管理', '/qc/template', 4),
  (1125, 112, 'MENU', 'm12.report', '质检报告', '/qc/report', 5),
  (1131, 113, 'MENU', 'm13.list', '工单列表', '/workorder/list', 1),
  (1132, 113, 'MENU', 'm13.field', '字段与布局', '/workorder/field', 2),
  (1133, 113, 'MENU', 'm13.flow', '工作流模板', '/workorder/flow', 3),
  (1134, 113, 'MENU', 'm13.assign', '工单分派', '/workorder/assign', 4),
  (1135, 113, 'MENU', 'm13.kb', '知识库', '/workorder/knowledge', 5),
  (1141, 114, 'MENU', 'm14.flow', '流程列表', '/ivr/flow', 1),
  (1142, 114, 'MENU', 'm14.designer', '流程设计器', '/ivr/designer', 2),
  (1143, 114, 'MENU', 'm14.audio', '语音文件', '/ivr/audio', 3),
  (1144, 114, 'MENU', 'm14.msg', '留言管理', '/ivr/message', 4),
  (1151, 115, 'MENU', 'm15.param', '参数配置', '/system/param', 1),
  (1152, 115, 'MENU', 'm15.dict', '数据字典', '/system/dict', 2),
  (1153, 115, 'MENU', 'm15.backup', '备份恢复', '/system/backup', 3),
  (1154, 115, 'MENU', 'm15.license', '许可证管理', '/system/license', 4),
  (1155, 115, 'MENU', 'm15.api', '接口管理', '/system/api', 5);

/* 权限型资源（PERM，按 Code 判断——对应 NK3C 公开的 4 类权限） */
INSERT INTO sys_resource (
  id,
  parent_id,
  resource_type,
  resource_code,
  resource_name,
  sort_no
)
VALUES
  (901, 0, 'PERM', 'domainAdmin', '域管理员权限', 1),
  (902, 0, 'PERM', 'orgAdmin', '组织管理员权限', 2),
  (903, 0, 'PERM', 'groupAdmin', '组管理员（督导）权限', 3),
  (904, 0, 'PERM', 'phoneAdmin', '一线员工（坐席）权限', 4);

/* 角色 */
INSERT INTO sys_role (
  id,
  domain_id,
  role_code,
  role_name
)
VALUES
  (1, 0, 'domainAdmin', '域管理员'),
  (2, 0, 'orgAdmin', '组织管理员（项目经理）'),
  (3, 0, 'groupAdmin', '组管理员（督导/质检）'),
  (4, 0, 'phoneAdmin', '一线员工（坐席）');

/* 角色授权示例：域管理员=全部菜单；坐席=仅工作台/执行 */
INSERT INTO sys_role_resource (
  role_id,
  resource_id
)
SELECT
  1,
  id
FROM sys_resource
WHERE
  resource_type = 'MENU';

INSERT INTO sys_role_resource (
  role_id,
  resource_id
)
VALUES
  (4, 101),
  (4, 1071),
  (4, 1074);

INSERT INTO sys_role_resource (
  role_id,
  resource_id
)
VALUES
  (3, 101),
  (3, 108),
  (3, 109),
  (3, 112);

/* 演示域/组织/组/用户 */
INSERT INTO org_domain (
  id,
  domain_code,
  domain_name,
  db_tag
)
VALUES
  (1, 'D001', '演示域（总部）', 'nk3c_main');

INSERT INTO org_org (
  id,
  domain_id,
  parent_id,
  org_code,
  org_name
)
VALUES
  (1, 1, 0, 'ORG01', '调查业务部');

INSERT INTO org_group (
  id,
  domain_id,
  org_id,
  group_code,
  group_name,
  group_type,
  skill_tags
)
VALUES
  (1, 1, 1, 'G-AG01', '呼出一组', 'AGENT', 'mandarin'),
  (2, 1, 1, 'G-SK-CT', '粤语技能组', 'SKILL', 'cantonese');

INSERT INTO sys_user (
  id,
  domain_id,
  org_id,
  group_id,
  user_no,
  user_name,
  login_name,
  password_hash,
  agent_no
)
VALUES
  (1, 1, 1, NULL, 'A0001', '系统管理员', 'admin', '$2a$10$CHANGE_ME_IN_PRODUCTION', NULL),
  (2, 1, 1, 1, '1020', '坐席演示', 'agent01', '$2a$10$CHANGE_ME_IN_PRODUCTION', '1020');

INSERT INTO sys_user_role (
  user_id,
  role_id
)
VALUES
  (1, 1),
  (2, 4);

/* 电话结束状态码（行业通用码表，可直接扩充） */
INSERT INTO smp_status_code (
  domain_id,
  code,
  name,
  category,
  closed_flag,
  allow_redial,
  hit_black_flag,
  sort_no
)
VALUES
  (1, 'SUCCESS', '访问成功', 'SUCCESS', 1, 0, 0, 1),
  (1, 'PARTIAL', '部分完成', 'NEUTRAL', 1, 1, 0, 2),
  (1, 'QUFAIL', '甄别不合格', 'FAIL', 1, 0, 0, 3),
  (1, 'REFUSE', '拒访', 'FAIL', 1, 0, 1, 4),
  (1, 'BREAKOFF', '中途挂断', 'FAIL', 1, 1, 0, 5),
  (1, 'APPOINT', '预约回拨', 'APPOINT', 0, 0, 0, 6),
  (1, 'NA', '无人接听', 'FAIL', 0, 1, 0, 7),
  (1, 'BUSY', '占线', 'FAIL', 0, 1, 0, 8),
  (1, 'INVALID', '空号', 'FAIL', 1, 0, 1, 9),
  (1, 'FAX', '传真/数据线', 'FAIL', 1, 0, 0, 10),
  (1, 'WRONGNO', '号码错误', 'FAIL', 1, 0, 0, 11),
  (1, 'NOTIN', '不符合条件', 'FAIL', 1, 0, 0, 12),
  (1, 'LANGUAGE', '语言不通', 'FAIL', 1, 0, 0, 13);

/* 系统参数（半年原则 = 180 天） */
INSERT INTO sys_param (
  domain_id,
  param_code,
  param_value,
  remark
)
VALUES
  (1, 'halfyear.days', '180', '半年原则：N 天内已访样本禁呼'),
  (1, 'redial.max', '3', '默认重拨上限'),
  (1, 'redial.interval.min', '60', '默认重拨间隔（分钟）'),
  (1, 'predict.abandon.max', '3.0', '预测外呼呼损率上限（%）'),
  (1, 'recording.retention.days', '365', '录音留存天数'),
  (1, 'sample.lock.seconds', '120', '派样锁超时秒数（坐席断线回收）');

/* [mysql-only removed] FOREIGN_KEY_CHECKS */ /* ============================================================================ */ /* 附：核心查询样例 */ /* ---------------------------------------------------------------------------- */ /* 1) 半年原则过滤 + 黑名单排除的取号（派样服务核心 SQL）： */ /* SELECT s.id */ /* FROM smp_sample s */ /* JOIN smp_phone p ON p.sample_id = s.id AND p.valid_flag = 1 */ /* WHERE s.project_id = @pid AND s.status = 'IDLE' */ /*   AND (s.last_called_at IS NULL */ /*        OR s.last_called_at < NOW() - INTERVAL (SELECT CAST(param_value AS UNSIGNED) */ /*            FROM sys_param WHERE param_code='halfyear.days' AND domain_id=s.domain_id) DAY) */ /*   AND NOT EXISTS (SELECT 1 FROM smp_blacklist b */ /*                   WHERE b.domain_id = s.domain_id AND b.phone_no = p.phone_no */ /*                     AND (b.scope='GLOBAL' OR (b.scope='PROJECT' AND b.project_id=s.project_id))) */ /*   AND s.call_attempts < (SELECT CAST(param_value AS UNSIGNED) */ /*            FROM sys_param WHERE param_code='redial.max' AND domain_id=s.domain_id) */ /* ORDER BY s.shuffle_key */ /* LIMIT 1 FOR UPDATE SKIP LOCKED;   -- 8.0 跳过锁：多派样线程不互相阻塞 */ /* 2) 配额原子扣减（命中判定通过后）： */ /* UPDATE qnr_quota_cell SET done_count = done_count + 1 */ /* WHERE id = @cell AND done_count < target_count;   -- 影响行数=0 即超配额，回滚放行逻辑 */ /* 3) 单题频数（实时统计）： */ /* SELECT o.option_values, COUNT(*) FROM ans_answer */ /* WHERE question_id = @qid AND EXISTS (SELECT 1 FROM ans_sheet */ /*   WHERE ans_sheet.id = ans_answer.sheet_id AND status IN ('SUBMITTED','AUDITED')) */ /* GROUP BY o.option_values; */ /* ============================================================================ */

-- ───── 由内联 INDEX 抽取的索引 ─────
CREATE INDEX idx_org_domain ON org_org (domain_id);
CREATE INDEX idx_org_parent ON org_org (parent_id);
CREATE INDEX idx_group_org ON org_group (org_id);
CREATE INDEX idx_group_domain ON org_group (domain_id);
CREATE INDEX idx_user_group ON sys_user (group_id);
CREATE INDEX idx_res_parent ON sys_resource (parent_id);
CREATE INDEX idx_login_user ON sys_login_log (user_id, login_at);
CREATE INDEX idx_op_user ON sys_op_log (user_id, op_at);
CREATE INDEX idx_op_module ON sys_op_log (module, op_at);
CREATE INDEX idx_prj_parent ON prj_project (parent_project_id);
CREATE INDEX idx_prj_status ON prj_project (status);
CREATE INDEX idx_dist_project ON prj_distribution (project_id);
CREATE INDEX idx_sch ON prj_schedule (project_id, shift_date);
CREATE INDEX idx_qnr_domain ON qnr_questionnaire (domain_id, status);
CREATE INDEX idx_q_qnr ON qnr_question (qnr_id);
CREATE INDEX idx_opt_q ON qnr_option (question_id);
CREATE INDEX idx_logic_qnr ON qnr_logic (qnr_id);
CREATE INDEX idx_quota_qnr ON qnr_quota (qnr_id);
CREATE INDEX idx_qc ON qnr_quota_cell (quota_id);
CREATE INDEX idx_bank ON qnr_bank (domain_id, category);
CREATE INDEX idx_smp_project ON smp_sample (project_id, status);
CREATE INDEX idx_smp_group ON smp_sample (owner_group_id, status);
CREATE INDEX idx_smp_batch ON smp_sample (import_batch_no);
CREATE INDEX idx_smp_lastcall ON smp_sample (last_called_at);
CREATE INDEX idx_phone_sample ON smp_phone (sample_id);
CREATE INDEX idx_phone_no ON smp_phone (phone_no);
CREATE INDEX idx_dict ON smp_dict (domain_id, dict_code);
CREATE INDEX idx_assign ON smp_assign_log (sample_id, assigned_at);
CREATE INDEX idx_assign_project ON smp_assign_log (project_id, assigned_at);
CREATE INDEX idx_strat_project ON strat_strategy (project_id);
CREATE INDEX idx_tw ON strat_time_window (strategy_id);
CREATE INDEX idx_call_project ON cti_call_record (project_id, begin_time);
CREATE INDEX idx_call_agent ON cti_call_record (agent_id, begin_time);
CREATE INDEX idx_call_sample ON cti_call_record (sample_id);
CREATE INDEX idx_call_result ON cti_call_record (result_code);
CREATE INDEX idx_rec_call ON cti_recording (call_id);
CREATE INDEX idx_asl_agent ON cti_agent_state_log (agent_id, begin_time);
CREATE INDEX idx_me_target ON cti_monitor_event (target_agent_id, event_time);
CREATE INDEX idx_sheet_project ON ans_sheet (project_id, status);
CREATE INDEX idx_sheet_call ON ans_sheet (call_id);
CREATE INDEX idx_sheet_agent ON ans_sheet (agent_id, submit_time);
CREATE INDEX idx_ans_question ON ans_answer (question_id);
CREATE INDEX idx_code ON ans_code (sheet_id, question_id);
CREATE INDEX idx_appt_time ON ans_appointment (appt_time, status);
CREATE INDEX idx_qti ON qc_template_item (template_id);
CREATE INDEX idx_qcr_task ON qc_result (task_id);
CREATE INDEX idx_qcr_agent_call ON qc_result (call_id);
CREATE INDEX idx_wfnode ON wfm_node (template_id);
CREATE INDEX idx_wfld ON wfm_field_def (template_id);
CREATE INDEX idx_wo_status ON wfm_workorder (domain_id, status);
CREATE INDEX idx_wo_phone ON wfm_workorder (cust_phone);
CREATE INDEX idx_woa ON wfm_action_log (workorder_id, created_at);
CREATE INDEX idx_ivrn ON ivr_node (flow_id);
CREATE INDEX idx_ivrm ON ivr_message (flow_id, received_at);
CREATE INDEX idx_exp ON exp_task (domain_id, status);
