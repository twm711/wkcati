-- +goose Up
CREATE TABLE cti_agent_state(
  user_id INTEGER PRIMARY KEY,
  state TEXT NOT NULL DEFAULT 'READY',
  reason TEXT,
  updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE cti_agent_state;
