package app_test

import "testing"

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
