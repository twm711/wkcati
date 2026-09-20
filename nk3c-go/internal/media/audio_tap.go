// audio_tap.go 会话音频单一消费者：录音（近端直通 tap）+ DTMF 事件泵 二合一
// 关键约束：RTP 管道单消费者——录音 tap 与 DTMF 必须串在同一条读链上，否则 marker 包被吞、DTMF 整事件丢失
package media

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/emiago/diago"
	diagoMedia "github.com/emiago/diago/media"
)

type audioTap struct {
	keys    chan rune
	closeFn func() error // 收尾：刷 WAV 头并关文件（无录音时为 nil）
	path    string       // 录音文件路径（""=未启用）
}

// startAudioTap 启动会话音频 tap：
//   - recPath != ""：AudioStereoRecordingWav 直通层上叠加 RTP 事件检测（近端录音 + DTMF）
//   - 否则：diago AudioReaderDTMF 直读
func startAudioTap(m *diago.DialogMedia, recPath string) *audioTap {
	t := &audioTap{keys: make(chan rune, 16), closeFn: func() error { return nil }}

	var pr io.Reader
	var check func() (rune, bool)

	if recPath != "" {
		if err := os.MkdirAll(filepath.Dir(recPath), 0o755); err != nil {
			slog.Warn("录音目录创建失败", "err", err)
		}
		f, err := os.OpenFile(recPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
		if err == nil {
			rec, rerr := m.AudioStereoRecordingCreate(f)
			if rerr == nil {
				t.path = recPath
				mon := rec.AudioReader()
				dr := diagoMedia.NewRTPDTMFReader(diagoMedia.CodecTelephoneEvent8000, m.RTPPacketReader, mon)
				pr, check = io.Reader(dr), dr.ReadDTMF
				t.closeFn = func() error {
					e1 := rec.Close()
					e2 := f.Close()
					if e1 != nil {
						return e1
					}
					return e2
				}
			} else {
				slog.Warn("录音创建失败，降级为纯 DTMF", "err", rerr)
				_ = f.Close()
			}
		} else {
			slog.Warn("录音文件打开失败，降级为纯 DTMF", "err", err)
		}
	}
	if pr == nil {
		ar, err := m.AudioReader()
		if err != nil {
			slog.Warn("音频通道创建失败（无媒体能力）", "err", err)
			close(t.keys)
			return t
		}
		dr := diagoMedia.NewRTPDTMFReader(diagoMedia.CodecTelephoneEvent8000, m.RTPPacketReader, ar)
		pr, check = io.Reader(dr), dr.ReadDTMF
	}

	go func() {
		defer func() { _ = recover() }() // 会话关闭竞态保护
		buf := make([]byte, diagoMedia.RTPBufSize)
		for {
			if _, err := pr.Read(buf); err != nil {
				sync.OnceFunc(func() { close(t.keys) })()
				return
			}
			if d, ok := check(); ok && d != 0 {
				select {
				case t.keys <- d:
				default:
				}
			}
		}
	}()
	return t
}

// key 阻塞取一键；超时返回 0；通话结束返回 error
func (t *audioTap) key(ctx context.Context, wait time.Duration) (rune, error) {
	select {
	case d, ok := <-t.keys:
		if !ok {
			return 0, io.ErrClosedPipe
		}
		return d, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(wait):
		return 0, nil
	}
}

// close 收尾（幂等；刷录音 WAV 头）
func (t *audioTap) close() error {
	if t.closeFn != nil {
		return t.closeFn()
	}
	return nil
}
