package app_test

import (
	"sync"
	"testing"

	"nk3c/internal/agent"
)

func TestHalfOpenLineAllowsSingleProbe(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()
	if _, err := db.Exec(`INSERT INTO cti_outbound_line(id,tenant_id,line_no,host,port,enabled,priority,capacity,active_calls,circuit_state,failure_streak,opened_until,created_at) VALUES(9001,1,'probe-1','127.0.0.1',5060,1,1,2,0,'OPEN',5,'2000-01-01T00:00:00+00:00','2026-09-21T00:00:00+00:00')`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	var held []agent.OutboundLine
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			line, err := agent.New(db).ReserveOutboundLine(1)
			if err == nil {
				mu.Lock()
				successes++
				held = append(held, line)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("expected one half-open probe, got %d", successes)
	}
	for _, line := range held {
		_ = agent.New(db).ReleaseOutboundLine(line.ID, 1)
	}
}
