-- 002 质检事件流：坐席动作（派样/接通/结果码/强签）实时落库，督导 WS 订阅推送（M3）
CREATE TABLE cti_monitor_event(
  id INTEGER PRIMARY KEY,
  agent_id INTEGER,
  agent_no TEXT,
  event TEXT,
  call_id INTEGER,
  sample_id INTEGER,
  detail TEXT,
  created_at TEXT
);
CREATE INDEX idx_cti_monitor_event_created ON cti_monitor_event(created_at);
