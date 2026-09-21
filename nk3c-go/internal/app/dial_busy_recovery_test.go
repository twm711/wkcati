package app_test

import (
	"testing"

	"nk3c/internal/agent"
)

func TestBusyResultClosesDialingCallAndReturnsSample(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(9402,1,'忙线测试','LEASED')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,sample_id,agent_id,caller_no,status,begin_time) VALUES(9401,1,9402,0,'','DIALING','2026-09-21T00:00:00+00:00')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := agent.New(db).FinishOutbound(9401, "BUSY"); err != nil {
		t.Fatal(err)
	}
	var callStatus, result, sampleStatus string
	if err := db.QueryRow(`SELECT status,result_code FROM cti_call_record WHERE id=?`, 9401).Scan(&callStatus, &result); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM smp_sample WHERE id=?`, 9402).Scan(&sampleStatus); err != nil {
		t.Fatal(err)
	}
	if callStatus != "CLOSED" || result != "BUSY" || sampleStatus != "IDLE" {
		t.Fatalf("unexpected recovery call=%s result=%s sample=%s", callStatus, result, sampleStatus)
	}
}
