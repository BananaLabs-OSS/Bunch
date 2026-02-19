package friends

import (
	"context"
	"net/http"
	"time"

	"github.com/bananalabs-oss/bunch/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type Handler struct {
	db *bun.DB
}

func NewHandler(db *bun.DB) *Handler {
	return &Handler{db: db}
}

func (h *Handler) SendRequest(c *gin.Context) {
	accountID := uuid.MustParse(c.GetString("account_id"))

	var req models.SendRequestInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "invalid_request",
			Message: "friend_id is required",
		})
		return
	}

	if accountID == req.FriendID {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "self_friend",
			Message: "Cannot send a friend request to yourself",
		})
		return
	}

	ctx := c.Request.Context()

	// Check if either user has blocked the other.
	blocked, err := h.db.NewSelect().
		Model((*models.Block)(nil)).
		Where("(blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)",
			accountID, req.FriendID, req.FriendID, accountID).
		Exists(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "database_error"})
		return
	}
	if blocked {
		c.JSON(http.StatusForbidden, models.ErrorResponse{
			Error:   "blocked",
			Message: "Cannot send friend request",
		})
		return
	}

	// Check for existing relationship.
	var existing models.Friendship
	err = h.db.NewSelect().
		Model(&existing).
		Where("(requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)",
			accountID, req.FriendID, req.FriendID, accountID).
		Scan(ctx)
	if err == nil {
		if existing.Status == models.StatusAccepted {
			c.JSON(http.StatusConflict, models.ErrorResponse{
				Error:   "already_friends",
				Message: "Already friends with this user",
			})
		} else {
			c.JSON(http.StatusConflict, models.ErrorResponse{
				Error:   "request_exists",
				Message: "A friend request already exists",
			})
		}
		return
	}

	now := time.Now().UTC()
	friendship := models.Friendship{
		ID:          uuid.New(),
		RequesterID: accountID,
		AddresseeID: req.FriendID,
		Status:      models.StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if _, err := h.db.NewInsert().Model(&friendship).Exec(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{
			Error:   "creation_failed",
			Message: "Failed to create friend request",
		})
		return
	}

	c.JSON(http.StatusCreated, friendship)
}

func (h *Handler) AcceptRequest(c *gin.Context) {
	accountID := uuid.MustParse(c.GetString("account_id"))

	var req models.HandleRequestInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "invalid_request",
			Message: "request_id is required",
		})
		return
	}

	ctx := c.Request.Context()

	var friendship models.Friendship
	err := h.db.NewSelect().
		Model(&friendship).
		Where("id = ? AND addressee_id = ? AND status = ?", req.RequestID, accountID, models.StatusPending).
		Scan(ctx)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "not_found",
			Message: "Friend request not found or you are not the recipient",
		})
		return
	}

	friendship.Status = models.StatusAccepted
	friendship.UpdatedAt = time.Now().UTC()

	if _, err := h.db.NewUpdate().Model(&friendship).WherePK().Exec(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "update_failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "accepted"})
}

func (h *Handler) DeclineRequest(c *gin.Context) {
	accountID := uuid.MustParse(c.GetString("account_id"))

	var req models.HandleRequestInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{
			Error:   "invalid_request",
			Message: "request_id is required",
		})
		return
	}

	ctx := c.Request.Context()

	result, err := h.db.NewDelete().
		Model((*models.Friendship)(nil)).
		Where("id = ? AND addressee_id = ? AND status = ?", req.RequestID, accountID, models.StatusPending).
		Exec(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "database_error"})
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "not_found",
			Message: "Friend request not found or you are not the recipient",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "declined"})
}

func (h *Handler) RemoveFriend(c *gin.Context) {
	accountID := uuid.MustParse(c.GetString("account_id"))
	friendID := uuid.MustParse(c.Param("friendId"))

	ctx := c.Request.Context()

	result, err := h.db.NewDelete().
		Model((*models.Friendship)(nil)).
		Where("((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)) AND status = ?",
			accountID, friendID, friendID, accountID, models.StatusAccepted).
		Exec(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "database_error"})
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, models.ErrorResponse{
			Error:   "not_friends",
			Message: "Not friends with this user",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "removed"})
}

func (h *Handler) ListFriends(c *gin.Context) {
	accountID := uuid.MustParse(c.GetString("account_id"))
	ctx := c.Request.Context()

	var friendships []models.Friendship
	err := h.db.NewSelect().
		Model(&friendships).
		Where("(requester_id = ? OR addressee_id = ?) AND status = ?",
			accountID, accountID, models.StatusAccepted).
		Scan(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "database_error"})
		return
	}

	friends := make([]models.Friend, 0, len(friendships))
	for _, f := range friendships {
		friendAccountID := f.AddresseeID
		if f.AddresseeID == accountID {
			friendAccountID = f.RequesterID
		}
		friends = append(friends, models.Friend{
			AccountID: friendAccountID,
			Since:     f.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"friends": friends})
}

func (h *Handler) ListRequests(c *gin.Context) {
	accountID := uuid.MustParse(c.GetString("account_id"))
	ctx := c.Request.Context()

	// Incoming
	var incomingRows []models.Friendship
	if err := h.db.NewSelect().
		Model(&incomingRows).
		Where("addressee_id = ? AND status = ?", accountID, models.StatusPending).
		Scan(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "database_error"})
		return
	}

	incoming := make([]models.FriendRequest, 0, len(incomingRows))
	for _, f := range incomingRows {
		incoming = append(incoming, models.FriendRequest{
			ID:            f.ID,
			FromAccountID: f.RequesterID,
			ToAccountID:   f.AddresseeID,
			CreatedAt:     f.CreatedAt,
		})
	}

	// Outgoing
	var outgoingRows []models.Friendship
	if err := h.db.NewSelect().
		Model(&outgoingRows).
		Where("requester_id = ? AND status = ?", accountID, models.StatusPending).
		Scan(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Error: "database_error"})
		return
	}

	outgoing := make([]models.FriendRequest, 0, len(outgoingRows))
	for _, f := range outgoingRows {
		outgoing = append(outgoing, models.FriendRequest{
			ID:            f.ID,
			FromAccountID: f.RequesterID,
			ToAccountID:   f.AddresseeID,
			CreatedAt:     f.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"incoming": incoming,
		"outgoing": outgoing,
	})
}

// RemoveFriendship is called internally by the blocks handler.
// It removes any friendship between two users regardless of direction.
func (h *Handler) RemoveFriendship(ctx context.Context, accountA, accountB uuid.UUID) error {
	_, err := h.db.NewDelete().
		Model((*models.Friendship)(nil)).
		Where("(requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)",
			accountA, accountB, accountB, accountA).
		Exec(ctx)
	return err
}
