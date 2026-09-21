package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func tenantGET(t *testing.T, base, path, token string) map[string]interface{} {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
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

func TestTenantScopeRejectsCrossTenantProject(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()

	// Build a second tenant without changing the demo tenant's seed rows.
	_, err := db.Exec(`INSERT INTO sys_user(id,login_name,password,user_name,agent_no,roles,status,tenant_id)
		VALUES(20,'agent_t2','123456','租户2坐席','2020','phoneAdmin',1,2)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO qnr_questionnaire(id,title,version,status) VALUES(20,'租户2问卷','v1.0','PUBLISHED')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO prj_project(id,project_code,project_name,status,questionnaire_id,tenant_id)
		VALUES(20,'T2-001','租户2项目','RUNNING',20,2)`)
	if err != nil {
		t.Fatal(err)
	}

	t2 := login(t, ts.URL, "agent_t2", "123456")
	projects := tenantGET(t, ts.URL, "/api/project", t2)
	if projects["success"] != true {
		t.Fatalf("租户2项目查询失败: %v", projects)
	}
	rows := projects["data"].([]interface{})
	for _, raw := range rows {
		row := raw.(map[string]interface{})
		if row["projectId"].(float64) == 1 {
			t.Fatalf("租户2看到了租户1项目: %v", row)
		}
	}

	cross := tenantGET(t, ts.URL, "/api/export/1/csv", t2)
	if cross["success"] == true {
		t.Fatalf("租户2不应导出租户1项目: %v", cross)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/agent/dispatch?projectId=1", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+t2)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var dispatch map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&dispatch); err != nil {
		t.Fatal(err)
	}
	if dispatch["success"] == true {
		t.Fatalf("租户2不应派发租户1项目: %v", dispatch)
	}

	admin := login(t, ts.URL, "admin", "123456")
	all := tenantGET(t, ts.URL, "/api/project", admin)
	if all["success"] != true {
		t.Fatalf("domainAdmin 跨租户项目查询失败: %v", all)
	}
	foundTenant2 := false
	for _, raw := range all["data"].([]interface{}) {
		if raw.(map[string]interface{})["projectId"].(float64) == 20 {
			foundTenant2 = true
		}
	}
	if !foundTenant2 {
		t.Fatal("domainAdmin 未看到租户2项目")
	}
}
