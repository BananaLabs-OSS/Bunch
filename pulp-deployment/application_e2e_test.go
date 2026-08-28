package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/BananaLabs-OSS/Pulp/run"
	"github.com/google/uuid"
	"github.com/vmihailenco/msgpack/v5"
)

type socialProviderObserver struct {
	ready chan run.ApplicationProviderAccess
}

func (o *socialProviderObserver) AfterApplicationStart(context.Context, run.ApplicationIdentity) error {
	return nil
}
func (o *socialProviderObserver) AfterApplicationStartWithProvider(_ context.Context, _ run.ApplicationIdentity, access run.ApplicationProviderAccess) error {
	o.ready <- access
	return nil
}
func (o *socialProviderObserver) BeforeApplicationShutdown(context.Context, run.ApplicationIdentity) error {
	return nil
}

type socialFriendRequest struct {
	RequestID    string    `msgpack:"request_id"`
	FriendshipID uuid.UUID `msgpack:"friendship_id"`
	ActorID      uuid.UUID `msgpack:"actor_id"`
	FriendID     uuid.UUID `msgpack:"friend_id"`
	NowUnixMS    int64     `msgpack:"now_unix_ms"`
}

type socialDecision struct {
	RequestID    string    `msgpack:"request_id"`
	ActorID      uuid.UUID `msgpack:"actor_id"`
	FriendshipID uuid.UUID `msgpack:"friendship_id"`
	NowUnixMS    int64     `msgpack:"now_unix_ms"`
}

type socialActor struct {
	ActorID uuid.UUID `msgpack:"actor_id"`
}

type socialError struct {
	Code string `msgpack:"code"`
}

type socialFriend struct {
	AccountID uuid.UUID `msgpack:"account_id"`
}

type socialFriends struct {
	Friends []socialFriend `msgpack:"friends"`
}

type socialFriendship struct {
	ID uuid.UUID `msgpack:"id"`
}

type socialAck struct {
	Status string `msgpack:"status"`
}

type socialResult[T any] struct {
	OK    bool         `msgpack:"ok"`
	Value T            `msgpack:"value,omitempty"`
	Error *socialError `msgpack:"error,omitempty"`
}

type socialDispatchRequest struct {
	Event   string         `msgpack:"event"`
	Payload map[string]any `msgpack:"payload,omitempty"`
}

type socialDispatchResult struct {
	Value any `msgpack:"value,omitempty"`
}

func TestSocialOwnerRealPulpLuaAndIdempotency(t *testing.T) {
	t.Setenv("HTTP_PORT", "0")
	buildSocialArtifacts(t)

	observer := &socialProviderObserver{ready: make(chan run.ApplicationProviderAccess, 1)}
	runtime, err := run.NewDirectApplicationRuntime(
		filepath.Clean(filepath.Join("..", "application", "pulp.app.toml")),
		run.DirectApplicationOptions{StorageRoot: t.TempDir(), Lifecycle: observer},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := runtime.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown Bunch application: %v", err)
		}
	})
	access := <-observer.ready

	alice := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	bob := uuid.MustParse("20000000-0000-0000-0000-000000000002")
	friendshipID := uuid.MustParse("30000000-0000-0000-0000-000000000003")
	request := socialFriendRequest{
		RequestID:    "request-1",
		FriendshipID: friendshipID,
		ActorID:      alice,
		FriendID:     bob,
		NowUnixMS:    time.Unix(1_800_000_000, 0).UnixMilli(),
	}
	first := callSocial[socialResult[socialFriendship]](t, ctx, access, "bunch.friend.request.v1", request)
	if !first.OK || first.Error != nil || first.Value.ID != friendshipID {
		t.Fatalf("friend request = %#v", first)
	}
	replayed := callSocial[socialResult[socialFriendship]](t, ctx, access, "bunch.friend.request.v1", request)
	if !replayed.OK || replayed.Value.ID != friendshipID {
		t.Fatalf("friend replay = %#v", replayed)
	}
	conflict := request
	conflict.FriendID = uuid.MustParse("40000000-0000-0000-0000-000000000004")
	rejected := callSocial[socialResult[socialFriendship]](t, ctx, access, "bunch.friend.request.v1", conflict)
	if rejected.Error == nil || rejected.Error.Code != "idempotency_conflict" {
		t.Fatalf("conflicting replay = %#v", rejected)
	}
	accepted := callSocial[socialResult[socialAck]](t, ctx, access, "bunch.friend.accept.v1", socialDecision{
		RequestID:    "accept-1",
		ActorID:      bob,
		FriendshipID: friendshipID,
		NowUnixMS:    request.NowUnixMS + 1,
	})
	if !accepted.OK || accepted.Value.Status != "accepted" {
		t.Fatalf("accept = %#v", accepted)
	}
	friends := callSocial[socialResult[socialFriends]](t, ctx, access, "bunch.friend.list.v1", socialActor{ActorID: alice})
	if !friends.OK || len(friends.Value.Friends) != 1 || friends.Value.Friends[0].AccountID != bob {
		t.Fatalf("friend projection = %#v", friends)
	}
}

func buildSocialArtifacts(t *testing.T) {
	t.Helper()
	cellDir := filepath.Clean(filepath.Join("..", "pulp-cell"))
	socialOutput, err := filepath.Abs(filepath.Join(cellDir, "social.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", "-trimpath", "-buildmode=c-shared", "-o", socialOutput, ".")
	command.Dir = cellDir
	command.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build social WASM: %v\n%s", err, output)
	}

	luaRoot := filepath.Clean(filepath.Join("..", "..", "Pulp-Lua"))
	luaOutput, err := filepath.Abs(filepath.Join("..", "application", "lua-orchestrator.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	luaCommand := exec.Command("go", "build", "-trimpath", "-buildmode=c-shared", "-o", luaOutput, "./pulp-cell")
	luaCommand.Dir = luaRoot
	luaCommand.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if output, err := luaCommand.CombinedOutput(); err != nil {
		t.Fatalf("build Pulp-Lua WASM: %v\n%s", err, output)
	}
}

func callSocial[T any](t *testing.T, ctx context.Context, access run.ApplicationProviderAccess, event string, request any) T {
	t.Helper()
	requestWire, err := msgpack.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	dispatchWire, err := msgpack.Marshal(socialDispatchRequest{
		Event: event,
		Payload: map[string]any{
			"request_msgpack": string(requestWire),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := access.CallProvider(ctx, "lua-orchestrator", "orchestrator.dispatch", dispatchWire)
	if err != nil {
		t.Fatalf("dispatch %s: %v", event, err)
	}
	var dispatched socialDispatchResult
	if err := msgpack.Unmarshal(response, &dispatched); err != nil {
		t.Fatal(err)
	}
	object, ok := dispatched.Value.(map[string]any)
	if !ok {
		t.Fatalf("%s result is %T", event, dispatched.Value)
	}
	var ownerWire []byte
	switch value := object["response_msgpack"].(type) {
	case string:
		ownerWire = []byte(value)
	case []byte:
		ownerWire = value
	default:
		t.Fatalf("%s response_msgpack is %T", event, object["response_msgpack"])
	}
	var result T
	if err := msgpack.Unmarshal(ownerWire, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
