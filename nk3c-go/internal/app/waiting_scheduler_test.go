package app_test

import (
	"testing"

	"nk3c/internal/agent"
)

func TestProcessWaitingTasksAssignsWhenCapacityFrees(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()
	if _, err := db.Exec(`INSERT INTO cti_queue(id,tenant_id,org_id,group_id,name,priority,status) VALUES(9901,1,1,1,'测试等待队列',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO prj_queue(project_id,queue_id,priority) VALUES(1,9901,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_agent_queue(user_id,queue_id,enabled,capacity) VALUES(2,9901,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE smp_sample SET status='IDLE',owner_agent_id=NULL,next_attempt_at=NULL WHERE id=101`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_waiting_task(id,tenant_id,project_id,queue_id,agent_id,priority,status,created_at) VALUES(9901,1,1,9901,2,1,'WAITING','2026-09-21T00:00:00+00:00')`); err != nil {
		t.Fatal(err)
	}
	n, err := agent.New(db).ProcessWaitingTasks()
	if err != nil || n != 1 {
		t.Fatalf("assigned=%d err=%v", n, err)
	}
	var waitStatus, sampleStatus, taskStatus string
	_ = db.QueryRow(`SELECT status FROM cti_waiting_task WHERE id=9901`).Scan(&waitStatus)
	_ = db.QueryRow(`SELECT status FROM smp_sample WHERE id=101`).Scan(&sampleStatus)
	_ = db.QueryRow(`SELECT status FROM cti_sample_task WHERE sample_id=101 ORDER BY id DESC LIMIT 1`).Scan(&taskStatus)
	if waitStatus != "ASSIGNED" || sampleStatus != "ASSIGNED" || taskStatus != "LEASED" {
		t.Fatalf("wait=%s sample=%s task=%s", waitStatus, sampleStatus, taskStatus)
	}
}
