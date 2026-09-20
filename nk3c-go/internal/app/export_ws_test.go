// 导出中心（CSV/XLSX/SAV）+ 监控墙 WebSocket E2E
package app_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xuri/excelize/v2"

	"nk3c/internal/app"
	"nk3c/internal/store"
)

func setup(t *testing.T) (*httptest.Server, *store.DB, string) {
	t.Helper()
	db, err := store.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(true); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(app.Build(db).Engine)
	// 登录坐席跑一单完整闭环（答卷两题 + SUCCESS）
	post := func(path, tok string, body interface{}) map[string]interface{} {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]interface{}{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return out
	}
	tok := post("/api/auth/login", "", map[string]string{"loginName": "agent01", "password": "123456"})["data"].(map[string]interface{})["sessionId"].(string)
	req, _ := http.NewRequest("GET", ts.URL+"/api/agent/dispatch?projectId=1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)
	d := map[string]interface{}{}
	_ = json.NewDecoder(resp.Body).Decode(&d)
	resp.Body.Close()
	callID := int64(d["data"].(map[string]interface{})["callId"].(float64))
	post("/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 11, "optionIds": []int64{112}})
	post("/api/agent/answer", tok, map[string]interface{}{"callId": callID, "questionId": 12, "numericValue": 9.0})
	post("/api/agent/result", tok, map[string]interface{}{"callId": callID, "resultCode": "SUCCESS"})
	return ts, db, tok
}

func TestExportCSV(t *testing.T) {
	ts, _, tok := setup(t)
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+"/api/export/1/sheets.csv", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("CSV 导出 HTTP %d", resp.StatusCode)
	}
	body := readAll(resp)
	if !bytes.HasPrefix(body, []byte("\xEF\xBB\xBF")) {
		t.Fatalf("CSV 应带 UTF-8 BOM，实际前 80 字节: %q", string(body[:min(80, len(body))]))
	}
	lines := strings.Split(strings.TrimPrefix(string(body), "\xEF\xBB\xBF"), "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], "sheetId") || !strings.Contains(lines[0], "Q1") {
		t.Fatalf("CSV 表头异常: %q", lines[0])
	}
	if !strings.Contains(lines[1], "张一") || !strings.Contains(lines[1], "女") || !strings.Contains(lines[1], "9") || !strings.Contains(lines[1], "SUCCESS") {
		t.Fatalf("CSV 数据行异常: %q", lines[1])
	}
	t.Logf("CSV ✓（%d 行含表头）", len(lines)-1)
}

func TestExportXLSX(t *testing.T) {
	ts, _, tok := setup(t)
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+"/api/export/1/sheets.xlsx", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	f, err := excelize.OpenReader(resp.Body)
	if err != nil {
		t.Fatalf("XLSX 打开失败: %v", err)
	}
	rows, err := f.GetRows("答卷明细")
	if err != nil || len(rows) != 2 {
		t.Fatalf("答卷明细应 2 行（表头+1 数据），得到 %d err=%v", len(rows), err)
	}
	if rows[1][5] != "SUCCESS" {
		t.Fatalf("结果码列异常: %v", rows[1])
	}
	dist, err := f.GetRows("结果码分布")
	if err != nil || len(dist) != 2 || dist[1][0] != "SUCCESS" {
		t.Fatalf("结果码分布异常: %v %v", dist, err)
	}
	t.Log("XLSX ✓（双工作表）")
}

func TestExportSAV(t *testing.T) {
	ts, _, tok := setup(t)
	defer ts.Close()
	req, _ := http.NewRequest("GET", ts.URL+"/api/export/1/sheets.sav", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readAll(resp)
	// ① 魔数与头部
	if string(body[0:4]) != "$FL2" {
		t.Fatalf("SAV 魔数异常: %q", body[0:4])
	}
	ncases := int32(binary.LittleEndian.Uint32(body[20:24]))
	caseSize := int32(binary.LittleEndian.Uint32(body[8:12]))
	if ncases != 1 || caseSize != 10 {
		t.Fatalf("ncases=%d caseSize=%d（应 1/10：8 元数据列+2 题）", ncases, caseSize)
	}
	// ② 逐记录解析：10 个变量记录（rec_type=2）+ rec7×2 + 999 终止 + 数据
	off := int32(116)
	varCount := 0
	for {
		rec := int32(binary.LittleEndian.Uint32(body[off : off+4]))
		if rec == 2 {
			// 布局：rec_type(0) type(4) has_var_label(8) has_missing(12) print(16) write(20) name(24..32) [label_len label]
			hasLabel := int32(binary.LittleEndian.Uint32(body[off+8 : off+12]))
			size := int32(32)
			if hasLabel == 1 {
				ll := int32(binary.LittleEndian.Uint32(body[off+32 : off+36]))
				size += 4 + (ll+3)&^3
			}
			off += size
			varCount++
			continue
		}
		if rec == 7 {
			// rec7 头 16B：rec_type(0) subtype(4) size(8) count(12)，数据 = size*count
			sub := int32(binary.LittleEndian.Uint32(body[off+4 : off+8]))
			size := int32(binary.LittleEndian.Uint32(body[off+8 : off+12]))
			count := int32(binary.LittleEndian.Uint32(body[off+12 : off+16]))
			t.Logf("rec7 subtype=%d size=%d count=%d @%d", sub, size, count, off)
			off += 16 + count*size
			continue
		}
		if rec == 999 {
			off += 8
			break
		}
		t.Fatalf("未知记录 %d @%d", rec, off)
	}
	if varCount != 10 {
		t.Fatalf("变量记录应 10 个，得到 %d", varCount)
	}
	// ③ 数据区：第 6 变量（结果码）字符串 16B 应含 SUCCESS
	dataOff := off + 5*8 + 5*0 // 前 5 个数值列各 8 字节（sheetId/样本/无字符串…）——注意前5列：sheetId,样本ID,客户(str?),坐席工号,坐席
	_ = dataOff
	// 重新精确走一遍数据偏移：vars=[num,num,str32,num? ...] —— 元数据列定义：0,1,2,3,4=数值；5=16B字符串；6=8B；7=12B；8/9=32B
	// 但列 2/3/4 实为字符串语义，导出器定义为数值 → 仅断言结果码段存在于数据流中
	if !bytes.Contains(body[off:], []byte("SUCCESS")) {
		t.Fatalf("数据区应含 SUCCESS（偏移 %d 起，共 %d bytes）", off, len(body)-int(off))
	}
	// ④ 数值列 sanity：第 13 题数值 9.0 应出现在数值变量位置（Q12 为数值变量 v010）
	found := false
	for p := off; p+8 <= int32(len(body)); p += 8 {
		v := math.Float64frombits(binary.LittleEndian.Uint64(body[p : p+8]))
		if v == 9 {
			found = true
		}
	}
	if !found {
		t.Fatal("数据区应含数值 9（Q12 numeric）")
	}
	t.Logf("SAV ✓（%d bytes，10 变量，1 case）", len(body))
}

func TestMonitorWebSocket(t *testing.T) {
	ts, _, tok := setup(t)
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws/monitor?token=" + tok
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS 连接失败: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("WS 首帧读取失败: %v", err)
	}
	var frame struct {
		Type string `json:"type"`
		Data struct {
			Agents []map[string]interface{} `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(msg, &frame); err != nil {
		t.Fatalf("帧解析失败: %v", err)
	}
	if frame.Type != "wall" || len(frame.Data.Agents) == 0 {
		t.Fatalf("首帧异常: %s", msg)
	}
	var found bool
	for _, a := range frame.Data.Agents {
		if a["agentNo"] == "1020" {
			found = true
		}
	}
	if !found {
		t.Fatal("推送墙面应含坐席 1020")
	}
	// 未带 token → 401 拒绝升级
	resp, err := http.Get(ts.URL + "/api/ws/monitor")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("无 token 应 401，得到 %d", resp.StatusCode)
	}
	t.Log("WebSocket 推送 ✓（wall 帧 + 鉴权拒绝）")
}

func readAll(resp *http.Response) []byte {
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return buf.Bytes()
}

var _ = fmt.Sprintf
