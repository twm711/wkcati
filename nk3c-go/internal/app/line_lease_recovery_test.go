package app_test

import (
	"testing"

	"nk3c/internal/agent"
)

func TestExpiredLineLeaseReleasesCapacity(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()
	if _, err := db.Exec(`INSERT INTO cti_outbound_line(id,tenant_id,line_no,host,port,enabled,priority,capacity,active_calls,circuit_state,failure_streak,rate_limit_per_minute,created_at) VALUES(9201,1,'lease-1','127.0.0.1',5060,1,1,1,0,'CLOSED',0,30,'2026-09-21T00:00:00+00:00')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,caller_no,status,begin_time) VALUES(9200,1,'','DIALING','2026-09-21T00:00:00+00:00')`); err != nil {
		t.Fatal(err)
	}
	line, err := agent.New(db).ReserveOutboundLine(9200)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE cti_outbound_line_lease SET lease_until='2000-01-01T00:00:00+00:00' WHERE call_id=?`, 9200); err != nil {
		t.Fatal(err)
	}
	released, err := agent.New(db).ReapOutboundLineLeases()
	if err != nil || released != 1 {
		t.Fatalf("expected one recovered lease, released=%d err=%v", released, err)
	}
	var active int
	if err := db.QueryRow(`SELECT active_calls FROM cti_outbound_line WHERE id=?`, line.ID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("expected active_calls=0, got %d", active)
	}
}
