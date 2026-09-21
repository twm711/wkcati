package store

import (
	"os"
	"sync"
	"testing"
	"time"
)

func mysqlTestDB(t *testing.T) *DB {
	t.Helper()
	dsn := os.Getenv("NK3C_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NK3C_MYSQL_DSN 未设置，跳过真实 MySQL 集成测试")
	}
	db, err := Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(false); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

// TestMySQLMigrationIntegration 在提供 NK3C_MYSQL_DSN 时执行真实 MySQL 迁移。
func TestMySQLMigrationIntegration(t *testing.T) {
	db := mysqlTestDB(t)
	defer db.Close()
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

// TestMySQLLineRateBucketConcurrentUpdate 验证 MySQL 多连接下分钟桶条件更新不会超过上限。
func TestMySQLLineRateBucketConcurrentUpdate(t *testing.T) {
	db := mysqlTestDB(t)
	defer db.Close()
	const id = 998001
	_, _ = db.Exec(`DELETE FROM cti_outbound_line WHERE id=?`, id)
	if _, err := db.Exec(`INSERT INTO cti_outbound_line(id,tenant_id,line_no,host,port,enabled,priority,capacity,active_calls,circuit_state,failure_streak,rate_limit_per_minute,rate_window_count,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, 1, "mysql-test-rate", "127.0.0.1", 5060, 1, 1, 100, 0, "CLOSED", 0, 2, 0, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM cti_outbound_line WHERE id=?`, id)
	cutoff := TimeFor(db.Driver, time.Now().UTC().Add(-time.Minute))
	now := NowFor(db.Driver)
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := db.Exec(`UPDATE cti_outbound_line SET active_calls=active_calls+1,rate_window_start=?,rate_window_count=CASE WHEN rate_window_start IS NULL OR rate_window_start<? THEN 1 ELSE rate_window_count+1 END WHERE id=? AND active_calls<capacity AND (rate_limit_per_minute<=0 OR rate_window_start IS NULL OR rate_window_start<? OR rate_window_count<rate_limit_per_minute)`, now, cutoff, id, cutoff)
			if err == nil {
				if n, _ := res.RowsAffected(); n == 1 {
					mu.Lock()
					successes++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if successes != 2 {
		t.Fatalf("expected exactly 2 MySQL bucket updates, got %d", successes)
	}
	var count int
	if err := db.QueryRow(`SELECT rate_window_count FROM cti_outbound_line WHERE id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected rate_window_count=2, got %d", count)
	}
}

// TestMySQLHalfOpenProbeConcurrentUpdate 验证多个 MySQL 连接只有一个可以领取 HALF_OPEN 探测。
func TestMySQLLeaseReapDeleteIsIdempotent(t *testing.T) {
	db := mysqlTestDB(t)
	defer db.Close()
	const lineID, callID = 998003, 998004
	_, _ = db.Exec(`DELETE FROM cti_outbound_line_lease WHERE call_id=?`, callID)
	_, _ = db.Exec(`DELETE FROM cti_outbound_line WHERE id=?`, lineID)
	if _, err := db.Exec(`INSERT INTO cti_outbound_line(id,tenant_id,line_no,host,port,enabled,priority,capacity,active_calls,circuit_state,failure_streak,rate_limit_per_minute,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, lineID, 1, "mysql-test-lease", "127.0.0.1", 5060, 1, 1, 10, 1, "CLOSED", 0, 30, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_outbound_line_lease(call_id,line_id,lease_until,created_at) VALUES(?,?,?,?)`, callID, lineID, time.Now().UTC().Add(-time.Minute), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM cti_outbound_line_lease WHERE call_id=?`, callID)
	defer db.Exec(`DELETE FROM cti_outbound_line WHERE id=?`, lineID)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := db.Begin()
			if err != nil {
				return
			}
			res, err := tx.Exec(`DELETE FROM cti_outbound_line_lease WHERE call_id=?`, callID)
			if err == nil {
				if n, _ := res.RowsAffected(); n == 1 {
					_, _ = tx.Exec(`UPDATE cti_outbound_line SET active_calls=CASE WHEN active_calls>0 THEN active_calls-1 ELSE 0 END WHERE id=?`, lineID)
				}
			}
			_ = tx.Commit()
		}()
	}
	wg.Wait()
	var active int
	if err := db.QueryRow(`SELECT active_calls FROM cti_outbound_line WHERE id=?`, lineID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("expected idempotent reap active_calls=0, got %d", active)
	}
}

func TestMySQLLeaseReapAndAcquireRace(t *testing.T) {
	db := mysqlTestDB(t)
	defer db.Close()
	const lineID, oldCall, newCall = 998005, 998006, 998007
	_, _ = db.Exec(`DELETE FROM cti_outbound_line_lease WHERE call_id IN (?,?)`, oldCall, newCall)
	_, _ = db.Exec(`DELETE FROM cti_outbound_line WHERE id=?`, lineID)
	if _, err := db.Exec(`INSERT INTO cti_outbound_line(id,tenant_id,line_no,host,port,enabled,priority,capacity,active_calls,circuit_state,failure_streak,rate_limit_per_minute,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, lineID, 1, "mysql-test-race", "127.0.0.1", 5060, 1, 1, 1, 1, "CLOSED", 0, 30, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_outbound_line_lease(call_id,line_id,lease_until,created_at) VALUES(?,?,?,?)`, oldCall, lineID, time.Now().UTC().Add(-time.Minute), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM cti_outbound_line_lease WHERE call_id IN (?,?)`, oldCall, newCall)
	defer db.Exec(`DELETE FROM cti_outbound_line WHERE id=?`, lineID)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tx, _ := db.Begin()
		res, _ := tx.Exec(`DELETE FROM cti_outbound_line_lease WHERE call_id=?`, oldCall)
		if n, _ := res.RowsAffected(); n == 1 {
			_, _ = tx.Exec(`UPDATE cti_outbound_line SET active_calls=active_calls-1 WHERE id=? AND active_calls>0`, lineID)
		}
		_ = tx.Commit()
	}()
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			res, err := db.Exec(`UPDATE cti_outbound_line SET active_calls=active_calls+1 WHERE id=? AND active_calls<capacity`, lineID)
			if err == nil {
				if n, _ := res.RowsAffected(); n == 1 {
					_, _ = db.Exec(`INSERT INTO cti_outbound_line_lease(call_id,line_id,lease_until,created_at) VALUES(?,?,?,?)`, newCall, lineID, time.Now().UTC().Add(time.Minute), time.Now().UTC())
					return
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	wg.Wait()
	var active int
	if err := db.QueryRow(`SELECT active_calls FROM cti_outbound_line WHERE id=?`, lineID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("expected exactly one active acquired call, got %d", active)
	}
}

// TestMySQLSampleClaimConcurrentUpdate 验证多实例领取同一 IDLE 样本时只有一个原子状态更新成功。
func TestMySQLSampleClaimConcurrentUpdate(t *testing.T) {
	db := mysqlTestDB(t)
	defer db.Close()
	const sampleID = 998008
	_, _ = db.Exec(`DELETE FROM smp_sample WHERE id=?`, sampleID)
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(?,?,?,?)`, sampleID, 1, "mysql-claim", "IDLE"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM smp_sample WHERE id=?`, sampleID)
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := db.Exec(`UPDATE smp_sample SET status='LEASED' WHERE id=? AND status='IDLE'`, sampleID)
			if err == nil {
				if n, _ := res.RowsAffected(); n == 1 {
					mu.Lock()
					successes++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("expected one sample claim, got %d", successes)
	}
}

// TestMySQLClaimTransactionCreatesOneCallAndTask 验证样本、话务、任务租约在同一事务内只产生一组记录。
func TestMySQLClaimTransactionCreatesOneCallAndTask(t *testing.T) {
	db := mysqlTestDB(t)
	defer db.Close()
	const sampleID, callID, taskID = 998009, 998010, 998011
	_, _ = db.Exec(`DELETE FROM cti_sample_task WHERE id=?`, taskID)
	_, _ = db.Exec(`DELETE FROM cti_call_record WHERE id=?`, callID)
	_, _ = db.Exec(`DELETE FROM smp_sample WHERE id=?`, sampleID)
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(?,?,?,?)`, sampleID, 1, "mysql-claim-tx", "IDLE"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM cti_sample_task WHERE id=?`, taskID)
	defer db.Exec(`DELETE FROM cti_call_record WHERE id=?`, callID)
	defer db.Exec(`DELETE FROM smp_sample WHERE id=?`, sampleID)
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := db.Begin()
			if err != nil {
				return
			}
			var status string
			if err = tx.QueryRow(`SELECT status FROM smp_sample WHERE id=? FOR UPDATE`, sampleID).Scan(&status); err == nil && status == "IDLE" {
				_, err = tx.Exec(`UPDATE smp_sample SET status='LEASED' WHERE id=?`, sampleID)
				if err == nil {
					_, err = tx.Exec(`INSERT INTO cti_call_record(id,project_id,sample_id,status,begin_time) VALUES(?,?,?,?,?)`, callID, 1, sampleID, "DIALING", time.Now().UTC())
				}
				if err == nil {
					_, err = tx.Exec(`INSERT INTO cti_sample_task(id,project_id,sample_id,call_id,status,leased_at,lease_until) VALUES(?,?,?,?,?,?,?)`, taskID, 1, sampleID, callID, "LEASED", time.Now().UTC(), time.Now().UTC().Add(time.Minute))
				}
			}
			if err == nil {
				if tx.Commit() == nil {
					mu.Lock()
					successes++
					mu.Unlock()
					return
				}
			}
			_ = tx.Rollback()
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("expected one complete claim transaction, got %d", successes)
	}
	var calls, tasks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cti_call_record WHERE id=?`, callID).Scan(&calls); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM cti_sample_task WHERE id=?`, taskID).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || tasks != 1 {
		t.Fatalf("expected one call/task, got %d/%d", calls, tasks)
	}
}

func TestMySQLHalfOpenProbeConcurrentUpdate(t *testing.T) {
	db := mysqlTestDB(t)
	defer db.Close()
	const id = 998002
	_, _ = db.Exec(`DELETE FROM cti_outbound_line WHERE id=?`, id)
	if _, err := db.Exec(`INSERT INTO cti_outbound_line(id,tenant_id,line_no,host,port,enabled,priority,capacity,active_calls,circuit_state,failure_streak,opened_until,rate_limit_per_minute,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, 1, "mysql-test-half", "127.0.0.1", 5060, 1, 1, 10, 0, "OPEN", 5, time.Now().UTC().Add(-time.Minute), 30, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM cti_outbound_line WHERE id=?`, id)
	now := NowFor(db.Driver)
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := db.Exec(`UPDATE cti_outbound_line SET circuit_state='HALF_OPEN' WHERE id=? AND circuit_state='OPEN' AND opened_until<=?`, id, now)
			if err == nil {
				if n, _ := res.RowsAffected(); n == 1 {
					mu.Lock()
					successes++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("expected exactly one HALF_OPEN probe, got %d", successes)
	}
}
