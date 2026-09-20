package media

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CallController 是 API 域对话务域的最小控制接口；当前先落地 HANGUP。
type CallController interface {
	Hangup(callID int64) error
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
