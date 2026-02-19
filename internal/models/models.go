package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// --- Database Models ---

type FriendshipStatus string

const (
	StatusPending  FriendshipStatus = "pending"
	StatusAccepted FriendshipStatus = "accepted"
)

type Friendship struct {
	bun.BaseModel `bun:"table:friendships,alias:f"`

	ID          uuid.UUID        `bun:"id,pk,type:uuid" json:"id"`
	RequesterID uuid.UUID        `bun:"requester_id,notnull,type:uuid" json:"requester_id"`
	AddresseeID uuid.UUID        `bun:"addressee_id,notnull,type:uuid" json:"addressee_id"`
	Status      FriendshipStatus `bun:"status,notnull" json:"status"`
	CreatedAt   time.Time        `bun:"created_at,nullzero,notnull" json:"created_at"`
	UpdatedAt   time.Time        `bun:"updated_at,nullzero,notnull" json:"updated_at"`
}

type Block struct {
	bun.BaseModel `bun:"table:blocks,alias:b"`

	ID        uuid.UUID `bun:"id,pk,type:uuid" json:"id"`
	BlockerID uuid.UUID `bun:"blocker_id,notnull,type:uuid" json:"blocker_id"`
	BlockedID uuid.UUID `bun:"blocked_id,notnull,type:uuid" json:"blocked_id"`
	CreatedAt time.Time `bun:"created_at,nullzero,notnull" json:"created_at"`
}

// --- API Response Types ---

// Friend is the client-facing view — the "other person" in the relationship.
type Friend struct {
	AccountID uuid.UUID `json:"account_id"`
	Since     time.Time `json:"since"`
}

// FriendRequest is a pending friendship.
type FriendRequest struct {
	ID            uuid.UUID `json:"id"`
	FromAccountID uuid.UUID `json:"from_account_id"`
	ToAccountID   uuid.UUID `json:"to_account_id"`
	CreatedAt     time.Time `json:"created_at"`
}

// BlockedUser is the client-facing view of a block.
type BlockedUser struct {
	AccountID uuid.UUID `json:"account_id"`
	Since     time.Time `json:"since"`
}

// --- Request Types ---

type SendRequestInput struct {
	FriendID uuid.UUID `json:"friend_id" binding:"required"`
}

type HandleRequestInput struct {
	RequestID uuid.UUID `json:"request_id" binding:"required"`
}

type BlockInput struct {
	AccountID uuid.UUID `json:"account_id" binding:"required"`
}

// ErrorResponse matches BananAuth's error format.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}
