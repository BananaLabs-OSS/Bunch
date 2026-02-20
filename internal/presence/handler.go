package presence

import (
	"net/http"

	"github.com/bananalabs-oss/potassium/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for now; lock down in production
	},
}

type Handler struct {
	hub       *Hub
	jwtSecret []byte
}

func NewHandler(hub *Hub, jwtSecret []byte) *Handler {
	return &Handler{hub: hub, jwtSecret: jwtSecret}
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

	accountID := uuid.MustParse(claims.AccountID)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade already wrote the HTTP error
		return
	}

	h.hub.Register(accountID, conn)

	// Read loop — keeps the connection alive, handles client disconnect.
	// We don't expect meaningful messages from the client yet.
	go func() {
		defer func() {
			h.hub.Unregister(accountID)
			_ = conn.Close()
		}()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
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
