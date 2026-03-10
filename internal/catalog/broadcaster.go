package catalog

import (
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const writeTimeout = 5 * time.Second
const clientSendBuf = 64

// client wraps a websocket connection with a dedicated write goroutine.
type client struct {
	conn *websocket.Conn
	send chan []byte
}

// Broadcaster manages WebSocket client connections and broadcasts messages.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]*client
	logger  *slog.Logger
}

// NewBroadcaster creates a new Broadcaster.
func NewBroadcaster(logger *slog.Logger) *Broadcaster {
	return &Broadcaster{
		clients: make(map[*websocket.Conn]*client),
		logger:  logger,
	}
}

// AddClient registers a WebSocket connection for broadcasts.
func (b *Broadcaster) AddClient(conn *websocket.Conn) {
	c := &client{conn: conn, send: make(chan []byte, clientSendBuf)}
	b.mu.Lock()
	b.clients[conn] = c
	count := len(b.clients)
	b.mu.Unlock()
	b.logger.Info("ws: client connected", "clients", count)
	go b.writePump(c)
}

// writePump serialises all writes for a single client.
func (b *Broadcaster) writePump(c *client) {
	defer b.RemoveClient(c.conn)
	for msg := range c.send {
		c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

// RemoveClient unregisters and closes a WebSocket connection.
func (b *Broadcaster) RemoveClient(conn *websocket.Conn) {
	b.mu.Lock()
	c, ok := b.clients[conn]
	if ok {
		delete(b.clients, conn)
	}
	count := len(b.clients)
	b.mu.Unlock()
	if ok {
		close(c.send)
		conn.Close()
		b.logger.Info("ws: client disconnected", "clients", count)
	}
}

// Broadcast sends a message to all connected clients. Clients whose send
// buffer is full are dropped immediately.
func (b *Broadcaster) Broadcast(msg []byte) {
	b.mu.RLock()
	clients := make([]*client, 0, len(b.clients))
	for _, c := range b.clients {
		clients = append(clients, c)
	}
	b.mu.RUnlock()

	for _, c := range clients {
		select {
		case c.send <- msg:
		default:
			// slow client — drop and remove
			go b.RemoveClient(c.conn)
		}
	}
}

// ClientCount returns the number of connected clients.
func (b *Broadcaster) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}
