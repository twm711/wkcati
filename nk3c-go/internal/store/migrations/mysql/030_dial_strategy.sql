-- +goose Up
CREATE TABLE IF NOT EXISTS cti_dial_strategy(
  project_id BIGINT PRIMARY KEY,
  mode VARCHAR(16) NOT NULL DEFAULT 'PREVIEW',
  max_concurrent INT NOT NULL DEFAULT 1,
  abandon_target DECIMAL(5,2) NOT NULL DEFAULT 3.0,
  preview_seconds INT NOT NULL DEFAULT 15,
  enabled TINYINT NOT NULL DEFAULT 1,
  updated_at DATETIME NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
DROP TABLE cti_dial_strategy;
