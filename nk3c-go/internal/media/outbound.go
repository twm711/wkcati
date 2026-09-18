// outbound.go 外呼腿：派样话务 → diago Invite 客户 → 自动问卷（提示音+RTP DTMF）→ 业务作答/结果码核心
// 业务规则全部走 agent 核心（answerCore/resultCore），与坐席 HTTP 链路同事务同约束
package media

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/emiago/diago"
	sip "github.com/emiago/sipgo/sip"

	"nk3c/internal/agent"
)

// AgentDriver 外呼腿 → 业务域 的窄接口（*agent.Service 实现）
type AgentDriver interface {
	LoadOutbound(callID int64) (agent.OutboundCall, error)
	AnswerOutbound(callID, questionID int64, optionIDs []int64, answerText string, numeric *float64) (int64, string, error)
	FinishOutbound(callID int64, resultCode string) (map[string]interface{}, string, error)
}

// OutboundCaller 外呼执行器（dg 由 SIPServer.Start 装配，与呼入共用 UA/传输）
type OutboundCaller struct {
	dg          *diago.Diago
	Driver      AgentDriver
	PeerHost    string // 外呼路由（沙箱=被叫模拟器地址；生产=运营商/IPPBX 中继）
	PeerPort    int
	RecordDir   string
	DtmfWait    time.Duration
	OnRecorded  func(callID int64, path string) // 录音落盘回调
	currentCall int64
}

// Dial 执行一通外呼自动调研；返回结果码提交数据（供 HTTP 响应）
func (o *OutboundCaller) Dial(ctx context.Context, callID int64) (map[string]interface{}, string, error) {
	if o.dg == nil {
		return nil, "", fmt.Errorf("话务域未启动（--sip-addr）")
	}
	if o.PeerHost == "" || o.PeerPort == 0 {
		return nil, "", fmt.Errorf("未配置外呼路由（--outbound host:port）")
	}
	task, err := o.Driver.LoadOutbound(callID)
	if err != nil {
		return nil, "", err
	}
	o.currentCall = callID
	wait := o.DtmfWait
	if wait == 0 {
		wait = 6 * time.Second
	}
	uri := sip.Uri{User: digitsOnly(task.Phone), Host: o.PeerHost, Port: o.PeerPort}
	opts := diago.InviteOptions{Transport: "udp"}
	opts.Headers = append(opts.Headers, &sip.FromHeader{
		DisplayName: "NK3C 调查中心",
		Address:     sip.Uri{User: task.CallerID, Host: "nk3c.local"},
		Params:      sip.NewParams(),
	})
	c, err := o.dg.Invite(ctx, uri, opts)
	if err != nil {
		// 未接通 → NA（回池重拨，业务规则统一）
		slog.Warn("外呼未接通", "call", callID, "phone", task.Phone, "err", err)
		return o.Finish(callID, "NA")
	}
	defer func() { _ = c.Close() }()
	slog.Info("外呼已接通", "call", callID, "phone", task.Phone, "questions", len(task.Questions))

	tap := startAudioTap(&c.DialogMedia, filepath.Join(o.RecordDir, fmt.Sprintf("out-%d", callID)+".wav"))
	if tap.path != "" && o.OnRecorded != nil {
		o.OnRecorded(callID, tap.path)
	}
	defer func() { _ = tap.close() }()
	pb, pbErr := c.PlaybackCreate()
	play := func(tone []byte) {
		if pbErr == nil {
			_, _ = pb.Play(strings.NewReader(string(tone)), "audio/wav")
		} else {
			time.Sleep(300 * time.Millisecond) // 无播放能力时保留节拍（兜底）
		}
	}

	answered := 0
	for _, q := range task.Questions {
		if !o.askOne(ctx, tap, callID, wait, q, play) {
			break // 主叫挂断/两次无效 → 中止
		}
		answered++
	}
	play(PromptFor("end"))
	_ = c.Hangup(ctx)

	code := "BREAKOFF"
	switch {
	case answered == 0:
		code = "NA"
	case answered == len(task.Questions):
		code = "SUCCESS"
	}
	return o.Finish(callID, code)
}

// askOne 播题收键并作答；false=中止（挂断或两次无效）
func (o *OutboundCaller) askOne(ctx context.Context, tap *audioTap, callID int64, wait time.Duration, q agent.OutboundQuestion, play func([]byte)) bool {
	for attempt := 0; attempt < 2; attempt++ {
		play(PromptFor("question"))
		key, err := tap.key(ctx, wait)
		if err != nil {
			return false // 主叫挂断/RTP 断
		}
		if key == 0 {
			continue // 超时重播一次
		}
		switch {
		case q.Type == "single" || q.Type == "multi":
			idx := int(key - '1')
			if idx < 0 || idx >= len(q.Options) {
				play(PromptFor("menu")) // 无效短音后重播
				continue
			}
			opt := q.Options[idx]
			if _, _, err := o.Driver.AnswerOutbound(callID, q.QuestionID, []int64{opt.OptionID}, "", nil); err != nil {
				slog.Warn("外呼作答被业务拒绝", "q", q.QuestionID, "err", err)
				return false
			}
			play(PromptFor("play")) // 确认音
			return true
		case q.Type == "number":
			v := float64(key - '0')
			if _, _, err := o.Driver.AnswerOutbound(callID, q.QuestionID, nil, "", &v); err != nil {
				slog.Warn("外呼数值作答被拒", "q", q.QuestionID, "err", err)
				return false
			}
			play(PromptFor("play"))
			return true
		}
	}
	return false
}

// Finish 收尾结果码（桥接业务核心）
func (o *OutboundCaller) Finish(callID int64, code string) (map[string]interface{}, string, error) {
	return o.Driver.FinishOutbound(callID, code)
}

// digitsOnly 号码数字清洗
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '+' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

