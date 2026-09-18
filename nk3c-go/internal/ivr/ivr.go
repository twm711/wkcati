// Package ivr 呼入流程引擎（flow_json 解释器 + 校验 + 模拟通道；真实音频由话务域 diago 驱动同一引擎）
package ivr

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"nk3c/internal/store"
	"nk3c/internal/workorder"
	"nk3c/pkg/rinfo"
)

type Service struct {
	db *store.DB
	wk *workorder.Service
}

func New(db *store.DB, wk *workorder.Service) *Service { return &Service{db: db, wk: wk} }

type node struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Text     string            `json:"text"`
	Next     string            `json:"next"`
	Branches map[string]string `json:"branches"`
	Options  map[string]string `json:"options"`
	Tag      string            `json:"tag"`
	Queue    string            `json:"queue"`
}
type flow struct {
	Entry string `json:"entry"`
	Nodes []node `json:"nodes"`
}

type session struct {
	CallerNo   string
	Transcript []map[string]interface{}
	Path       []string
	Answers    map[string]string
	Outcome    string
	Start      string
	Done       bool
	Current    string
}

var sessions = map[string]*session{}

func (s *Service) flowNodes() (flow, map[string]node, error) {
	var raw string
	if err := s.db.QueryRow(`SELECT flow_json FROM ivr_flow WHERE id=1`).Scan(&raw); err != nil {
		return flow{}, nil, err
	}
	var f flow
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return flow{}, nil, err
	}
	m := map[string]node{}
	for _, n := range f.Nodes {
		m[n.ID] = n
	}
	return f, m, nil
}

func (s *Service) GetFlow(c *gin.Context) {
	var name, raw, updated string
	if err := s.db.QueryRow(`SELECT name,flow_json,updated_at FROM ivr_flow WHERE id=1`).Scan(&name, &raw, &updated); err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error()); return
	}
	var f flow
	_ = json.Unmarshal([]byte(raw), &f)
	rinfo.GinOK(c, gin.H{"name": name, "flow": f, "updatedAt": updated}, "ok")
}

func validate(f flow) string {
	if len(f.Nodes) == 0 {
		return "nodes 须为非空数组"
	}
	ids := map[string]bool{}
	for _, n := range f.Nodes {
		if n.ID == "" || ids[n.ID] {
			return "节点 id 缺失或重复"
		}
		ids[n.ID] = true
	}
	if !ids[f.Entry] {
		return "entry 必须指向存在的节点"
	}
	types := map[string]bool{"play": true, "menu": true, "question": true, "voicemail": true, "transfer": true, "end": true}
	for _, n := range f.Nodes {
		if !types[n.Type] {
			return fmt.Sprintf("节点 %s：type 非法", n.ID)
		}
		if strings.TrimSpace(n.Text) == "" {
			return fmt.Sprintf("节点 %s：播报文本不能为空", n.ID)
		}
		var targets []string
		if n.Type == "menu" {
			if len(n.Branches) == 0 {
				return fmt.Sprintf("节点 %s：menu 至少一个按键分支", n.ID)
			}
			for _, v := range n.Branches {
				targets = append(targets, v)
			}
		}
		if n.Type == "question" {
			if len(n.Options) == 0 {
				return fmt.Sprintf("节点 %s：question 至少一个按键选项", n.ID)
			}
			if n.Tag == "" {
				return fmt.Sprintf("节点 %s：question 须有 tag", n.ID)
			}
		}
		if n.Type == "play" || n.Type == "question" || n.Type == "voicemail" || n.Type == "transfer" {
			if n.Next == "" {
				return fmt.Sprintf("节点 %s：%s 节点须有 next（menu 的出口即按键分支）", n.ID, n.Type)
			}
			targets = append(targets, n.Next)
		}
		for _, t := range targets {
			if !ids[t] {
				return fmt.Sprintf("节点 %s：跳转目标 %s 不存在", n.ID, t)
			}
		}
	}
	return ""
}

type flowReq struct {
	Name string    `json:"name"`
	Flow flowValidate `json:"flow" binding:"required"`
}
type flowValidate struct {
	Entry string `json:"entry"`
	Nodes []node `json:"nodes"`
}

func (s *Service) PutFlow(c *gin.Context) {
	var req flowReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误"); return
	}
	f := flow{Entry: req.Flow.Entry, Nodes: req.Flow.Nodes}
	if msg := validate(f); msg != "" {
		rinfo.GinFail(c, rinfo.CodeParam, "流程校验失败："+msg); return
	}
	raw, _ := json.Marshal(f)
	_, err := s.db.Exec(`UPDATE ivr_flow SET name=?,flow_json=?,updated_at=? WHERE id=1`,
		req.Name, string(raw), store.NowISO())
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error()); return
	}
	rinfo.GinOK(c, true, "流程已保存并即时生效（下一通呼入按新流程走线）")
}

