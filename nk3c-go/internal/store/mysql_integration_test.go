package store

import (
	"os"
	"testing"
)

// TestMySQLMigrationIntegration 在提供 NK3C_MYSQL_DSN 时执行真实 MySQL 迁移；默认跳过，避免开发 SQLite 测试依赖外部服务。
func TestMySQLMigrationIntegration(t *testing.T) {
	dsn := os.Getenv("NK3C_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NK3C_MYSQL_DSN 未设置，跳过真实 MySQL 集成测试")
	}
	db, err := Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(false); err != nil {
		t.Fatal(err)
	}
	checks := []string{
		`SELECT rate_limit_per_minute,rate_window_start,rate_window_count FROM cti_outbound_line LIMIT 1`,
		`SELECT current_multiplier,last_sample_count FROM cti_dial_strategy LIMIT 1`,
		`SELECT id,old_rate,new_rate FROM cti_line_rate_audit LIMIT 1`,
	}
	for _, query := range checks {
		if _, err := db.Query(query); err != nil {
			t.Fatalf("MySQL schema check failed for %q: %v", query, err)
		}
	}
}
