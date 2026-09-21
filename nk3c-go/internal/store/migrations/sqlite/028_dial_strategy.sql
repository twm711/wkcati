-- +goose Up
CREATE TABLE IF NOT EXISTS cti_dial_strategy(
  project_id INTEGER PRIMARY KEY,
  mode TEXT NOT NULL DEFAULT 'PREVIEW',
  max_concurrent INTEGER NOT NULL DEFAULT 1,
  abandon_target REAL NOT NULL DEFAULT 3.0,
  preview_seconds INTEGER NOT NULL DEFAULT 15,
  enabled INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE cti_dial_strategy;