// enter 进入节点：play/transfer 自动流转；menu/question/voicemail 等按键；end 落库
func (s *Service) enter(nodes map[string]node, cur node, sess *session) node {
	for {
		sess.Transcript = append(sess.Transcript, map[string]interface{}{"node": cur.ID, "type": cur.Type, "text": cur.Text})
		sess.Path = append(sess.Path, cur.ID)
		switch cur.Type {
		case "play":
			cur = nodes[cur.Next]
			continue
		case "transfer":
			sess.Outcome = "TRANSFER:" + orDefault(cur.Queue, "MANUAL")
			sess.Transcript[len(sess.Transcript)-1]["event"] = "转人工队列"
			cur = nodes[cur.Next]
			continue
		case "end":
			sess.Done = true
			s.finalize(sess)
			return cur
		}
		return cur
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func (s *Service) finalize(sess *session) {
	if _, ok := sess.Answers["留言"]; ok && sess.Outcome == "" {
		sess.Outcome = "VOICEMAIL"
	}
	if sess.Outcome == "" {
		if _, ok := sess.Answers["Q11"]; ok {
			sess.Outcome = "SURVEY"
		}
	}
	if sess.Outcome == "" {
		sess.Outcome = "INFO"
	}
	end := store.NowISO()
	path, _ := json.Marshal(sess.Path)
	ans, _ := json.Marshal(sess.Answers)
	_, err := s.db.Exec(`INSERT INTO ivr_call_log(caller_no,start_time,end_time,outcome,path_json,answers_json) VALUES(?,?,?,?,?,?)`,
		sess.CallerNo, sess.Start, end, sess.Outcome, string(path), string(ans))
	if err != nil {
		return
	}
	if strings.HasPrefix(sess.Outcome, "TRANSFER") && s.wk != nil {
		var callID int64
		_ = s.db.QueryRow(`SELECT COALESCE(MAX(id),0) FROM cti_call_record`).Scan(&callID)
		s.wk.CreateFromIVR(callID, sess.CallerNo, strings.Join(sess.Path, " → "), end)
	}
}

func (s *Service) StartCall(c *gin.Context) {
	var body struct {
		CallerNo string `json:"callerNo"`
	}
	_ = c.ShouldBindJSON(&body)
	sid, cur, sess, err := s.startCore(body.CallerNo)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error()); return
	}
	rinfo.GinOK(c, gin.H{"sessionId": sid, "callerNo": sess.CallerNo, "node": cur,
		"transcript": sess.Transcript, "answers": sess.Answers, "done": sess.Done, "outcome": sess.Outcome}, "呼入已接入")
}

type inputReq struct {
	Key     string  `json:"key"`
	Message *string `json:"message"`
}

func (s *Service) Input(c *gin.Context) {
	sid := c.Param("sid")
	if sess := sessions[sid]; sess == nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "会话不存在（可能已被重置）"); return
	} else if sess.Done {
		rinfo.GinFail(c, rinfo.CodeConflict, "通话已结束"); return
	}
	var req inputReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误"); return
	}
	cur, sess, invalid, err := s.keyCore(sid, req.Key, req.Message)
	if err == errSessionNotFound {
		rinfo.GinFail(c, rinfo.CodeNotFound, "会话不存在（可能已被重置）"); return
	}
	if err == errSessionDone {
		rinfo.GinFail(c, rinfo.CodeConflict, "通话已结束"); return
	}
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error()); return
	}
	if invalid {
		rinfo.GinOK(c, gin.H{"sessionId": sid, "node": cur, "transcript": sess.Transcript, "answers": sess.Answers,
			"done": false, "outcome": nil, "invalidKey": true}, "按键无效，已重播当前节点")
		return
	}
	rinfo.GinOK(c, gin.H{"sessionId": sid, "node": cur, "transcript": sess.Transcript, "answers": sess.Answers,
		"done": sess.Done, "outcome": sess.Outcome}, "ok")
}

func (s *Service) Hangup(c *gin.Context) {
	sid := c.Param("sid")
	sess, err := s.hangupCore(sid)
	if err == errSessionNotFound {
		rinfo.GinFail(c, rinfo.CodeNotFound, "会话不存在（可能已被重置）"); return
	}
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error()); return
	}
	rinfo.GinOK(c, gin.H{"sessionId": sid, "outcome": sess.Outcome, "answers": sess.Answers}, "主叫挂断，通话已落库")
}

func (s *Service) Logs(c *gin.Context) {
	rows, err := s.db.Query(`SELECT id,caller_no,start_time,end_time,outcome,path_json,answers_json
		FROM ivr_call_log ORDER BY id DESC LIMIT 20`)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error()); return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id int64
		var callerNo, start, outcome, path, ans string
		var end interface{}
		_ = rows.Scan(&id, &callerNo, &start, &end, &outcome, &path, &ans)
		out = append(out, map[string]interface{}{"id": id, "caller_no": callerNo, "start_time": start,
			"end_time": end, "outcome": outcome, "path_json": path, "answers_json": ans})
	}
	rinfo.GinOK(c, out, "ok")
}
