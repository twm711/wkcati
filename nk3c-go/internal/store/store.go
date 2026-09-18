// Package store 数据访问层：双方言（SQLite dev / MySQL prod）、迁移、事务助手
package store

import (
	"database/sql"
	"errors"
	"embed"
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
	var done string
	_ = d.QueryRow("SELECT version FROM schema_migrations WHERE version=?", "001").Scan(&done)
	if done == "001" {
		return nil
	}
	data, err := sqliteMigrations.ReadFile("migrations/sqlite/001_init.sql")
	if err != nil {
		return err
	}
	if _, err := d.Exec(string(data)); err != nil {
		return fmt.Errorf("迁移 001 失败: %w", err)
	}
	_, err = d.Exec("INSERT INTO schema_migrations VALUES('001',?)", time.Now().UTC().Format(time.RFC3339))
	return err
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
