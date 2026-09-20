// Package store 数据访问层：双方言（SQLite dev / MySQL prod）、迁移、事务助手
package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS

type DB struct {
	*sql.DB
	Driver string
	Dsn    string
}

// sqliteDriver 逻辑名→物理驱动名（CGO mattn；如需 CGO-free 可换 modernc，编译需 ≥4GB 内存）
const sqliteDriverName = "sqlite3"

func sqliteDriver(driver string) string {
	if driver == "sqlite" {
		return sqliteDriverName
	}
	return driver
}

func Open(driver, dsn string) (*DB, error) {
	db, err := sql.Open(sqliteDriver(driver), dsn)
	if err != nil {
		return nil, err
	}
	if driver == "sqlite" {
		db.SetMaxOpenConns(1) // SQLite 单写者：串行化事务（生产 MySQL 放开 + SKIP LOCKED）
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &DB{DB: db, Driver: driver, Dsn: dsn}, nil
}

// Migrate 执行嵌入式迁移（goose 兼容格式：-- +goose Up / Down）
func (d *DB) Migrate(force bool) error {
	if force && d.Driver == "sqlite" {
		d.Close()
	}
	if force && d.Driver == "sqlite" {
		// 从 dsn 提取文件名删除重建（演示重置用）
		f := d.Dsn
		if i := strings.Index(f, "?"); i > 0 {
			f = f[:i]
		}
		f = strings.TrimPrefix(strings.TrimPrefix(f, "file://"), "file:")
		_ = os.Remove(f)
		_ = os.Remove(f + "-wal")
		_ = os.Remove(f + "-shm")
		var err error
		db, e := sql.Open(sqliteDriverName, d.Dsn)
		if e != nil {
			return e
		}
		db.SetMaxOpenConns(1)
		d.DB = db
		if err = d.Ping(); err != nil {
			return err
		}
	}
	if _, err := d.Exec("CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT)"); err != nil {
		return err
	}
	// 顺序应用未执行的迁移版本（goose 语义子集：文件名=版本号，单文件单事务语义由外层保证）
	entries, err := sqliteMigrations.ReadDir("migrations/sqlite")
	if err != nil {
		return err
	}
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		ver := strings.TrimSuffix(name, ".sql")
		if i := strings.IndexByte(ver, '_'); i >= 0 {
			ver = ver[:i]
		}
		var done string
		_ = d.QueryRow(`SELECT version FROM schema_migrations WHERE version=?`, ver).Scan(&done)
		if done == ver {
			continue
		}
		data, err := sqliteMigrations.ReadFile("migrations/sqlite/" + name)
		if err != nil {
			return err
		}
		if ver == "003" {
			// 003 曾有部分字段随 001 初始表发布；升级旧库时允许重复列，
			// 但其余错误仍必须中止，保证迁移不会静默损坏 schema。
			clean := make([]string, 0)
			for _, line := range strings.Split(string(data), "\n") {
				if !strings.HasPrefix(strings.TrimSpace(line), "--") {
					clean = append(clean, line)
				}
			}
			for _, stmt := range strings.Split(strings.Join(clean, "\n"), ";") {
				stmt = strings.TrimSpace(stmt)
				if stmt == "" {
					continue
				}
				if _, err := d.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
					return fmt.Errorf("迁移 %s 失败: %w", ver, err)
				}
			}
		} else if _, err := d.Exec(string(data)); err != nil {
			return fmt.Errorf("迁移 %s 失败: %w", ver, err)
		}
		if _, err := d.Exec(`INSERT INTO schema_migrations VALUES(?,?)`, ver, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

// ErrAbort 业务哨兵：fn 已自行写响应，事务应提交而非回滚
var ErrAbort = errors.New("abort")

// Tx 事务助手（派样/配额/审核等关键路径使用）
func (d *DB) Tx(fn func(tx *sql.Tx) error) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		if errors.Is(err, ErrAbort) {
			return tx.Commit() // 业务分支已应答，保留写入
		}
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func NowISO() string { return time.Now().UTC().Format("2006-01-02T15:04:05+00:00") }
