package main

import (
	"context"
	"net/http"
	"time"

	pulpgin "github.com/BananaLabs-OSS/Fiber/pulp/gin"
	"github.com/BananaLabs-OSS/Fiber/pulp/gin/middleware"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// FriendshipRemover is implemented by FriendsHandler.
type FriendshipRemover interface {
	RemoveFriendship(ctx context.Context, accountA, accountB uuid.UUID) error
}

type BlocksHandler struct {
	db      *bun.DB
	friends FriendshipRemover
}

func NewBlocksHandler(db *bun.DB, friends FriendshipRemover) *BlocksHandler {
	return &BlocksHandler{db: db, friends: friends}
}

func (h *BlocksHandler) BlockUser(c *pulpgin.Context) {
	blockerID := uuid.MustParse(c.GetString("account_id"))

	var req BlockInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "invalid_request",
			Message: "account_id is required",
		})
		return
	}

	if blockerID == req.AccountID {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "self_block",
			Message: "Cannot block yourself",
		})
		return
	}

	ctx := c.Ctx()

	exists, err := h.db.NewSelect().
		Model((*Block)(nil)).
		Where("blocker_id = ? AND blocked_id = ?", blockerID, req.AccountID).
		Exists(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}
	if exists {
		c.JSON(http.StatusConflict, middleware.ErrorResponse{
			Error:   "already_blocked",
			Message: "User already blocked",
		})
		return
	}

	block := Block{
		ID:        uuid.New(),
		BlockerID: blockerID,
		BlockedID: req.AccountID,
		CreatedAt: time.Now().UTC(),
	}

	if _, err := h.db.NewInsert().Model(&block).Exec(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "creation_failed"})
		return
	}

	_ = h.friends.RemoveFriendship(ctx, blockerID, req.AccountID)

	c.JSON(http.StatusCreated, pulpgin.H{"status": "blocked"})
}

func (h *BlocksHandler) UnblockUser(c *pulpgin.Context) {
	blockerID := uuid.MustParse(c.GetString("account_id"))
	blockedID, err := uuid.Parse(c.Param("accountId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid account ID",
		})
		return
	}

	ctx := c.Ctx()

	result, err := h.db.NewDelete().
		Model((*Block)(nil)).
		Where("blocker_id = ? AND blocked_id = ?", blockerID, blockedID).
		Exec(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, middleware.ErrorResponse{
			Error:   "not_found",
			Message: "Block not found",
		})
		return
	}

	c.JSON(http.StatusOK, pulpgin.H{"status": "unblocked"})
}

func (h *BlocksHandler) ListBlocked(c *pulpgin.Context) {
	blockerID := uuid.MustParse(c.GetString("account_id"))
	ctx := c.Ctx()

	var blockRows []Block
	err := h.db.NewSelect().
		Model(&blockRows).
		Where("blocker_id = ?", blockerID).
		Scan(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}

	blocked := make([]BlockedUser, 0, len(blockRows))
	for _, b := range blockRows {
		blocked = append(blocked, BlockedUser{
			AccountID: b.BlockedID,
			Since:     b.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, pulpgin.H{"blocks": blocked})
}
