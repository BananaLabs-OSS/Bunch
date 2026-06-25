package main

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	pulpgin "github.com/BananaLabs-OSS/Fiber/pulp/gin"
	"github.com/BananaLabs-OSS/Fiber/pulp/gin/middleware"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type FriendsHandler struct {
	db *bun.DB
}

func NewFriendsHandler(db *bun.DB) *FriendsHandler {
	return &FriendsHandler{db: db}
}

func (h *FriendsHandler) SendRequest(c *pulpgin.Context) {
	accountID, err := uuid.Parse(c.GetString("account_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{Error: "invalid_token", Message: "Malformed account_id in token"})
		return
	}

	var req SendRequestInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "invalid_request",
			Message: "friend_id is required",
		})
		return
	}

	if accountID == req.FriendID {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "self_friend",
			Message: "Cannot send a friend request to yourself",
		})
		return
	}

	ctx := c.Ctx()

	blocked, err := h.db.NewSelect().
		Model((*Block)(nil)).
		Where("(blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)",
			accountID, req.FriendID, req.FriendID, accountID).
		Exists(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}
	if blocked {
		c.JSON(http.StatusForbidden, middleware.ErrorResponse{
			Error:   "blocked",
			Message: "Cannot send friend request",
		})
		return
	}

	var existing Friendship
	err = h.db.NewSelect().
		Model(&existing).
		Where("(requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)",
			accountID, req.FriendID, req.FriendID, accountID).
		Scan(ctx)
	if err == nil {
		if existing.Status == StatusAccepted {
			c.JSON(http.StatusConflict, middleware.ErrorResponse{
				Error:   "already_friends",
				Message: "Already friends with this user",
			})
		} else {
			c.JSON(http.StatusConflict, middleware.ErrorResponse{
				Error:   "request_exists",
				Message: "A friend request already exists",
			})
		}
		return
	}
	if err != sql.ErrNoRows {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}

	now := time.Now().UTC()
	friendship := Friendship{
		ID:          uuid.New(),
		RequesterID: accountID,
		AddresseeID: req.FriendID,
		Status:      StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if _, err := h.db.NewInsert().Model(&friendship).Exec(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{
			Error:   "creation_failed",
			Message: "Failed to create friend request",
		})
		return
	}

	c.JSON(http.StatusCreated, friendship)
}

func (h *FriendsHandler) AcceptRequest(c *pulpgin.Context) {
	accountID, err := uuid.Parse(c.GetString("account_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{Error: "invalid_token", Message: "Malformed account_id in token"})
		return
	}

	var req HandleRequestInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "invalid_request",
			Message: "request_id is required",
		})
		return
	}

	ctx := c.Ctx()

	var friendship Friendship
	err = h.db.NewSelect().
		Model(&friendship).
		Where("id = ? AND addressee_id = ? AND status = ?", req.RequestID, accountID, StatusPending).
		Scan(ctx)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, middleware.ErrorResponse{
			Error:   "not_found",
			Message: "Friend request not found or you are not the recipient",
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}

	friendship.Status = StatusAccepted
	friendship.UpdatedAt = time.Now().UTC()

	if _, err := h.db.NewUpdate().Model(&friendship).WherePK().Exec(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "update_failed"})
		return
	}

	c.JSON(http.StatusOK, pulpgin.H{"status": "accepted"})
}

func (h *FriendsHandler) DeclineRequest(c *pulpgin.Context) {
	accountID, err := uuid.Parse(c.GetString("account_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{Error: "invalid_token", Message: "Malformed account_id in token"})
		return
	}

	var req HandleRequestInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "invalid_request",
			Message: "request_id is required",
		})
		return
	}

	ctx := c.Ctx()

	result, err := h.db.NewDelete().
		Model((*Friendship)(nil)).
		Where("id = ? AND addressee_id = ? AND status = ?", req.RequestID, accountID, StatusPending).
		Exec(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, middleware.ErrorResponse{
			Error:   "not_found",
			Message: "Friend request not found or you are not the recipient",
		})
		return
	}

	c.JSON(http.StatusOK, pulpgin.H{"status": "declined"})
}

