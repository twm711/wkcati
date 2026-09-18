// server.go 话务域 SIP 服务器：diago 驱动，走线逻辑全部委托 ivr 核心引擎（接口隔离，见 GoReact 文档 M2）
package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// SIPServer 话务域服务器（呼入 IVR）
type SIPServer struct {
	Driver   IvrDriver
	BindHost string // SIP/RTP 绑定地址（默认 0.0.0.0）
	BindPort int    // SIP 端口（RTP 使用同主机临时端口段）
	DtmfWait time.Duration
	OnFinish func(st ivr.NodeState) // 会话结束回调（监控/日志钩子）
}

var errStop = errors.New("dtmf-received")

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
	slog.Info("话务域 SIP 服务器已启动", "addr", fmt.Sprintf("%s:%d (udp)", host, s.BindPort))
	return dg.Serve(ctx, func(in *diago.DialogServerSession) {
		s.handle(ctx, in)
	})
}

// handle 单通呼入：diago 会话 ↔ ivr 核心引擎 的驱动循环
func (s *SIPServer) handle(ctx context.Context, in *diago.DialogServerSession) {
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
		// 收 DTMF
		key, kerr := s.readDTMF(ctx, in, s.DtmfWait)
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
	slog.Info("SIP 通话结束", "session", st.SessionID, "outcome", st.Outcome, "answers", st.Answers)
	if s.OnFinish != nil {
		s.OnFinish(st)
	}
	_ = in.Hangup(ctx)
}

// readDTMF 阻塞收一个 DTMF 键；超时返回 (0,nil)；主叫挂断等错误向上返回
func (s *SIPServer) readDTMF(ctx context.Context, in *diago.DialogServerSession, wait time.Duration) (rune, error) {
	reader, err := in.AudioReaderDTMF()
	if err != nil {
		return 0, err
	}
	got := make(chan rune, 1)
	fail := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		lerr := reader.Listen(func(d rune) error {
			select {
			case got <- d:
			default:
			}
			return errStop
		}, wait)
		if lerr != nil && !errors.Is(lerr, errStop) {
			fail <- lerr
		}
	}()
	select {
	case d := <-got:
		return d, nil
	case lerr := <-fail:
		return 0, lerr
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(wait + 500*time.Millisecond):
		return 0, nil
	}
}
