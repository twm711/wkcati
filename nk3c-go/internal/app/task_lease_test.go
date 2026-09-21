package app_test

import (
	"testing"

	"nk3c/internal/agent"
)

func TestTaskLeaseRenewAndReap(t *testing.T) {
	ts, db, agentToken := setup(t)
	defer ts.Close()
	if _, err := db.Exec(`UPDATE smp_sample SET status='ASSIGNED',owner_agent_id=2 WHERE id=101`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE cti_call_record SET status='DIALING',result_code=NULL,end_time=NULL WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_sample_task(id,project_id,sample_id,call_id,queue_id,assigned_user_id,status,leased_at,lease_until)
		VALUES(9001,1,101,1,NULL,2,'LEASED','2026-09-21T00:00:00+00:00','2999-01-01T00:00:00+00:00')`); err != nil {
		t.Fatal(err)
	}

	renew := tenantPost(t, ts.URL, "/api/agent/task/renew", agentToken, map[string]interface{}{"callId": 1})
	if renew["success"] != true {
		t.Fatalf("续租失败: %v", renew)
	}

	if _, err := db.Exec(`UPDATE cti_sample_task SET lease_until='2000-01-01T00:00:00+00:00' WHERE id=9001`); err != nil {
		t.Fatal(err)
	}
	renew = tenantPost(t, ts.URL, "/api/agent/task/renew", agentToken, map[string]interface{}{"callId": 1})
	if renew["success"] == true {
		t.Fatalf("过期任务不应续租: %v", renew)
	}

	n, err := agent.New(db).ReapExpiredTasks()
	if err != nil || n != 1 {
		t.Fatalf("回收数量错误 n=%d err=%v", n, err)
	}
	var taskStatus, sampleStatus, callStatus, result string
	if err := db.QueryRow(`SELECT status FROM cti_sample_task WHERE id=9001`).Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM smp_sample WHERE id=101`).Scan(&sampleStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status,COALESCE(result_code,'') FROM cti_call_record WHERE id=1`).Scan(&callStatus, &result); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "EXPIRED" || sampleStatus != "IDLE" || callStatus != "CLOSED" || result != "NA" {
		t.Fatalf("回收状态错误 task=%s sample=%s call=%s result=%s", taskStatus, sampleStatus, callStatus, result)
	}
}
