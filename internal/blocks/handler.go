package blocks

import (
	"context"
	"net/http"
	"time"

	"github.com/bananalabs-oss/bunch/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// FriendshipRemover is implemented by friends.Handler.
type FriendshipRemover interface {
	RemoveFriendship(ctx context.Context, accountA, accountB uuid.UUID) error
}

type Handler struct {
	db      *bun.DB
	friends FriendshipRemover
}

func NewHandler(db *bun.DB, friends FriendshipRemover) *Handler {
	return &Handler{db: db, friends: friends}
}

func (h *Handler) BlockUser(c *gin.Context) {
	blockerID := uuid.MustParse(c.GetString("account_id"))

	var req models.BlockInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "invalid_request",
			Message: "account_id is required",
		})
		return
	}

	if blockerID == req.AccountID {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "self_block",
			Message: "Cannot block yourself",
		})
		return
	}

	ctx := c.Request.Context()

	// Check if already blocked.
	exists, err := h.db.NewSelect().
		Model((*models.Block)(nil)).
		Where("blocker_id = ? AND blocked_id = ?", blockerID, req.AccountID).
		Exists(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "database_error"})
		return
	}
	if exists {
		c.JSON(http.StatusConflict, models.ErrorResponse{
			Error:   "already_blocked",
			Message: "User already blocked",
		})
		return
	}

	block := models.Block{
		ID:        uuid.New(),
		BlockerID: blockerID,
		BlockedID: req.AccountID,
		CreatedAt: time.Now().UTC(),
	}

	if _, err := h.db.NewInsert().Model(&block).Exec(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "creation_failed"})
		return
	}

	// Also remove any existing friendship.
	_ = h.friends.RemoveFriendship(ctx, blockerID, req.AccountID)

	c.JSON(http.StatusCreated, gin.H{"status": "blocked"})
}

func (h *Handler) UnblockUser(c *gin.Context) {
	blockerID := uuid.MustParse(c.GetString("account_id"))
	blockedID := uuid.MustParse(c.Param("accountId"))

	ctx := c.Request.Context()

	result, err := h.db.NewDelete().
		Model((*models.Block)(nil)).
		Where("blocker_id = ? AND blocked_id = ?", blockerID, blockedID).
		Exec(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "database_error"})
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "not_found",
			Message: "Block not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "unblocked"})
}

func (h *Handler) ListBlocked(c *gin.Context) {
	blockerID := uuid.MustParse(c.GetString("account_id"))
	ctx := c.Request.Context()

	var blockRows []models.Block
	err := h.db.NewSelect().
		Model(&blockRows).
		Where("blocker_id = ?", blockerID).
		Scan(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "database_error"})
		return
	}

	blocked := make([]models.BlockedUser, 0, len(blockRows))
	for _, b := range blockRows {
		blocked = append(blocked, models.BlockedUser{
			AccountID: b.BlockedID,
			Since:     b.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"blocks": blocked})
}
