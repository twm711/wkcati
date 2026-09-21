package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func tenantPost(t *testing.T, base, path, token string, body interface{}) map[string]interface{} {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]interface{}{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTenantScopeExtendedResources(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()

	// setup creates one completed tenant-1 call/sheet. Add a tenant-1 ticket
	// and recording index, then access them with a tenant-2 session.
	if _, err := db.Exec(`INSERT INTO wko_ticket(id,project_id,call_id,caller_no,subject,detail,status,priority,created_at)
		VALUES(9001,1,1,'138000009001','租户1工单','detail','PENDING','HIGH','2026-09-21T00:00:00+00:00')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE cti_call_record SET record_file='/tmp/tenant1.wav' WHERE id=(SELECT id FROM cti_call_record WHERE project_id=1 ORDER BY id LIMIT 1)`); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`INSERT INTO sys_user(id,login_name,password,user_name,agent_no,roles,status,tenant_id)
		VALUES(21,'agent_t2b','123456','租户2坐席B','2021','phoneAdmin',1,2)`); err != nil {
		t.Fatal(err)
	}
	t2 := login(t, ts.URL, "agent_t2b", "123456")

	for _, path := range []string{
		"/api/workorder/9001",
		"/api/recording/1",
		"/api/ivr/flow?projectId=1",
		"/api/monitor/calls",
	} {
		out := tenantGET(t, ts.URL, path, t2)
		if out["success"] == true {
			// monitor/calls is a list endpoint, so success is allowed only when
			// it returns no tenant-1 call rows.
			if path == "/api/monitor/calls" {
				data, ok := out["data"].([]interface{})
				if !ok || len(data) != 0 {
					t.Fatalf("租户2看到了租户1话务: %v", out)
				}
			} else {
				t.Fatalf("租户2越权读取 %s: %v", path, out)
			}
		}
	}
}
