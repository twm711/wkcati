package app_test

import (
	"fmt"
	"testing"

	"nk3c/internal/agent"
)

func TestClaimSkipsFullProjectForAvailableProject(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()
	// 两个运行中的 PROGRESSIVE 项目：项目 4 已满载，项目 4 有可领取样本。
	for _, p := range []int64{3, 4} {
		uid := p + 20
		if p == 4 {
			uid = 23
		}
		if _, err := db.Exec(`INSERT INTO prj_project(id,project_code,project_name,status,tenant_id,group_id) VALUES(?,?,?,?,?,?)`, p, "MP", "multi", "RUNNING", 1, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO cti_dial_strategy(project_id,mode,max_concurrent,abandon_target,enabled,updated_at,current_multiplier,last_sample_count) VALUES(?,?,1,3,1,?,?,?)`, p, "PREDICTIVE", "2026-09-21T00:00:00Z", 1, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO prj_queue(project_id,queue_id) VALUES(?,?)`, p, p); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO cti_queue(id,tenant_id,org_id,group_id,name,status) VALUES(?,?,1,1,?,1)`, p, 1, fmt.Sprintf("multi-%d", p)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO sys_user(id,login_name,user_name,agent_no,roles,status,tenant_id,org_id,group_id) VALUES(?,?,?,?,1,1,1,1,1)`, uid, fmt.Sprintf("mp-%d", p), fmt.Sprintf("mp-%d", p), fmt.Sprintf("MP%d", p)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO cti_agent_state(user_id,state,updated_at) VALUES(?,?,?)`, uid, "READY", "2026-09-21T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO cti_agent_queue(user_id,queue_id,enabled,capacity) VALUES(?,?,1,1)`, uid, p); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(?,?,?,?)`, p+30, p, "multi", "IDLE"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO smp_phone(id,sample_id,phone_no,valid_flag) VALUES(?,?,?,1)`, p+30, p+30, "13800000000"); err != nil {
			t.Fatal(err)
		}
		for i := int64(0); i < 20; i++ {
			result := "SUCCESS"
			if p == 3 {
				result = "BREAKOFF"
			}
			if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,status,result_code,begin_time,end_time) VALUES(?,?,?,?,?,?)`, 3000+p*100+i, p, "CLOSED", result, "2099-01-01T00:00:00Z", "2099-01-01T00:01:00Z"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Exec(`INSERT INTO cti_sample_task(id,project_id,sample_id,queue_id,assigned_user_id,status,leased_at,lease_until) VALUES(?,?,?,?,?,?,?,?)`, 2001, 3, 33, 3, 23, "LEASED", "2026-09-21T00:00:00Z", "2099-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	callID, ok, err := agent.New(db).ClaimProgressiveTask()
	if err != nil {
		t.Fatal(err)
	}
	if ok || callID != 0 {
		t.Fatalf("expected shared READY agent capacity to block second project, call=%d ok=%v", callID, ok)
	}
	var p3Multiplier, p4Multiplier float64
	if err := db.QueryRow(`SELECT current_multiplier FROM cti_dial_strategy WHERE project_id=?`, 3).Scan(&p3Multiplier); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT current_multiplier FROM cti_dial_strategy WHERE project_id=?`, 4).Scan(&p4Multiplier); err != nil {
		t.Fatal(err)
	}
	if p3Multiplier != 1 || p4Multiplier != 1 {
		t.Fatalf("claim must not mutate either project multiplier: %v/%v", p3Multiplier, p4Multiplier)
	}
}
