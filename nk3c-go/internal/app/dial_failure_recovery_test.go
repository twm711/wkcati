package app_test

import (
	"context"
	"testing"

	"nk3c/internal/agent"
	"nk3c/internal/media"
)

func TestDialWithoutSIPDomainFinishesClaimedCall(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(9302,1,'故障测试','LEASED')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,sample_id,agent_id,caller_no,status,begin_time) VALUES(9301,1,9302,0,'','DIALING','2026-09-21T00:00:00+00:00')`); err != nil {
		t.Fatal(err)
	}
	driver := agent.New(db)
	_, _, err := (&media.OutboundCaller{Driver: driver}).Dial(context.Background(), 9301)
	if err == nil {
		t.Fatal("expected SIP domain error")
	}
	var status string
	var result interface{}
	if err := db.QueryRow(`SELECT status,result_code FROM cti_call_record WHERE id=?`, 9301).Scan(&status, &result); err != nil {
		t.Fatal(err)
	}
	if status != "CLOSED" {
		t.Fatalf("expected CLOSED, got %s", status)
	}
}
