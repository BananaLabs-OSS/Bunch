package presence

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// FriendLister returns the account IDs of a user's accepted friends.
type FriendLister interface {
	ListFriendIDs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error)
}

// Message is the JSON envelope sent over WebSocket.
type Message struct {
	Type      string `json:"type"`
	AccountID string `json:"account_id"`
}

const (
	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second
	// pongWait is the time allowed to read the next pong from the peer.
	pongWait = 60 * time.Second
	// pingPeriod is how often the writer goroutine pings the peer.
	// Must be less than pongWait.
	pingPeriod = 30 * time.Second
	// sendBufSize is the capacity of the per-connection send channel.
	sendBufSize = 16
)

// connEntry wraps a WebSocket connection with a serialized send channel
// so that concurrent callers never race on WriteMessage.
type connEntry struct {
	conn *websocket.Conn
	send chan []byte
}

type Hub struct {
	mu          sync.Mutex
	connections map[uuid.UUID]*connEntry
	friends     FriendLister
}

func NewHub(friends FriendLister) *Hub {
	return &Hub{
		connections: make(map[uuid.UUID]*connEntry),
		friends:     friends,
	}
}

// Register adds a player to the presence map and notifies their online friends.
// If the account already has a connection, the old one is closed first.
func (h *Hub) Register(accountID uuid.UUID, conn *websocket.Conn) {
	entry := &connEntry{
		conn: conn,
		send: make(chan []byte, sendBufSize),
	}

	h.mu.Lock()
	if old, exists := h.connections[accountID]; exists {
		close(old.send) // signals the old writer goroutine to exit
	}
	h.connections[accountID] = entry
	h.mu.Unlock()

	// Start a dedicated writer goroutine for this connection. All
	// WriteMessage calls are funnelled through this goroutine so that
	// concurrent notifyFriends calls for the same conn never race.
	go entry.writePump()

	h.notifyFriends(accountID, "friend_online")
}

// Unregister removes a player from the presence map and notifies their
// online friends. Only deletes the map entry when the stored connection
// is the same one being unregistered — guards against a reconnect race
// where a new Register overwrites the entry before the old conn's read
// loop calls Unregister.
func (h *Hub) Unregister(accountID uuid.UUID, conn *websocket.Conn) {
	h.mu.Lock()
	if entry, exists := h.connections[accountID]; exists && entry.conn == conn {
		close(entry.send) // signals the writer goroutine to exit
		delete(h.connections, accountID)
	}
	h.mu.Unlock()

	h.notifyFriends(accountID, "friend_offline")
}

// IsOnline checks if a single user is connected.
func (h *Hub) IsOnline(accountID uuid.UUID) bool {
	h.mu.Lock()
	_, online := h.connections[accountID]
	h.mu.Unlock()
	return online
}

// BulkOnline checks which users from a list are currently connected.
func (h *Hub) BulkOnline(accountIDs []uuid.UUID) map[uuid.UUID]bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	result := make(map[uuid.UUID]bool, len(accountIDs))
	for _, id := range accountIDs {
		_, online := h.connections[id]
		result[id] = online
	}
	return result
}

// OnlineCount returns the total number of connected players.
func (h *Hub) OnlineCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.connections)
}

// notifyFriends looks up a user's friends and sends a message to each online friend.
// Writes are sent to per-connection channels, never directly via WriteMessage.
func (h *Hub) notifyFriends(accountID uuid.UUID, msgType string) {
	friendIDs, err := h.friends.ListFriendIDs(context.Background(), accountID)
	if err != nil {
		log.Printf("presence: failed to list friends for %s: %v", accountID, err)
		return
	}

	msg := Message{
		Type:      msgType,
		AccountID: accountID.String(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.Lock()
	targets := make([]*connEntry, 0, len(friendIDs))
	for _, friendID := range friendIDs {
		if entry, online := h.connections[friendID]; online {
			targets = append(targets, entry)
		}
	}
	h.mu.Unlock()

	for _, entry := range targets {
		select {
		case entry.send <- data:
		default:
			// Channel full — drop rather than block the broadcast path.
			log.Printf("presence: send channel full, dropping notify")
		}
	}
}

// writePump is the per-connection serialised write goroutine. It reads
// from entry.send and calls WriteMessage, and sends periodic pings to
// detect dead connections. It exits when entry.send is closed.
func (e *connEntry) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = e.conn.Close()
	}()

	// Reset the read deadline on pong so the reader loop's SetReadDeadline
	// call has a matching pong handler on this side.
	e.conn.SetPongHandler(func(string) error {
		return e.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		select {
		case data, ok := <-e.send:
			if !ok {
				// Channel closed — connection is being replaced or shut down.
				_ = e.conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
					time.Now().Add(writeWait))
				return
			}
			_ = e.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := e.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("presence: write error: %v", err)
				return
			}
		case <-ticker.C:
			_ = e.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := e.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
