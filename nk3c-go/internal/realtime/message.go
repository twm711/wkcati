package realtime

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type messageClient struct {
	userID int64
	conn   *websocket.Conn
	send   chan []byte
}

type MessageHub struct {
	mu      sync.Mutex
	clients map[*messageClient]struct{}
	upg     websocket.Upgrader
}

func NewMessageHub() *MessageHub {
	return &MessageHub{clients: map[*messageClient]struct{}{}, upg: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}}
}

func (h *MessageHub) ServeWS(userID int64, w http.ResponseWriter, r *http.Request) {
	c, err := h.upg.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	cl := &messageClient{userID: userID, conn: c, send: make(chan []byte, 16)}
	h.mu.Lock()
	h.clients[cl] = struct{}{}
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.clients, cl); h.mu.Unlock(); _ = c.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for b := range cl.send {
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if c.WriteMessage(websocket.TextMessage, b) != nil {
				return
			}
		}
	}()
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
	close(cl.send)
	<-done
}

func (h *MessageHub) Publish(userID int64, text string, byUserID int64) {
	b, err := json.Marshal(map[string]interface{}{"type": "message", "text": text, "fromUserId": byUserID, "ts": time.Now().UnixMilli()})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for cl := range h.clients {
		if cl.userID == userID {
			select {
			case cl.send <- b:
			default:
				slog.Warn("坐席消息背压丢弃", "userId", userID)
			}
		}
	}
}
