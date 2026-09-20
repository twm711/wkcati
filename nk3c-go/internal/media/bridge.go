// bridge.go B2BUA 坐席桥接：派样话务 → 客户腿 + 坐席腿 双腿桥接（真人通话）
// 业务联动：客户腿接通即 MarkBridgeConnect（connect_time + 样本 INCALL）；任一端挂断桥散，
// 话务保持 OPEN 由坐席经 HTTP /api/agent/result 手工提交结果码（真人外呼标准动线）。
// 桥接模式录音交 M5 录音子系统（桥消费双腿音频，tap 需在 proxyMedia 链上拦截实现）。
package media

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/emiago/diago"
	sip "github.com/emiago/sipgo/sip"

	"nk3c/internal/agent"
)

// BridgeDriver 桥接腿业务回调
type BridgeDriver interface {
	MarkBridgeConnect(callID int64) error
}

// Bridge 坐席桥接执行（挂 OutboundCaller：复用 dg/路由/From 头）
// agentHost/agentPort 为坐席话机（或 IPPBX 坐席队列）SIP 地址；阻塞至任一端挂断
func (o *OutboundCaller) Bridge(ctx context.Context, callID int64, bd BridgeDriver, agentHost string, agentPort int) error {
	if o.dg == nil {
		return fmt.Errorf("话务域未启动（--sip-addr）")
	}
	if o.PeerHost == "" || o.PeerPort == 0 || agentHost == "" || agentPort == 0 {
		return fmt.Errorf("外呼路由或坐席地址未配置")
	}
	task, err := o.Driver.LoadOutbound(callID)
	if err != nil {
		return err
	}
	bridge := diago.NewBridge()
	from := &sip.FromHeader{
		DisplayName: "NK3C 坐席外呼",
		Address:     sip.Uri{User: task.CallerID, Host: "nk3c.local"},
		Params:      sip.NewParams(),
	}
	dial := func(host string, port int, user string) (*diago.DialogClientSession, error) {
		opts := diago.InviteOptions{Transport: "udp"}
		opts.Headers = append(opts.Headers, from)
		return o.dg.InviteBridge(ctx, sip.Uri{User: digitsOnly(user), Host: host, Port: port}, &bridge, opts)
	}

	slog.Info("B2BUA 桥接：呼叫客户", "call", callID, "phone", task.Phone)
	cust, err := dial(o.PeerHost, o.PeerPort, task.Phone)
	if err != nil {
		slog.Warn("客户腿未接通", "err", err)
		_, _, _ = o.Finish(callID, "NA") // 未接通 → NA 回池（业务规则统一）
		return fmt.Errorf("客户未接通: %w", err)
	}
	o.register(callID, func(ctx context.Context) error { return cust.Hangup(ctx) })
	defer o.unregister(callID)
	defer cust.Close()
	if err := bd.MarkBridgeConnect(callID); err != nil {
		slog.Warn("接通标记失败", "err", err)
	}
	slog.Info("客户已接通，呼叫坐席", "agent", fmt.Sprintf("%s:%d", agentHost, agentPort))
	ag, err := dial(agentHost, agentPort, task.CallerID)
	if err != nil {
		slog.Warn("坐席腿未接通，挂断客户", "err", err)
		_ = cust.Hangup(ctx)
		return fmt.Errorf("坐席未接通: %w", err)
	}
	o.register(callID, func(ctx context.Context) error { return ag.Hangup(ctx) })
	defer ag.Close()
	slog.Info("桥接建立（双方通话中）", "call", callID)

	// 任一端挂断 → 桥散
	select {
	case <-cust.Context().Done():
		slog.Info("客户挂断", "call", callID)
	case <-ag.Context().Done():
		slog.Info("坐席挂断", "call", callID)
	case <-time.After(30 * time.Minute):
		slog.Info("通话时长上限", "call", callID)
	}
	_ = cust.Hangup(ctx)
	_ = ag.Hangup(ctx)
	// 话务保持 OPEN：结果码由坐席人工经 HTTP 提交（真人动线）
	return nil
}

var _ = agent.OutboundCall{} // 编译锚：确保 agent 类型可见
