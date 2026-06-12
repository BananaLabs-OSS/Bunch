package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/BananaLabs-OSS/Fiber/pulp"
	pulpgin "github.com/BananaLabs-OSS/Fiber/pulp/gin"
	"github.com/BananaLabs-OSS/Fiber/pulp/gin/middleware"
	"github.com/google/uuid"
)

// FriendLister returns the account IDs of a user's accepted friends.
type FriendLister interface {
	ListFriendIDs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error)
}

// PresenceMessage is the JSON envelope sent over WebSocket.
type PresenceMessage struct {
	Type      string `json:"type"`
	AccountID string `json:"account_id"`
}

// Hub tracks connected accounts and broadcasts presence changes to
// their friends. Runs inside the cell, owns no goroutines (all
// work happens on step callbacks) — WS conns are identified by the
// host-assigned connID instead of a *websocket.Conn.
type Hub struct {
	mu sync.Mutex

	// accountToConn maps an authenticated accountID to the connID the
	// host assigned when it opened the WebSocket. A single account
	// can only have one conn at a time; re-registration closes the
	// previous one.
	accountToConn map[uuid.UUID]uint64

	friends FriendLister
}

func NewHub(friends FriendLister) *Hub {
	return &Hub{
		accountToConn: map[uuid.UUID]uint64{},
		friends:       friends,
	}
}

// Register adds an account to the presence map. If the account
// already has a connection registered, the old one is closed first.
func (h *Hub) Register(accountID uuid.UUID, connID uint64) {
	h.mu.Lock()
	if oldConn, exists := h.accountToConn[accountID]; exists && oldConn != connID {
		_ = pulp.WS.Close(pulp.WSCloseRequest{
			ConnID: oldConn,
			Code:   1000,
			Reason: "reconnected",
		})
	}
	h.accountToConn[accountID] = connID
	h.mu.Unlock()

	h.notifyFriends(accountID, "friend_online")
}

// Unregister removes an account from the presence map when its
// WebSocket closes. Only deletes the map entry when the stored connID
// matches the one being unregistered — guards against a reconnect race
// where a new Register overwrites the entry before the old conn's close
// event fires.
func (h *Hub) Unregister(accountID uuid.UUID, connID uint64) {
	h.mu.Lock()
	if stored, exists := h.accountToConn[accountID]; exists && stored == connID {
		delete(h.accountToConn, accountID)
	}
	h.mu.Unlock()

	h.notifyFriends(accountID, "friend_offline")
}

// IsOnline reports whether accountID has an active WebSocket.
func (h *Hub) IsOnline(accountID uuid.UUID) bool {
	h.mu.Lock()
	_, online := h.accountToConn[accountID]
	h.mu.Unlock()
	return online
}

// BulkOnline tests a batch of account IDs in one lock acquisition.
func (h *Hub) BulkOnline(accountIDs []uuid.UUID) map[uuid.UUID]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make(map[uuid.UUID]bool, len(accountIDs))
	for _, id := range accountIDs {
		_, online := h.accountToConn[id]
		result[id] = online
	}
	return result
}

// OnlineCount returns the total number of connected accounts.
func (h *Hub) OnlineCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.accountToConn)
}

// notifyFriends looks up the user's friends and sends a presence
// message to each online one via the host's ws_send import.
func (h *Hub) notifyFriends(accountID uuid.UUID, msgType string) {
	friendIDs, err := h.friends.ListFriendIDs(context.Background(), accountID)
	if err != nil {
		// Parity with native Bunch/internal/presence/hub.go:91.
		log.Printf("presence: failed to list friends for %s: %v", accountID, err)
		return
	}

	msg := PresenceMessage{Type: msgType, AccountID: accountID.String()}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	type target struct {
		friendID uuid.UUID
		connID   uint64
	}
	h.mu.Lock()
	targets := make([]target, 0, len(friendIDs))
	for _, friendID := range friendIDs {
		if connID, online := h.accountToConn[friendID]; online {
			targets = append(targets, target{friendID: friendID, connID: connID})
		}
	}
	h.mu.Unlock()

	for _, t := range targets {
		if err := pulp.WS.Send(pulp.WSSendRequest{
			ConnID:  t.connID,
			OpCode:  pulp.WSOpCodeText,
			Payload: data,
		}); err != nil {
			// Parity with native Bunch/internal/presence/hub.go:111.
			log.Printf("presence: failed to notify %s: %v", t.friendID, err)
		}
	}
}

// PresenceHandler exposes HTTP endpoints for presence queries and
// wires the WebSocket upgrade. Mirrors the original Bunch handler.
type PresenceHandler struct {
	hub       *Hub
	jwtSecret []byte
}

func NewPresenceHandler(hub *Hub, jwtSecret []byte) *PresenceHandler {
	return &PresenceHandler{hub: hub, jwtSecret: jwtSecret}
}

// WSHandlers returns the event callbacks pulpgin will install on the
// /ws route. Auth happens in OnOpen via the token query param — the
// host has already accepted the upgrade by this point, so we either
// Register the account or Close with a policy-violation code.
func (h *PresenceHandler) WSHandlers() pulpgin.WSHandlers {
	return pulpgin.WSHandlers{
		OnOpen: func(c *pulpgin.WSContext) {
			tokenStr := c.Query["token"]
			if tokenStr == "" {
				_ = c.Close(1008, "missing token")
				return
			}
			claims, err := middleware.ParseToken(tokenStr, h.jwtSecret)
			if err != nil {
				_ = c.Close(1008, "invalid token")
				return
			}
			accountID, err := uuid.Parse(claims.AccountID)
			if err != nil {
				_ = c.Close(1008, "invalid account")
				return
			}
			c.Keys["account_id"] = accountID
			h.hub.Register(accountID, c.ConnID)
		},
		OnFrame: func(c *pulpgin.WSContext) {
			// Bunch doesn't expect meaningful inbound frames; keep the
			// connection alive but ignore payload contents.
		},
		OnClose: func(c *pulpgin.WSContext) {
			raw, ok := c.Keys["account_id"]
			if !ok {
				return
			}
			accountID, ok := raw.(uuid.UUID)
			if !ok {
				return
			}
			h.hub.Unregister(accountID, c.ConnID)
		},
	}
}

func (h *PresenceHandler) GetPresence(c *pulpgin.Context) {
	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid user ID",
		})
		return
	}
	c.JSON(http.StatusOK, pulpgin.H{
		"account_id": userID.String(),
		"online":     h.hub.IsOnline(userID),
	})
}

func (h *PresenceHandler) BulkPresence(c *pulpgin.Context) {
	var req struct {
		AccountIDs []uuid.UUID `json:"account_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "invalid_request",
			Message: "account_ids is required",
		})
		return
	}

	result := h.hub.BulkOnline(req.AccountIDs)
	presenceMap := make(map[string]bool, len(result))
	for id, online := range result {
		presenceMap[id.String()] = online
	}
	c.JSON(http.StatusOK, pulpgin.H{"presence": presenceMap})
}

func (h *PresenceHandler) OnlineCount(c *pulpgin.Context) {
	c.JSON(http.StatusOK, pulpgin.H{"online_count": h.hub.OnlineCount()})
}
