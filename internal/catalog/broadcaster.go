package catalog

import (
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const writeTimeout = 5 * time.Second

// Broadcaster manages WebSocket client connections and broadcasts messages.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
	logger  *slog.Logger
}

// NewBroadcaster creates a new Broadcaster.
func NewBroadcaster(logger *slog.Logger) *Broadcaster {
	return &Broadcaster{
		clients: make(map[*websocket.Conn]struct{}),
		logger:  logger,
	}
}

// AddClient registers a WebSocket connection for broadcasts.
func (b *Broadcaster) AddClient(conn *websocket.Conn) {
	b.mu.Lock()
	b.clients[conn] = struct{}{}
	count := len(b.clients)
	b.mu.Unlock()
	b.logger.Info("ws: client connected", "clients", count)
}

// RemoveClient unregisters and closes a WebSocket connection.
func (b *Broadcaster) RemoveClient(conn *websocket.Conn) {
	b.mu.Lock()
	if _, ok := b.clients[conn]; ok {
		delete(b.clients, conn)
		conn.Close()
	}
	count := len(b.clients)
	b.mu.Unlock()
	b.logger.Info("ws: client disconnected", "clients", count)
}

// Broadcast sends a message to all connected clients. Clients that fail to
// receive the message within the write deadline are removed.
func (b *Broadcaster) Broadcast(msg []byte) {
	b.mu.RLock()
	clients := make([]*websocket.Conn, 0, len(b.clients))
	for c := range b.clients {
		clients = append(clients, c)
	}
	b.mu.RUnlock()

	var failed []*websocket.Conn
	for _, c := range clients {
		c.SetWriteDeadline(time.Now().Add(writeTimeout))
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			failed = append(failed, c)
		}
	}

	if len(failed) > 0 {
		b.mu.Lock()
		for _, c := range failed {
			if _, ok := b.clients[c]; ok {
				delete(b.clients, c)
				c.Close()
			}
		}
		b.mu.Unlock()
		b.logger.Debug("ws: removed slow/dead clients", "removed", len(failed))
	}
}

// ClientCount returns the number of connected clients.
func (b *Broadcaster) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}
