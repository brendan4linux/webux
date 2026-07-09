// Package ws provides the WebSocket hub for real-time events.
package ws

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin:     checkOrigin,
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
}

// checkOrigin allows only requests where the Origin matches the Host header.
// This prevents cross-site WebSocket hijacking from other browser tabs.
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// No Origin header — allow (non-browser clients, curl, etc.)
		return true
	}
	// Strip scheme from origin and compare to Host
	origin = strings.TrimPrefix(origin, "https://")
	origin = strings.TrimPrefix(origin, "http://")
	return origin == r.Host
}

// EventType classifies WebSocket messages so the frontend can route them.
type EventType string

const (
	EventMetric  EventType = "metric"
	EventLog     EventType = "log"
	EventCLIEcho EventType = "cli_echo"
	EventAlert   EventType = "alert"
)

// Event is the envelope for all WebSocket messages.
type Event struct {
	Type    EventType   `json:"type"`
	Payload interface{} `json:"payload"`
	SentAt  time.Time   `json:"sent_at"`
}

type client struct {
	conn *websocket.Conn
	send chan []byte
}

// Hub manages all connected WebSocket clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[*client]bool
	reg     chan *client
	unreg   chan *client
	bcast   chan []byte
}

// NewHub creates an uninitialised Hub. Call Run() in a goroutine.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[*client]bool),
		reg:     make(chan *client, 8),
		unreg:   make(chan *client, 8),
		bcast:   make(chan []byte, 256),
	}
}

// Run is the hub event loop — must be called in its own goroutine.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.reg:
			h.mu.Lock()
			h.clients[c] = true
			h.mu.Unlock()

		case c := <-h.unreg:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			h.mu.Unlock()

		case msg := <-h.bcast:
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					// Slow client — drop the message to avoid blocking
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends a typed event to all connected clients.
func (h *Hub) Broadcast(eventType EventType, payload interface{}) {
	evt := Event{Type: eventType, Payload: payload, SentAt: time.Now().UTC()}
	b, err := json.Marshal(evt)
	if err != nil {
		slog.Warn("ws marshal error", "err", err)
		return
	}
	select {
	case h.bcast <- b:
	default:
		slog.Warn("ws broadcast channel full — dropping event")
	}
}

// ServeWS upgrades an HTTP connection and registers the client.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
    slog.Info("ws upgrade attempt", "remote", r.RemoteAddr, "origin", r.Header.Get("Origin"), "upgrade", r.Header.Get("Upgrade"), "cookie", r.Header.Get("Cookie") != "")
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        slog.Error("ws upgrade failed", "err", err, "remote", r.RemoteAddr)
        return
    }
    slog.Info("ws client connected", "remote", r.RemoteAddr)

	c := &client{conn: conn, send: make(chan []byte, 64)}
	h.reg <- c

	go c.writePump()
	go c.readPump(h)
}

func (c *client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *client) readPump(h *Hub) {
	defer func() {
		h.unreg <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}
