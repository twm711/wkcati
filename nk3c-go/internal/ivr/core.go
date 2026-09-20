// core.go 话务域共用核心：HTTP 模拟通道与真实 SIP 通道（internal/media）走同一套走线引擎
// 保证「网页模拟按键」与「真实话机 DTMF」行为逐字节一致（M2 接口隔离原则）
package ivr

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"nk3c/internal/store"
)

// NodeState SIP 驱动可见的会话/节点快照（不含 gin/内部类型）
type NodeState struct {
	SessionID  string
	CallerNo   string
	RecordFile string // 录音落盘路径（呼入腿）
	NodeID     string
	NodeType   string
	Text       string
	Branches   map[string]string // menu 分支：键→下一节点
	Options    map[string]string // question 选项：键→答案文本
	Answers    map[string]string
	Done       bool
	Outcome    string
	Invalid    bool // 上一按键无效，需重播当前节点
}

func (s *Service) snapshot(sid string, cur node, sess *session, invalid bool) NodeState {
	if cur.ID == "" {
		cur = node{ID: "(none)", Type: "end"}
	}
	return NodeState{
		SessionID: sid, CallerNo: sess.CallerNo, NodeID: cur.ID, NodeType: cur.Type, Text: cur.Text,
		Branches: cur.Branches, Options: cur.Options,
		Answers: sess.Answers, Done: sess.Done, Outcome: sess.Outcome, Invalid: invalid,
	}
}

// StartSIP 呼入接续（真实话机入口；与 HTTP StartCall 同一走线）
func (s *Service) StartSIP(callerNo string) (NodeState, error) {
	sid, cur, sess, err := s.startCore(callerNo, 1)
	if err != nil {
		return NodeState{}, err
	}
	return s.snapshot(sid, cur, sess, false), nil
}

// KeySIP 按键/留言（真实话机 DTMF 入口；与 HTTP Input 同一走线）
func (s *Service) KeySIP(sid, key, message string) (NodeState, error) {
	var mp *string
	if message != "" {
		mp = &message
	}
	cur, sess, invalid, err := s.keyCore(sid, key, mp)
	if err != nil {
		return NodeState{}, err
	}
	return s.snapshot(sid, cur, sess, invalid), nil
}

// HangupSIP 主叫挂断（幂等；未走完即 ABANDONED 落库）
func (s *Service) HangupSIP(sid string) (NodeState, error) {
	sess, err := s.hangupCore(sid)
	if err != nil {
		return NodeState{}, err
	}
	return s.snapshot(sid, node{}, sess, false), nil
}

// ── 核心实现（gin handler 与 SIP 驱动共同调用）─────────────────────────

func (s *Service) startCore(callerNo string, projectID int64) (string, node, *session, error) {
	f, nodes, err := s.flowNodes()
	if err != nil {
		return "", node{}, nil, err
	}
	sid := fmt.Sprintf("%x", time.Now().UnixNano())
	if callerNo == "" {
		callerNo = "139" + sid[:8]
	}
	if projectID == 0 {
		projectID = 1
	}
	sess := &session{CallerNo: callerNo, ProjectID: projectID, Answers: map[string]string{}, Start: store.NowISO()}
	sessions[sid] = sess
	n := s.enter(nodes, nodes[f.Entry], sess)
	sess.Current = n.ID
	return sid, n, sess, nil
}

func (s *Service) keyCore(sid string, key string, message *string) (node, *session, bool, error) {
	sess := sessions[sid]
	if sess == nil {
		return node{}, nil, false, errSessionNotFound
	}
	if sess.Done {
		return node{}, sess, false, errSessionDone
	}
	_, nodes, err := s.flowNodes()
	if err != nil {
		return node{}, nil, false, err
	}
	cur := nodes[sess.Current]
	k := strings.TrimSpace(key)
	var nxt string
	invalid := false
	switch cur.Type {
	case "menu":
		v, ok := cur.Branches[k]
		if !ok {
			invalid = true
		} else {
			nxt = v
		}
	case "question":
		v, ok := cur.Options[k]
		if !ok {
			invalid = true
		} else {
			tag := orDefault(cur.Tag, cur.ID)
			sess.Answers[tag] = v
			nxt = cur.Next
		}
	case "voicemail":
		if k == "#" || message != nil {
			if message != nil {
				sess.Answers["留言"] = *message
			} else {
				sess.Answers["留言"] = "(语音留言)"
			}
			nxt = cur.Next
		} else {
			invalid = true
		}
	default:
		invalid = true
	}
	if invalid {
		sess.Transcript = append(sess.Transcript, map[string]interface{}{"node": cur.ID, "type": "INVALID", "text": "按键 " + k + " 无效，请重听"})
		return cur, sess, true, nil
	}
	n := s.enter(nodes, nodes[nxt], sess)
	sess.Current = n.ID
	return n, sess, false, nil
}

func (s *Service) hangupCore(sid string) (*session, error) {
	sess := sessions[sid]
	if sess == nil {
		return nil, errSessionNotFound
	}
	if sess.Done {
		return sess, nil
	}
	sess.Done = true
	if sess.Outcome == "" {
		sess.Outcome = "ABANDONED"
	}
	s.finalize(sess)
	return sess, nil
}

// 哨兵与助手（避免 core.go 与 ivr.go 重复导入冲突，集中在此）
var (
	errSessionNotFound = errors.New("会话不存在（可能已被重置）")
	errSessionDone     = errors.New("通话已结束")
)
