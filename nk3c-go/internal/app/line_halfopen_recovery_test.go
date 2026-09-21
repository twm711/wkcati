package app_test

import (
	"testing"

	"nk3c/internal/agent"
)

func TestHalfOpenSuccessfulResultClosesCircuit(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()
	if _, err := db.Exec(`INSERT INTO cti_outbound_line(id,tenant_id,line_no,host,port,enabled,priority,capacity,active_calls,circuit_state,failure_streak,opened_until,created_at) VALUES(9701,1,'half-success','127.0.0.1',5060,1,1,1,0,'OPEN',5,'2000-01-01T00:00:00+00:00','2026-09-21T00:00:00+00:00')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(9702,1,'半开成功','LEASED')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,sample_id,agent_id,caller_no,status,begin_time) VALUES(9703,1,9702,0,'half-success','DIALING',?)`, "2026-09-21T00:00:00+00:00"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := agent.New(db).FinishOutbound(9703, "SUCCESS"); err != nil {
		t.Fatal(err)
	}
	var streak int
	var state string
	if err := db.QueryRow(`SELECT failure_streak,circuit_state FROM cti_outbound_line WHERE id=?`, 9701).Scan(&streak, &state); err != nil {
		t.Fatal(err)
	}
	if streak != 0 || state != "CLOSED" {
		t.Fatalf("expected CLOSED/0, got %s/%d", state, streak)
	}
}
