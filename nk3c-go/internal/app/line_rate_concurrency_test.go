package app_test

import (
	"sync"
	"testing"

	"nk3c/internal/agent"
)

func TestLineRateBucketAndCapacityAreAtomic(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()
	if _, err := db.Exec(`INSERT INTO cti_outbound_line(id,tenant_id,line_no,host,port,enabled,priority,capacity,active_calls,circuit_state,failure_streak,rate_limit_per_minute,created_at) VALUES(9101,1,'rate-1','127.0.0.1',5060,1,1,2,0,'CLOSED',0,2,'2026-09-21T00:00:00+00:00')`); err != nil {
		t.Fatal(err)
	}
	for i := int64(0); i < 8; i++ {
		if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,caller_no,status,begin_time) VALUES(?,?,?,?,?)`, 9100+i, 1, "", "DIALING", "2026-09-21T00:00:00+00:00"); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := int64(0); i < 8; i++ {
		wg.Add(1)
		go func(callID int64) {
			defer wg.Done()
			if _, err := agent.New(db).ReserveOutboundLine(callID); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(9100 + i)
	}
	wg.Wait()
	if successes != 2 {
		t.Fatalf("expected capacity/rate capped at 2, got %d", successes)
	}
}
