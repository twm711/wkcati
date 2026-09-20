-- +goose Up
-- NK3C MySQL 8.0 schema rehearsal (utf8mb4 / InnoDB)
-- 种子 ID 为开发演示小 ID（生产以 Snowflake 应用层生成）
CREATE TABLE sys_user(
  id BIGINT PRIMARY KEY, login_name VARCHAR(255) UNIQUE, password VARCHAR(255), user_name VARCHAR(255),
  agent_no VARCHAR(255), roles VARCHAR(255), status BIGINT DEFAULT 1) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE sys_param(param_code VARCHAR(255) PRIMARY KEY, param_value VARCHAR(255)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE prj_project(id BIGINT PRIMARY KEY, project_code VARCHAR(255), project_name VARCHAR(255),
  status VARCHAR(255) DEFAULT 'DRAFT', questionnaire_id BIGINT) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE qnr_questionnaire(id BIGINT PRIMARY KEY, title VARCHAR(255), version VARCHAR(255), status VARCHAR(255)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE qnr_question(id BIGINT PRIMARY KEY, qnr_id BIGINT, q_no BIGINT, q_type VARCHAR(255),
  title VARCHAR(255), required BIGINT DEFAULT 1, min_value DOUBLE, max_value DOUBLE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE qnr_option(id BIGINT PRIMARY KEY, question_id BIGINT, opt_no BIGINT,
  opt_text VARCHAR(255), opt_value VARCHAR(255), jump VARCHAR(255), jump_target BIGINT) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE qnr_quota(id BIGINT PRIMARY KEY, qnr_id BIGINT, quota_name VARCHAR(255), total_target BIGINT) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE qnr_quota_cell(id BIGINT PRIMARY KEY, quota_id BIGINT, conditions_json VARCHAR(255),
  target_count BIGINT DEFAULT 0, done_count BIGINT DEFAULT 0) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE smp_sample(id BIGINT PRIMARY KEY, project_id BIGINT, cust_name VARCHAR(255), gender VARCHAR(255),
  status VARCHAR(255) DEFAULT 'IDLE', ext_json VARCHAR(255), attempts BIGINT DEFAULT 0,
  last_connected_at VARCHAR(255), shuffle_key BIGINT, owner_agent_id BIGINT) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE smp_phone(id BIGINT PRIMARY KEY, sample_id BIGINT, phone_no VARCHAR(255),
  sort_no BIGINT DEFAULT 1, valid_flag BIGINT DEFAULT 1) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE smp_blacklist(id BIGINT PRIMARY KEY, phone_no VARCHAR(255) UNIQUE, scope VARCHAR(255) DEFAULT 'GLOBAL', reason VARCHAR(255)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE smp_status_code(code VARCHAR(255) PRIMARY KEY, name VARCHAR(255), category VARCHAR(255),
  closes_call BIGINT, reopen_sample BIGINT, hit_black_flag BIGINT) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE cti_call_record(id BIGINT PRIMARY KEY, project_id BIGINT, sample_id BIGINT,
  agent_id BIGINT, agent_no VARCHAR(255), caller_no VARCHAR(255), called_no VARCHAR(255), status VARCHAR(255),
  begin_time VARCHAR(255), connect_time VARCHAR(255), end_time VARCHAR(255), result_code VARCHAR(255)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE ans_sheet(id BIGINT PRIMARY KEY, call_id BIGINT UNIQUE, project_id BIGINT,
  sample_id BIGINT, agent_id BIGINT, qnr_id BIGINT, qnr_version VARCHAR(255),
  status VARCHAR(255) DEFAULT 'DOING', audit_remark VARCHAR(255)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE ans_answer(id BIGINT PRIMARY KEY, sheet_id BIGINT, question_id BIGINT,
  option_ids VARCHAR(255), answer_text VARCHAR(255), numeric_value DOUBLE, answered_at VARCHAR(255),
  UNIQUE(sheet_id, question_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE wko_ticket(id BIGINT PRIMARY KEY, project_id BIGINT, call_id BIGINT,
  caller_no VARCHAR(255), subject VARCHAR(255), detail VARCHAR(255), status VARCHAR(255) DEFAULT 'PENDING',
  assigned_agent_id BIGINT, priority VARCHAR(255) DEFAULT 'NORMAL', remark VARCHAR(255),
  created_at VARCHAR(255), accepted_at VARCHAR(255), resolved_at VARCHAR(255), closed_at VARCHAR(255), revisit_sample_id BIGINT) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE ivr_flow(id BIGINT PRIMARY KEY, name VARCHAR(255), flow_json VARCHAR(255), updated_at VARCHAR(255)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE ivr_call_log(id BIGINT PRIMARY KEY, caller_no VARCHAR(255), start_time VARCHAR(255),
  end_time VARCHAR(255), outcome VARCHAR(255), path_json VARCHAR(255), answers_json VARCHAR(255)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;


-- +goose Down
