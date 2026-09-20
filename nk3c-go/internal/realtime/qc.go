// 质检事件 Hub：督导订阅 /api/qc/ws，坐席动作（派样/接通/结果码/强签）即时推送
// 帧格式：{"type":"qc","event":"ANSWER","data":{...},"ts":ms}
package realtime

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type eventClient struct {
	conn *websocket.Conn
	send chan []byte
}

// EventHub 事件驱动广播（与墙式 Hub 互补：无周期帧，事件到达即推）
type EventHub struct {
	mu      sync.Mutex
	clients map[*eventClient]struct{}
	upg     websocket.Upgrader
}

func NewEventHub() *EventHub {
	return &EventHub{
		clients: map[*eventClient]struct{}{},
		upg: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // 演示：预览代理跨源放行
		},
	}
}

// ServeWS 升级并挂载一个督导客户端（阻塞；断开自动清理）
func (h *EventHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	c, err := h.upg.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("质检 WS 升级失败", "err", err)
		return
	}
	cl := &eventClient{conn: c, send: make(chan []byte, 32)}
	h.mu.Lock()
	h.clients[cl] = struct{}{}
	n := len(h.clients)
	h.mu.Unlock()
	slog.Info("质检 WS 督导接入", "online", n)

	defer func() {
		h.mu.Lock()
		delete(h.clients, cl)
		h.mu.Unlock()
		_ = c.Close()
	}()
	// 写泵：独占写连接，排空 send
	done := make(chan struct{})
	go func() {
		defer close(done)
		for b := range cl.send {
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
				_ = c.Close()
				return
			}
		}
	}()
	// 读泵：仅保活（客户端不发送业务消息）
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
	h.mu.Lock()
	delete(h.clients, cl)
	h.mu.Unlock()
	_ = c.Close()
	close(cl.send)
	<-done
}

// Publish 向全部督导广播一个质检事件（无订阅者时零开销）
func (h *EventHub) Publish(event string, data map[string]interface{}) {
	frame := map[string]interface{}{
		"type":  "qc",
		"event": event,
		"data":  data,
		"ts":    time.Now().UnixMilli(),
	}
	b, err := json.Marshal(frame)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for cl := range h.clients {
		select {
		case cl.send <- b:
		default: // 背压丢弃（督导端断线由读泵感知清理）
		}
	}
}

// Online 当前订阅数（监控/测试用）
func (h *EventHub) Online() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
