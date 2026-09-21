// Package realtime WebSocket Hub：监控墙实时推送（替代前端轮询；断线由前端降级轮询）
package realtime

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// PayloadFn 当前墙面快照（复用 monitor 查询逻辑）
type PayloadFn func(tenantID int64) map[string]interface{}

type client struct {
	conn     *websocket.Conn
	tenantID int64
}

// Hub 客户端注册表 + 2s 周期广播
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
	payload PayloadFn
	upg     websocket.Upgrader
}

func NewHub(payload PayloadFn) *Hub {
	return &Hub{
		clients: map[*client]struct{}{},
		payload: payload,
		upg: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // 演示：预览代理跨源放行
		},
	}
}

// ServeWS 升级并挂载一个客户端（阻塞；断开自动清理）
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request, tenantIDs ...int64) {
	tenantID := int64(0)
	if len(tenantIDs) > 0 {
		tenantID = tenantIDs[0]
	}
	c, err := h.upg.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("WS 升级失败", "err", err)
		return
	}
	cl := &client{conn: c, tenantID: tenantID}
	h.mu.Lock()
	h.clients[cl] = struct{}{}
	n := len(h.clients)
	h.mu.Unlock()
	slog.Info("监控墙 WS 客户端接入", "online", n)

	defer func() {
		h.mu.Lock()
		delete(h.clients, cl)
		h.mu.Unlock()
		_ = c.Close()
	}()
	// 读泵：仅保活（客户端不发送业务消息）
	go func() {
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}()
	// 写泵：立即推一帧 + 2s 周期推
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	push := func() bool {
		b, err := json.Marshal(map[string]interface{}{"type": "wall", "data": h.payload(cl.tenantID), "ts": time.Now().UnixMilli()})
		if err != nil {
			return true
		}
		_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
			return false
		}
		return true
	}
	if !push() {
		return
	}
	for range tick.C {
		if !push() {
			return
		}
	}
}
