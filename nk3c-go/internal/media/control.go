package media

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo/sip"
)

// CallController 是 API 域对话务域的最小控制接口；当前先落地 HANGUP。
type CallController interface {
	Hangup(callID int64) error
	AddSupervisor(ctx context.Context, callID int64, host string, port int) error
}

// ActiveCalls 保存外呼/桥接中的 SIP legs。媒体域结束时必须注销，避免控制已结束通话。
type ActiveCalls struct {
	mu    sync.Mutex
	calls map[int64][]func(context.Context) error
}

func (o *OutboundCaller) activeStore() *ActiveCalls {
	o.controlMu.Lock()
	defer o.controlMu.Unlock()
	if o.active == nil {
		o.active = &ActiveCalls{calls: map[int64][]func(context.Context) error{}}
	}
	return o.active
}

func (a *ActiveCalls) add(callID int64, fn func(context.Context) error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls[callID] = append(a.calls[callID], fn)
}
func (a *ActiveCalls) remove(callID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.calls, callID)
}
func (a *ActiveCalls) hangup(callID int64) error {
	a.mu.Lock()
	legs := append([]func(context.Context) error(nil), a.calls[callID]...)
	a.mu.Unlock()
	if len(legs) == 0 {
		return fmt.Errorf("话务 %d 不在活动通话表", callID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var first error
	for _, fn := range legs {
		if err := fn(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (o *OutboundCaller) register(callID int64, fn func(context.Context) error) {
	o.activeStore().add(callID, fn)
}
func (o *OutboundCaller) unregister(callID int64)   { o.activeStore().remove(callID) }
func (o *OutboundCaller) Hangup(callID int64) error { return o.activeStore().hangup(callID) }

func (o *OutboundCaller) registerMix(callID int64, mix *diago.BridgeMix) {
	o.mixMu.Lock()
	defer o.mixMu.Unlock()
	if o.mixes == nil {
		o.mixes = map[int64]*diago.BridgeMix{}
	}
	o.mixes[callID] = mix
}

func (o *OutboundCaller) unregisterMix(callID int64) {
	o.mixMu.Lock()
	defer o.mixMu.Unlock()
	delete(o.mixes, callID)
}

// AddSupervisor adds a third SIP leg to an active BridgeMix. This is the
// media primitive used by BARGE; LISTEN still needs an upstream mute control.
func (o *OutboundCaller) AddSupervisor(ctx context.Context, callID int64, host string, port int) error {
	o.mixMu.Lock()
	mix := o.mixes[callID]
	o.mixMu.Unlock()
	if mix == nil {
		return fmt.Errorf("活动混音桥不存在")
	}
	if o.dg == nil {
		return fmt.Errorf("话务域未启动")
	}
	dialogs := mix.DialogSessionsList()
	if len(dialogs) == 0 {
		return fmt.Errorf("混音桥无主叫腿")
	}
	from := &sip.FromHeader{DisplayName: "NK3C 质检督导", Address: sip.Uri{User: "supervisor", Host: "nk3c.local"}, Params: sip.NewParams()}
	d, err := o.dg.NewDialog(sip.Uri{User: "supervisor", Host: host, Port: port}, diago.NewDialogOptions{Transport: "udp"})
	if err != nil {
		return err
	}
	if err := d.Invite(ctx, diago.InviteClientOptions{Originator: dialogs[0], Headers: []sip.Header{from}}); err != nil {
		_ = d.Close()
		return err
	}
	if err := d.Ack(ctx); err != nil {
		_ = d.Close()
		return err
	}
	if err := mix.AddDialogSession(d); err != nil {
		_ = d.Close()
		return err
	}
	o.register(callID, func(hctx context.Context) error {
		_ = mix.RemoveDialogSession(d)
		return d.Hangup(hctx)
	})
	return nil
}
