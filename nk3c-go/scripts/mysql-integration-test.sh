#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${NK3C_MYSQL_DSN:-}" ]]; then
  echo '请先设置 NK3C_MYSQL_DSN，例如 user:password@tcp(127.0.0.1:3306)/nk3c?parseTime=true' >&2
  exit 2
fi

GOMODCACHE="${GOMODCACHE:-/tmp/gomod}" \
GOCACHE="${GOCACHE:-/var/tmp/gocache}" \
GOFLAGS="${GOFLAGS:--mod=mod}" \
NK3C_MYSQL_DSN="$NK3C_MYSQL_DSN" \
  go test ./internal/store -run TestMySQLMigrationIntegration -count=1 -v
