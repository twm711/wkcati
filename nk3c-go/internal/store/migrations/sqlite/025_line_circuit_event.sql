-- +goose Up
CREATE TABLE IF NOT EXISTS cti_line_circuit_event(
  id INTEGER PRIMARY KEY,
  line_id INTEGER NOT NULL,
  call_id INTEGER,
  from_state TEXT NOT NULL,
  to_state TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_line_circuit_event_line ON cti_line_circuit_event(line_id,created_at);

-- +goose Down
DROP TABLE cti_line_circuit_event;
