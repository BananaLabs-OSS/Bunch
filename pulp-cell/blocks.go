package main

import (
	pulpgin "github.com/BananaLabs-OSS/Fiber/pulp/gin"
	"github.com/BananaLabs-OSS/Fiber/pulp/gin/middleware"
	"github.com/SirNiklas9/pulp-engines/social-graph-sqlite-cell/socialowner"
	"github.com/google/uuid"
	"time"
)

type BlocksHandler struct{ friends *FriendsHandler }

func NewBlocksHandler(friends *FriendsHandler) *BlocksHandler {
	return &BlocksHandler{friends: friends}
}
func (h *BlocksHandler) BlockUser(c *pulpgin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input BlockInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(400, middleware.ErrorResponse{Error: "invalid_request", Message: "account_id is required"})
		return
	}
	var result socialowner.Result[socialowner.Ack]
	f := h.friends.call("bunch.block.v1", socialowner.BlockCommand{RequestID: commandID(c, "block"), BlockID: uuid.New(), ActorID: a, TargetID: input.AccountID, NowUnixMS: time.Now().UTC().UnixMilli()}, &result)
	if f != nil {
		socialFailure(c, f)
		return
	}
	if result.Error != nil {
		socialFailure(c, result.Error)
		return
	}
	c.JSON(201, result.Value)
}
func (h *BlocksHandler) UnblockUser(c *pulpgin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	target, err := uuid.Parse(c.Param("accountId"))
	if err != nil {
		c.JSON(400, middleware.ErrorResponse{Error: "invalid_id", Message: "Invalid account ID"})
		return
	}
	var result socialowner.Result[socialowner.Ack]
	f := h.friends.call("bunch.unblock.v1", socialowner.FriendPairCommand{RequestID: commandID(c, "unblock"), ActorID: a, FriendID: target}, &result)
	if f != nil {
		socialFailure(c, f)
		return
	}
	if result.Error != nil {
		socialFailure(c, result.Error)
		return
	}
	c.JSON(200, result.Value)
}
func (h *BlocksHandler) ListBlocked(c *pulpgin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var result socialowner.Result[socialowner.Blocks]
	f := h.friends.call("bunch.block.list.v1", socialowner.ActorRequest{ActorID: a}, &result)
	if f != nil {
		socialFailure(c, f)
		return
	}
	if result.Error != nil {
		socialFailure(c, result.Error)
		return
	}
	c.JSON(200, result.Value)
}
