package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func tenantPut(t *testing.T, base, path, token string, body interface{}) map[string]interface{} {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPut, base+path, strings.NewReader(string(b)))
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

func TestOrgGroupAdministrationScope(t *testing.T) {
	ts, db, _ := setup(t)
	defer ts.Close()
	admin := login(t, ts.URL, "admin", "123456")
	orgs := tenantGET(t, ts.URL, "/api/orgs", admin)
	if orgs["success"] != true {
		t.Fatalf("机构列表失败: %v", orgs)
	}
	created := tenantPost(t, ts.URL, "/api/orgs", admin, map[string]string{"name": "测试机构"})
	if created["success"] != true {
		t.Fatalf("机构创建失败: %v", created)
	}
	id := int64(created["data"].(map[string]interface{})["id"].(float64))
	group := tenantPost(t, ts.URL, "/api/orgs/"+itoa(id)+"/groups", admin, map[string]string{"name": "测试组"})
	if group["success"] != true {
		t.Fatalf("坐席组创建失败: %v", group)
	}
	if _, err := db.Exec(`SELECT id FROM sys_group WHERE id=?`, group["data"].(map[string]interface{})["id"]); err != nil {
		t.Fatal(err)
	}
	gid := group["data"].(map[string]interface{})["id"].(float64)
	if r := tenantPut(t, ts.URL, "/api/users/2/group", admin, map[string]interface{}{"orgId": id, "groupId": gid}); r["success"] != true {
		t.Fatalf("坐席归组失败: %v", r)
	}
	if r := tenantPut(t, ts.URL, "/api/project/1/group", admin, map[string]interface{}{"groupId": gid}); r["success"] != true {
		t.Fatalf("项目归组失败: %v", r)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	b := make([]byte, 0, 20)
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
