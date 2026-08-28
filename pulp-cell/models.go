package main

import (
	"github.com/SirNiklas9/pulp-engines/social-graph-sqlite-cell/socialowner"

	"github.com/google/uuid"
)

type FriendshipStatus = socialowner.FriendshipStatus

const (
	StatusPending  = socialowner.StatusPending
	StatusAccepted = socialowner.StatusAccepted
)

type Friendship = socialowner.Friendship
type Block = socialowner.Block
type Friend = socialowner.Friend
type FriendRequest = socialowner.FriendRequest
type BlockedUser = socialowner.BlockedUser

type SendRequestInput struct {
	FriendID uuid.UUID `json:"friend_id" binding:"required"`
}

type HandleRequestInput struct {
	RequestID uuid.UUID `json:"request_id" binding:"required"`
}

type BlockInput struct {
	AccountID uuid.UUID `json:"account_id" binding:"required"`
}
