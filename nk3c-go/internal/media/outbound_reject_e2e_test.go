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
}
