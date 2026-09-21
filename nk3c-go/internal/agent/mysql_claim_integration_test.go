package agent

import (
	"os"
	"sync"
	"testing"
	"time"

	"nk3c/internal/store"
)

func TestMySQLClaimProgressiveTaskConcurrent(t *testing.T) {
	dsn := os.Getenv("NK3C_MYSQL_DSN")
	if dsn == "" {
		t.Skip("NK3C_MYSQL_DSN 未设置，跳过真实 MySQL Claim 集成测试")
	}
	db, err := store.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(false); err != nil {
		t.Fatal(err)
	}
	const pid, uid, qid, sid = 998020, 998021, 998022, 998023
	for _, q := range []string{`DELETE FROM cti_sample_task WHERE project_id=?`, `DELETE FROM cti_call_record WHERE project_id=?`, `DELETE FROM smp_phone WHERE sample_id=?`, `DELETE FROM smp_sample WHERE id=?`, `DELETE FROM cti_dial_strategy WHERE project_id=?`, `DELETE FROM prj_queue WHERE project_id=?`, `DELETE FROM cti_agent_queue WHERE user_id=?`, `DELETE FROM cti_agent_state WHERE user_id=?`, `DELETE FROM prj_project WHERE id=?`, `DELETE FROM cti_queue WHERE id=?`, `DELETE FROM sys_user WHERE id=?`} {
		_, _ = db.Exec(q, pid)
		_, _ = db.Exec(q, uid)
	}
	if _, err := db.Exec(`INSERT INTO sys_user(id,login_name,user_name,agent_no,roles,status,tenant_id,org_id,group_id) VALUES(?,?,?,?,?,?,?,?,?)`, uid, "mysql-claim", "mysql-claim", "MC1", "AGENT", 1, 1, 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO prj_project(id,project_code,project_name,status,tenant_id,group_id) VALUES(?,?,?,?,?,?)`, pid, "MC", "mysql claim", "RUNNING", 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_dial_strategy(project_id,mode,max_concurrent,abandon_target,enabled,updated_at,current_multiplier,last_sample_count) VALUES(?,?,?, ?,?,?,?,?)`, pid, "PREDICTIVE", 2, 3, 1, time.Now(), 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_queue(id,tenant_id,org_id,group_id,name,status) VALUES(?,?,?,?,?,1)`, qid, 1, 1, 1, "mysql-claim"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO prj_queue(project_id,queue_id) VALUES(?,?)`, pid, qid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_agent_state(user_id,state,updated_at) VALUES(?,?,?)`, uid, "READY", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_agent_queue(user_id,queue_id,enabled,capacity) VALUES(?,?,1,1)`, uid, qid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(?,?,?,?)`, sid, pid, "claim", "IDLE"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO smp_phone(id,sample_id,phone_no,valid_flag) VALUES(?,?,?,1)`, sid, sid, "13800000001"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DELETE FROM prj_project WHERE id=?`, pid)
	var wg sync.WaitGroup
	successes := 0
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok, e := New(db).ClaimProgressiveTask(); e == nil && ok {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("expected one production claim, got %d", successes)
	}
	var multiplier float64
	var samples int
	if err := db.QueryRow(`SELECT current_multiplier,last_sample_count FROM cti_dial_strategy WHERE project_id=?`, pid).Scan(&multiplier, &samples); err != nil {
		t.Fatal(err)
	}
	if multiplier != 1 || samples != 0 {
		t.Fatalf("expected predictive state 1/0 under minimum samples, got %v/%d", multiplier, samples)
	}
}
