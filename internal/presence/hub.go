package presence

import (
	"context"
	"encoding/json"
	"log"
	"sync"

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

type Hub struct {
	mu          sync.RWMutex
	connections map[uuid.UUID]*websocket.Conn
	friends     FriendLister
}

func NewHub(friends FriendLister) *Hub {
	return &Hub{
		connections: make(map[uuid.UUID]*websocket.Conn),
		friends:     friends,
	}
}

// Register adds a player to the presence map and notifies their online friends.
func (h *Hub) Register(accountID uuid.UUID, conn *websocket.Conn) {
	h.mu.Lock()
	// Close existing connection if reconnecting
	if old, exists := h.connections[accountID]; exists {
		_ = old.Close()
	}
	h.connections[accountID] = conn
	h.mu.Unlock()

	h.notifyFriends(accountID, "friend_online")
}

// Unregister removes a player from the presence map and notifies their online friends.
func (h *Hub) Unregister(accountID uuid.UUID) {
	h.mu.Lock()
	delete(h.connections, accountID)
	h.mu.Unlock()

	h.notifyFriends(accountID, "friend_offline")
}

// IsOnline checks if a single user is connected.
func (h *Hub) IsOnline(accountID uuid.UUID) bool {
	h.mu.RLock()
	_, online := h.connections[accountID]
	h.mu.RUnlock()
	return online
}

// BulkOnline checks which users from a list are currently connected.
func (h *Hub) BulkOnline(accountIDs []uuid.UUID) map[uuid.UUID]bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make(map[uuid.UUID]bool, len(accountIDs))
	for _, id := range accountIDs {
		_, online := h.connections[id]
		result[id] = online
	}
	return result
}

// OnlineCount returns the total number of connected players.
func (h *Hub) OnlineCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}

// notifyFriends looks up a user's friends and sends a message to each online friend.
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

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, friendID := range friendIDs {
		if conn, online := h.connections[friendID]; online {
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("presence: failed to notify %s: %v", friendID, err)
			}
		}
	}
}