func (h *FriendsHandler) RemoveFriend(c *pulpgin.Context) {
	accountID, err := uuid.Parse(c.GetString("account_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{Error: "invalid_token", Message: "Malformed account_id in token"})
		return
	}
	friendID, err := uuid.Parse(c.Param("friendId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{
			Error:   "invalid_id",
			Message: "Invalid friend ID",
		})
		return
	}

	ctx := c.Ctx()

	result, err := h.db.NewDelete().
		Model((*Friendship)(nil)).
		Where("((requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)) AND status = ?",
			accountID, friendID, friendID, accountID, StatusAccepted).
		Exec(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, middleware.ErrorResponse{
			Error:   "not_friends",
			Message: "Not friends with this user",
		})
		return
	}

	c.JSON(http.StatusOK, pulpgin.H{"status": "removed"})
}

func (h *FriendsHandler) ListFriends(c *pulpgin.Context) {
	accountID, err := uuid.Parse(c.GetString("account_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{Error: "invalid_token", Message: "Malformed account_id in token"})
		return
	}
	ctx := c.Ctx()

	var friendships []Friendship
	err = h.db.NewSelect().
		Model(&friendships).
		Where("(requester_id = ? OR addressee_id = ?) AND status = ?",
			accountID, accountID, StatusAccepted).
		Scan(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}

	friends := make([]Friend, 0, len(friendships))
	for _, f := range friendships {
		friendAccountID := f.AddresseeID
		if f.AddresseeID == accountID {
			friendAccountID = f.RequesterID
		}
		friends = append(friends, Friend{
			AccountID: friendAccountID,
			Since:     f.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, pulpgin.H{"friends": friends})
}

func (h *FriendsHandler) ListRequests(c *pulpgin.Context) {
	accountID, err := uuid.Parse(c.GetString("account_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, middleware.ErrorResponse{Error: "invalid_token", Message: "Malformed account_id in token"})
		return
	}
	ctx := c.Ctx()

	var incomingRows []Friendship
	if err := h.db.NewSelect().
		Model(&incomingRows).
		Where("addressee_id = ? AND status = ?", accountID, StatusPending).
		Scan(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}

	incoming := make([]FriendRequest, 0, len(incomingRows))
	for _, f := range incomingRows {
		incoming = append(incoming, FriendRequest{
			ID:            f.ID,
			FromAccountID: f.RequesterID,
			ToAccountID:   f.AddresseeID,
			CreatedAt:     f.CreatedAt,
		})
	}

	var outgoingRows []Friendship
	if err := h.db.NewSelect().
		Model(&outgoingRows).
		Where("requester_id = ? AND status = ?", accountID, StatusPending).
		Scan(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, middleware.ErrorResponse{Error: "database_error"})
		return
	}

	outgoing := make([]FriendRequest, 0, len(outgoingRows))
	for _, f := range outgoingRows {
		outgoing = append(outgoing, FriendRequest{
			ID:            f.ID,
			FromAccountID: f.RequesterID,
			ToAccountID:   f.AddresseeID,
			CreatedAt:     f.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, pulpgin.H{
		"incoming": incoming,
		"outgoing": outgoing,
	})
}

// RemoveFriendship is called internally by the blocks handler.
func (h *FriendsHandler) RemoveFriendship(ctx context.Context, accountA, accountB uuid.UUID) error {
	_, err := h.db.NewDelete().
		Model((*Friendship)(nil)).
		Where("(requester_id = ? AND addressee_id = ?) OR (requester_id = ? AND addressee_id = ?)",
			accountA, accountB, accountB, accountA).
		Exec(ctx)
	return err
}

// ListFriendIDs is used by the presence hub to know who to notify.
func (h *FriendsHandler) ListFriendIDs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error) {
	var friendships []Friendship
	err := h.db.NewSelect().
		Model(&friendships).
		Where("(requester_id = ? OR addressee_id = ?) AND status = ?",
			accountID, accountID, StatusAccepted).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	ids := make([]uuid.UUID, 0, len(friendships))
	for _, f := range friendships {
		if f.AddresseeID == accountID {
			ids = append(ids, f.RequesterID)
		} else {
			ids = append(ids, f.AddresseeID)
		}
	}
	return ids, nil
}
