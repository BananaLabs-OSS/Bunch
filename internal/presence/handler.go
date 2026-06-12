package presence

import (
	"net/http"
	"strings"
	"time"

	"github.com/bananalabs-oss/potassium/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Handler struct {
	hub       *Hub
	jwtSecret []byte
	upgrader  websocket.Upgrader
}

// NewHandler builds the presence handler. allowedOrigins controls the WS
// Origin allowlist: nil/empty denies all browser WS upgrades (fail closed);
// a single "*" entry allows any origin (dev only); otherwise each request's
// Origin header must exact-match (case-insensitively) one of the entries.
func NewHandler(hub *Hub, jwtSecret []byte, allowedOrigins []string) *Handler {
	allow := normalizeOrigins(allowedOrigins)
	wildcard := len(allow) == 1 && allow[0] == "*"
	return &Handler{
		hub:       hub,
		jwtSecret: jwtSecret,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				if wildcard {
					return true
				}
				origin := strings.ToLower(strings.TrimSpace(r.Header.Get("Origin")))
				if origin == "" {
					return false
				}
				for _, a := range allow {
					if a == origin {
						return true
					}
				}
				return false
			},
		},
	}
}

func normalizeOrigins(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// WebSocket handles the WS upgrade. JWT is passed as ?token= query param
// because browsers can't set headers on WebSocket connections.
func (h *Handler) WebSocket(c *gin.Context) {
	tokenStr := c.Query("token")
	if tokenStr == "" {
		c.JSON(http.StatusUnauthorized, middleware.ErrorResponse{
			Error:   "missing_token",
			Message: "token query parameter is required",
		})
		return
	}

	claims, err := middleware.ParseToken(tokenStr, h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, middleware.ErrorResponse{
			Error:   "invalid_token",
			Message: "Invalid or expired token",
		})
		return
	}

	accountID, err := uuid.Parse(claims.AccountID)
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "invalid_account",
			Message: "Invalid account ID in token",
		})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade already wrote the HTTP error
		return
	}

	h.hub.Register(accountID, conn)

	// Read loop — keeps the connection alive, handles client disconnect.
	// Sets an initial read deadline; the pong handler in writePump resets
	// it on each pong so dead connections are detected within pongWait.
	go func() {
		defer func() {
			h.hub.Unregister(accountID, conn)
			_ = conn.Close()
		}()

		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
			// Reset deadline on any inbound message (including pong frames
			// forwarded to the pong handler).
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		}
	}()
}

// GetPresence checks if a single user is online. Internal endpoint.
func (h *Handler) GetPresence(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid user ID",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account_id": userID.String(),
		"online":     h.hub.IsOnline(userID),
	})
}

// BulkPresence checks which users from a list are online. Internal endpoint.
func (h *Handler) BulkPresence(c *gin.Context) {
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

	// Convert to string keys for JSON
	presenceMap := make(map[string]bool, len(result))
	for id, online := range result {
		presenceMap[id.String()] = online
	}

	c.JSON(http.StatusOK, gin.H{"presence": presenceMap})
}

// OnlineCount returns the total connected players. Useful for health/stats.
func (h *Handler) OnlineCount(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"online_count": h.hub.OnlineCount(),
	})
}
