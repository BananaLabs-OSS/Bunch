package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	pulpgin "github.com/BananaLabs-OSS/Fiber/pulp/gin"
	"github.com/BananaLabs-OSS/Fiber/pulp/gin/middleware"
	"github.com/BananaLabs-OSS/Fiber/pulp/workflow"
	"github.com/SirNiklas9/pulp-engines/social-graph-sqlite-cell/socialowner"
	"github.com/google/uuid"
	"github.com/vmihailenco/msgpack/v5"
)

type FriendsHandler struct{ client *workflow.Client }

func NewFriendsHandler(client *workflow.Client) *FriendsHandler {
	return &FriendsHandler{client: client}
}

type socialReply struct {
	ResponseMsgpack []byte `msgpack:"response_msgpack"`
}

func (h *FriendsHandler) call(event string, request any, output any) *socialowner.Error {
	wire, err := msgpack.Marshal(request)
	if err != nil {
		return &socialowner.Error{Code: "unavailable", Message: err.Error()}
	}
	result, err := h.client.Dispatch(workflow.DispatchRequest{Event: event, Payload: map[string]any{"request_msgpack": wire}})
	if err != nil {
		return &socialowner.Error{Code: "unavailable", Message: err.Error()}
	}
	reply, err := workflow.DecodeValue[socialReply](result)
	if err != nil {
		return &socialowner.Error{Code: "unavailable", Message: err.Error()}
	}
	if err := msgpack.Unmarshal(reply.ResponseMsgpack, output); err != nil {
		return &socialowner.Error{Code: "unavailable", Message: err.Error()}
	}
	return nil
}

func actor(c *pulpgin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.GetString("account_id"))
	if err != nil {
		c.JSON(400, middleware.ErrorResponse{Error: "invalid_token", Message: "Malformed account_id in token"})
		return uuid.Nil, false
	}
	return id, true
}

func commandID(c *pulpgin.Context, operation string) string {
	return fmt.Sprintf("%s:http-%d", operation, c.Request().ID)
}

func socialFailure(c *pulpgin.Context, failure *socialowner.Error) {
	status := http.StatusConflict
	if failure.Code == "invalid_request" || failure.Code == "self_friend" || failure.Code == "self_block" {
		status = 400
	}
	if failure.Code == "blocked" {
		status = 403
	}
	if failure.Code == "not_found" || failure.Code == "not_friends" {
		status = 404
	}
	if failure.Code == "unavailable" {
		status = 500
	}
	c.JSON(status, middleware.ErrorResponse{Error: failure.Code, Message: failure.Message})
}

func (h *FriendsHandler) SendRequest(c *pulpgin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input SendRequestInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(400, middleware.ErrorResponse{Error: "invalid_request", Message: "friend_id is required"})
		return
	}
	var result socialowner.Result[socialowner.Friendship]
	err := h.call("bunch.friend.request.v1", socialowner.FriendRequestCommand{RequestID: commandID(c, "friend-request"), FriendshipID: uuid.New(), ActorID: a, FriendID: input.FriendID, NowUnixMS: time.Now().UTC().UnixMilli()}, &result)
	if err != nil {
		socialFailure(c, err)
		return
	}
	if result.Error != nil {
		socialFailure(c, result.Error)
		return
	}
	c.JSON(201, result.Value)
}

func (h *FriendsHandler) AcceptRequest(c *pulpgin.Context) {
	h.decision(c, "bunch.friend.accept.v1", "accept")
}
func (h *FriendsHandler) DeclineRequest(c *pulpgin.Context) {
	h.decision(c, "bunch.friend.decline.v1", "decline")
}
func (h *FriendsHandler) decision(c *pulpgin.Context, event, operation string) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var input HandleRequestInput
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(400, middleware.ErrorResponse{Error: "invalid_request", Message: "request_id is required"})
		return
	}
	var result socialowner.Result[socialowner.Ack]
	err := h.call(event, socialowner.RequestDecisionCommand{RequestID: commandID(c, operation), ActorID: a, FriendshipID: input.RequestID, NowUnixMS: time.Now().UTC().UnixMilli()}, &result)
	if err != nil {
		socialFailure(c, err)
		return
	}
	if result.Error != nil {
		socialFailure(c, result.Error)
		return
	}
	c.JSON(200, result.Value)
}

func (h *FriendsHandler) RemoveFriend(c *pulpgin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	friend, err := uuid.Parse(c.Param("friendId"))
	if err != nil {
		c.JSON(400, middleware.ErrorResponse{Error: "invalid_id", Message: "Invalid friend ID"})
		return
	}
	var result socialowner.Result[socialowner.Ack]
	failure := h.call("bunch.friend.remove.v1", socialowner.FriendPairCommand{RequestID: commandID(c, "remove"), ActorID: a, FriendID: friend}, &result)
	if failure != nil {
		socialFailure(c, failure)
		return
	}
	if result.Error != nil {
		socialFailure(c, result.Error)
		return
	}
	c.JSON(200, result.Value)
}

func (h *FriendsHandler) ListFriends(c *pulpgin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var result socialowner.Result[socialowner.Friends]
	failure := h.call("bunch.friend.list.v1", socialowner.ActorRequest{ActorID: a}, &result)
	if failure != nil {
		socialFailure(c, failure)
		return
	}
	if result.Error != nil {
		socialFailure(c, result.Error)
		return
	}
	c.JSON(200, result.Value)
}
func (h *FriendsHandler) ListRequests(c *pulpgin.Context) {
	a, ok := actor(c)
	if !ok {
		return
	}
	var result socialowner.Result[socialowner.Requests]
	failure := h.call("bunch.request.list.v1", socialowner.ActorRequest{ActorID: a}, &result)
	if failure != nil {
		socialFailure(c, failure)
		return
	}
	if result.Error != nil {
		socialFailure(c, result.Error)
		return
	}
	c.JSON(200, result.Value)
}
func (h *FriendsHandler) ListFriendIDs(_ context.Context, accountID uuid.UUID) ([]uuid.UUID, error) {
	var result socialowner.Result[socialowner.FriendIDs]
	if failure := h.call("bunch.friend.ids.v1", socialowner.ActorRequest{ActorID: accountID}, &result); failure != nil {
		return nil, fmt.Errorf("%s", failure.Message)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("%s", result.Error.Message)
	}
	return result.Value.AccountIDs, nil
}
