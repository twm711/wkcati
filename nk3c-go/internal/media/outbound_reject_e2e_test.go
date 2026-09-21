package media_test

import (
	"context"
	"testing"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	sip "github.com/emiago/sipgo/sip"

	"nk3c/internal/agent"
	"nk3c/internal/ivr"
	"nk3c/internal/media"
	"nk3c/internal/store"
	"nk3c/internal/workorder"
)

func TestOutboundInvite486FinishesBusy(t *testing.T) {
	db, err := store.Open("sqlite", t.TempDir()+"/reject.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(9502,1,'486测试','LEASED')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_outbound_line(id,tenant_id,line_no,host,port,enabled,priority,capacity,active_calls,circuit_state,failure_streak,rate_limit_per_minute,created_at) VALUES(9601,1,'line-486','127.0.0.1',25273,1,1,1,0,'CLOSED',0,30,?)`, store.NowFor(db.Driver)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,sample_id,agent_id,called_no,caller_no,status,begin_time) VALUES(9501,1,9502,0,'13800000999','','DIALING',?)`, store.NowFor(db.Driver)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ua, _ := sipgo.NewUA()
	rejector := diago.NewDiago(ua, diago.WithTransport(diago.Transport{Transport: "udp", BindHost: "127.0.0.1", BindPort: 25273}))
	go func() {
		_ = rejector.Serve(ctx, func(in *diago.DialogServerSession) { _ = in.Respond(sip.StatusBusyHere, "Busy Here", nil) })
	}()
	time.Sleep(300 * time.Millisecond)

	srv := &media.SIPServer{Driver: ivr.New(db, workorder.New(db)), BindHost: "127.0.0.1", BindPort: 25272, DtmfWait: time.Second, RecordDir: t.TempDir()}
	go func() { _ = srv.Start(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for srv.Outbound == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.Outbound == nil {
		t.Fatal("outbound SIP server did not start")
	}
	srv.Outbound.Driver = agent.New(db)
	srv.Outbound.PeerHost, srv.Outbound.PeerPort = "127.0.0.1", 25273
	_, _, dialErr := srv.Outbound.Dial(ctx, 9501)
	if dialErr != nil {
		t.Fatalf("业务应收敛为 BUSY 而非传输错误: %v", dialErr)
	}
	var status, result string
	if err := db.QueryRow(`SELECT status,result_code FROM cti_call_record WHERE id=?`, 9501).Scan(&status, &result); err != nil {
		t.Fatal(err)
	}
	if status != "CLOSED" || result != "BUSY" {
		t.Fatalf("expected CLOSED/BUSY, got %s/%s", status, result)
	}
	var active int
	if err := db.QueryRow(`SELECT active_calls FROM cti_outbound_line WHERE id=?`, 9601).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("expected line active_calls=0, got %d", active)
	}
}

func TestOutboundInvite480FinishesNA(t *testing.T) {
	db, err := store.Open("sqlite", t.TempDir()+"/reject.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(9502,1,'486测试','LEASED')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,sample_id,agent_id,called_no,caller_no,status,begin_time) VALUES(9501,1,9502,0,'13800000999','','DIALING',?)`, store.NowFor(db.Driver)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ua, _ := sipgo.NewUA()
	rejector := diago.NewDiago(ua, diago.WithTransport(diago.Transport{Transport: "udp", BindHost: "127.0.0.1", BindPort: 25275}))
	go func() {
		_ = rejector.Serve(ctx, func(in *diago.DialogServerSession) {
			_ = in.Respond(sip.StatusTemporarilyUnavailable, "Temporarily Unavailable", nil)
		})
	}()
	time.Sleep(300 * time.Millisecond)

	srv := &media.SIPServer{Driver: ivr.New(db, workorder.New(db)), BindHost: "127.0.0.1", BindPort: 25274, DtmfWait: time.Second, RecordDir: t.TempDir()}
	go func() { _ = srv.Start(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for srv.Outbound == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.Outbound == nil {
		t.Fatal("outbound SIP server did not start")
	}
	srv.Outbound.Driver = agent.New(db)
	srv.Outbound.PeerHost, srv.Outbound.PeerPort = "127.0.0.1", 25275
	_, _, dialErr := srv.Outbound.Dial(ctx, 9501)
	if dialErr != nil {
		t.Fatalf("业务应收敛为 BUSY 而非传输错误: %v", dialErr)
	}
	var status, result string
	if err := db.QueryRow(`SELECT status,result_code FROM cti_call_record WHERE id=?`, 9501).Scan(&status, &result); err != nil {
		t.Fatal(err)
	}
	if status != "CLOSED" || result != "NA" {
		t.Fatalf("expected CLOSED/NA, got %s/%s", status, result)
	}
}

func TestOutboundInvite408FinishesNA(t *testing.T) {
	db, err := store.Open("sqlite", t.TempDir()+"/reject.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(9502,1,'486测试','LEASED')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,sample_id,agent_id,called_no,caller_no,status,begin_time) VALUES(9501,1,9502,0,'13800000999','','DIALING',?)`, store.NowFor(db.Driver)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ua, _ := sipgo.NewUA()
	rejector := diago.NewDiago(ua, diago.WithTransport(diago.Transport{Transport: "udp", BindHost: "127.0.0.1", BindPort: 25277}))
	go func() {
		_ = rejector.Serve(ctx, func(in *diago.DialogServerSession) {
			_ = in.Respond(sip.StatusRequestTimeout, "Request Timeout", nil)
		})
	}()
	time.Sleep(300 * time.Millisecond)

	srv := &media.SIPServer{Driver: ivr.New(db, workorder.New(db)), BindHost: "127.0.0.1", BindPort: 25276, DtmfWait: time.Second, RecordDir: t.TempDir()}
	go func() { _ = srv.Start(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for srv.Outbound == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.Outbound == nil {
		t.Fatal("outbound SIP server did not start")
	}
	srv.Outbound.Driver = agent.New(db)
	srv.Outbound.PeerHost, srv.Outbound.PeerPort = "127.0.0.1", 25277
	_, _, dialErr := srv.Outbound.Dial(ctx, 9501)
	if dialErr != nil {
		t.Fatalf("业务应收敛为 BUSY 而非传输错误: %v", dialErr)
	}
	var status, result string
	if err := db.QueryRow(`SELECT status,result_code FROM cti_call_record WHERE id=?`, 9501).Scan(&status, &result); err != nil {
		t.Fatal(err)
	}
	if status != "CLOSED" || result != "NA" {
		t.Fatalf("expected CLOSED/NA, got %s/%s", status, result)
	}
}

func TestOutboundInvite404FinishesInvalid(t *testing.T) {
	db, err := store.Open("sqlite", t.TempDir()+"/reject.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(9502,1,'486测试','LEASED')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,sample_id,agent_id,called_no,caller_no,status,begin_time) VALUES(9501,1,9502,0,'13800000999','','DIALING',?)`, store.NowFor(db.Driver)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ua, _ := sipgo.NewUA()
	rejector := diago.NewDiago(ua, diago.WithTransport(diago.Transport{Transport: "udp", BindHost: "127.0.0.1", BindPort: 25279}))
	go func() {
		_ = rejector.Serve(ctx, func(in *diago.DialogServerSession) {
			_ = in.Respond(sip.StatusNotFound, "Not Found", nil)
		})
	}()
	time.Sleep(300 * time.Millisecond)

	srv := &media.SIPServer{Driver: ivr.New(db, workorder.New(db)), BindHost: "127.0.0.1", BindPort: 25278, DtmfWait: time.Second, RecordDir: t.TempDir()}
	go func() { _ = srv.Start(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for srv.Outbound == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.Outbound == nil {
		t.Fatal("outbound SIP server did not start")
	}
	srv.Outbound.Driver = agent.New(db)
	srv.Outbound.PeerHost, srv.Outbound.PeerPort = "127.0.0.1", 25279
	_, _, dialErr := srv.Outbound.Dial(ctx, 9501)
	if dialErr != nil {
		t.Fatalf("业务应收敛为 BUSY 而非传输错误: %v", dialErr)
	}
	var status, result string
	if err := db.QueryRow(`SELECT status,result_code FROM cti_call_record WHERE id=?`, 9501).Scan(&status, &result); err != nil {
		t.Fatal(err)
	}
	if status != "CLOSED" || result != "INVALID" {
		t.Fatalf("expected CLOSED/INVALID, got %s/%s", status, result)
	}
}

func TestOutboundInvite603FinishesRefuse(t *testing.T) {
	db, err := store.Open("sqlite", t.TempDir()+"/reject.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO smp_sample(id,project_id,cust_name,status) VALUES(9502,1,'486测试','LEASED')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cti_call_record(id,project_id,sample_id,agent_id,called_no,caller_no,status,begin_time) VALUES(9501,1,9502,0,'13800000999','','DIALING',?)`, store.NowFor(db.Driver)); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ua, _ := sipgo.NewUA()
	rejector := diago.NewDiago(ua, diago.WithTransport(diago.Transport{Transport: "udp", BindHost: "127.0.0.1", BindPort: 25281}))
	go func() {
		_ = rejector.Serve(ctx, func(in *diago.DialogServerSession) {
			_ = in.Respond(sip.StatusGlobalDecline, "Decline", nil)
		})
	}()
	time.Sleep(300 * time.Millisecond)

	srv := &media.SIPServer{Driver: ivr.New(db, workorder.New(db)), BindHost: "127.0.0.1", BindPort: 25280, DtmfWait: time.Second, RecordDir: t.TempDir()}
	go func() { _ = srv.Start(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for srv.Outbound == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.Outbound == nil {
		t.Fatal("outbound SIP server did not start")
	}
	srv.Outbound.Driver = agent.New(db)
	srv.Outbound.PeerHost, srv.Outbound.PeerPort = "127.0.0.1", 25281
	_, _, dialErr := srv.Outbound.Dial(ctx, 9501)
	if dialErr != nil {
		t.Fatalf("业务应收敛为 BUSY 而非传输错误: %v", dialErr)
	}
	var status, result string
	if err := db.QueryRow(`SELECT status,result_code FROM cti_call_record WHERE id=?`, 9501).Scan(&status, &result); err != nil {
		t.Fatal(err)
	}
	if status != "CLOSED" || result != "REFUSE" {
		t.Fatalf("expected CLOSED/REFUSE, got %s/%s", status, result)
	}
}
