// server.go 话务域 SIP 服务器：diago 驱动，走线逻辑全部委托 ivr 核心引擎（接口隔离，见 GoReact 文档 M2）
package media

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"

	"nk3c/internal/ivr"
)

// IvrDriver 话务域 → 业务域 的窄接口（ivr.Service 实现；测试可用 Mock 替换）
type IvrDriver interface {
	StartSIP(callerNo string) (ivr.NodeState, error)
	KeySIP(sid, key, message string) (ivr.NodeState, error)
	HangupSIP(sid string) (ivr.NodeState, error)
}

// SIPServer 话务域服务器（呼入 IVR + 外呼执行器）
type SIPServer struct {
	Driver    IvrDriver
	BindHost  string // SIP/RTP 绑定地址（默认 0.0.0.0）
	BindPort  int    // SIP 端口（RTP 使用同主机临时端口段）
	DtmfWait  time.Duration
	RecordDir string                 // 录音目录（默认 recordings/）
	OnFinish  func(st ivr.NodeState) // 会话结束回调（监控/日志钩子）

	Outbound *OutboundCaller // Start 后可用（外呼腿；需配 Peer 路由）
}

// Hangup implements monitor.CTIController; Outbound is populated when Start begins.
func (s *SIPServer) Hangup(callID int64) error {
	if s.Outbound == nil {
		return fmt.Errorf("话务域尚未启动")
	}
	return s.Outbound.Hangup(callID)
}

// Start 阻塞运行（调用方包 goroutine）；ctx 取消即关停
func (s *SIPServer) Start(ctx context.Context) error {
	if s.DtmfWait == 0 {
		s.DtmfWait = 8 * time.Second
	}
	host := s.BindHost
	if host == "" {
		host = "0.0.0.0"
	}
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("NK3C-Media/1.0"))
	if err != nil {
		return fmt.Errorf("SIP UA 初始化失败: %w", err)
	}
	dg := diago.NewDiago(ua, diago.WithTransport(diago.Transport{
		Transport: "udp",
		BindHost:  host,
		BindPort:  s.BindPort,
	}))
	recDir := s.RecordDir
	if recDir == "" {
		recDir = "recordings"
	}
	s.Outbound = &OutboundCaller{
		dg:        dg,
		RecordDir: recDir,
		DtmfWait:  s.DtmfWait,
	}
	slog.Info("话务域 SIP 服务器已启动", "addr", fmt.Sprintf("%s:%d (udp)", host, s.BindPort), "recordDir", recDir)
	return dg.Serve(ctx, func(in *diago.DialogServerSession) {
		s.handle(ctx, in, recDir)
	})
}

// handle 单通呼入：diago 会话 ↔ ivr 核心引擎 的驱动循环
func (s *SIPServer) handle(ctx context.Context, in *diago.DialogServerSession, recDir string) {
	callerNo := in.FromUser()
	st, err := s.Driver.StartSIP(callerNo)
	if err != nil {
		slog.Error("IVR 引擎接续失败，拒接", "caller", callerNo, "err", err)
		_ = in.Close()
		return
	}
	slog.Info("SIP 呼入接入", "caller", callerNo, "session", st.SessionID)

	_ = in.Trying()
	_ = in.Ringing()
	if err := in.Answer(); err != nil {
		slog.Error("应答失败", "err", err)
		_, _ = s.Driver.HangupSIP(st.SessionID) // ABANDONED 落库
		return
	}
	tap := startAudioTap(&in.DialogMedia, filepath.Join(recDir, "in-"+st.SessionID+".wav"))
	pb, pbErr := in.PlaybackCreate()

	timeouts := 0
	for !st.Done {
		// 播当前节点提示音（invalid 重播同款）
		if pbErr == nil {
			tone := PromptFor(st.NodeType)
			if _, err := pb.Play(bytes.NewReader(tone), "audio/wav"); err != nil {
				slog.Warn("提示音播放中断（主叫可能已挂断）", "err", err)
				break
			}
		}
		if st.NodeType == "end" || st.Done {
			break
		}
		// 收 DTMF（会话音频 tap：录音与事件检测同链）
		key, kerr := tap.key(ctx, s.DtmfWait)
		if kerr != nil {
			slog.Warn("DTMF 读取中断（主叫可能已挂断）", "err", kerr)
			break
		}
		if key == 0 { // 超时无键：重播；连续 3 次超时挂断（ABANDONED）
			timeouts++
			st.Invalid = false
			if timeouts >= 3 {
				slog.Info("连续超时，自动挂断", "session", st.SessionID)
				break
			}
			continue
		}
		timeouts = 0
		msg := ""
		if st.NodeType == "voicemail" && key == '#' {
			msg = "（语音留言）"
		}
		next, err := s.Driver.KeySIP(st.SessionID, string(key), msg)
		if err != nil {
			slog.Error("IVR 引擎按键处理失败", "err", err)
			break
		}
		st = next
	}

	// 收尾：未走完（挂断/超时/错误）→ ABANDONED 落库；已走完 → 补结束音后 BYE
	if !st.Done {
		st, _ = s.Driver.HangupSIP(st.SessionID)
	} else if pbErr == nil && st.NodeType == "end" {
		_, _ = pb.Play(bytes.NewReader(PromptFor("end")), "audio/wav")
	}
	st.RecordFile = tap.path
	_ = tap.close()
	slog.Info("SIP 通话结束", "session", st.SessionID, "outcome", st.Outcome, "answers", st.Answers, "record", tap.path)
	if s.OnFinish != nil {
		s.OnFinish(st)
	}
	_ = in.Hangup(ctx)
}

// （readDTMF 已泛化为 readDTMFMedia，见 outbound.go）
