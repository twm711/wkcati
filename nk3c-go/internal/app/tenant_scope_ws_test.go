package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTenantScopeWebSockets(t *testing.T) {
	ts, db, tenant1Agent := setup(t)
	defer ts.Close()
	if _, err := db.Exec(`INSERT INTO sys_user(id,login_name,password,user_name,agent_no,roles,status,tenant_id)
		VALUES(22,'sup_t2','123456','租户2督导',NULL,'groupAdmin',1,2)`); err != nil {
		t.Fatal(err)
	}
	tenant2Sup := login(t, ts.URL, "sup_t2", "123456")

	// Tenant 2 wall must not contain tenant-1's seeded agent.
	wallURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws/monitor?token=" + tenant2Sup
	wall, _, err := websocket.DefaultDialer.Dial(wallURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer wall.Close()
	_ = wall.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := wall.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Data struct {
			Agents []interface{} `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(msg, &frame); err != nil {
		t.Fatal(err)
	}
	if len(frame.Data.Agents) != 0 {
		t.Fatalf("租户2监控墙泄露了坐席: %s", msg)
	}

	// Tenant 2 QC subscriber must not receive tenant-1 DIAL events.
	qc, err := qcDial(ts.URL, tenant2Sup)
	if err != nil {
		t.Fatal(err)
	}
	defer qc.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/agent/dispatch?projectId=1", nil)
	req.Header.Set("Authorization", "Bearer "+tenant1Agent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	_ = qc.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, _, err := qc.ReadMessage(); err == nil {
		t.Fatal("租户2 QC WS 收到了租户1事件")
	}
}
