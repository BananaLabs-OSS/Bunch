// Bunch — Pulp cell port.
//
// Rewrite of the friends / blocks / presence microservice as a WASM
// cell. HTTP handlers run on pulpgin; data access uses Bun over
// the Fiber pulp/sql driver; the presence hub uses pulpgin's WS
// bridge instead of gorilla/websocket. Business logic is unchanged
// from the original service.
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o bunch.wasm .
package main

import (
	"context"
	dsql "database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/BananaLabs-OSS/Fiber/pulp"
	pulpgin "github.com/BananaLabs-OSS/Fiber/pulp/gin"
	"github.com/BananaLabs-OSS/Fiber/pulp/gin/middleware"
	_ "github.com/BananaLabs-OSS/Fiber/pulp/sql"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
)

func main() {}

var (
	db *bun.DB
)

func init() {
	pulp.OnInit(bootstrap)
}

func bootstrap(configBytes []byte) error {
	cfg, err := parseConfig(configBytes)
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	raw, err := dsql.Open("pulp", "")
	if err != nil {
		return fmt.Errorf("open pulp sql driver: %w", err)
	}
	// Match host single-writer pool; prevents nested-BEGIN races.
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	db = bun.NewDB(raw, sqlitedialect.New())

	if err := migrate(context.Background()); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	friends := NewFriendsHandler(db)
	blocks := NewBlocksHandler(db, friends)
	hub := NewHub(friends)
	presence := NewPresenceHandler(hub, []byte(cfg.JWTSecret))

	r := pulpgin.New()

	r.GET("/health", func(c *pulpgin.Context) {
		c.JSON(http.StatusOK, pulpgin.H{
			"service":      "bunch",
			"status":       "healthy",
			"online_count": hub.OnlineCount(),
		})
	})

	// WebSocket — JWT goes via ?token= query param (browsers cannot
	// set Authorization on WS upgrades).
	r.WS("/ws", presence.WSHandlers())

	// Authenticated player routes.
	authed := r.Group("/")
	authed.Use(middleware.JWTAuth(middleware.JWTConfig{Secret: []byte(cfg.JWTSecret)}))

	f := authed.Group("/friends")
	f.POST("/request", friends.SendRequest)
	f.POST("/accept", friends.AcceptRequest)
	f.POST("/decline", friends.DeclineRequest)
	f.DELETE("/:friendId", friends.RemoveFriend)
	f.GET("", friends.ListFriends)
	f.GET("/requests", friends.ListRequests)

	b := authed.Group("/blocks")
	b.POST("", blocks.BlockUser)
	b.DELETE("/:accountId", blocks.UnblockUser)
	b.GET("", blocks.ListBlocked)

	// Internal service routes.
	internal := r.Group("/internal")
	internal.Use(middleware.ServiceAuth(cfg.ServiceSecret))
	internal.GET("/presence/:userId", presence.GetPresence)
	internal.POST("/presence/bulk", presence.BulkPresence)
	internal.GET("/presence/count", presence.OnlineCount)

	if err := r.Run(); err != nil {
		return fmt.Errorf("router: %w", err)
	}
	return nil
}

func migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS friendships (
			id TEXT PRIMARY KEY,
			requester_id TEXT NOT NULL,
			addressee_id TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS blocks (
			id TEXT PRIMARY KEY,
			blocker_id TEXT NOT NULL,
			blocked_id TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_friendships_requester ON friendships (requester_id)`,
		`CREATE INDEX IF NOT EXISTS idx_friendships_addressee ON friendships (addressee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_friendships_status ON friendships (status)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_friendships_pair ON friendships (requester_id, addressee_id)`,
		`CREATE INDEX IF NOT EXISTS idx_blocks_blocker ON blocks (blocker_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_blocks_pair ON blocks (blocker_id, blocked_id)`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migrate exec: %w", err)
		}
	}
	return nil
}

type config struct {
	JWTSecret    string `json:"jwt_secret"`
	// ServiceSecret is the /internal-route auth token. Aliased to
	// `service_token` in manifests for backwards compatibility; the
	// canonical key is `service_secret` so the config name matches the
	// native env var (SERVICE_SECRET) the cmd/server reads.
	ServiceSecret string `json:"service_secret"`
	// ServiceTokenAlias preserves older manifests that wrote
	// `service_token = "..."` under the wrong key.
	ServiceTokenAlias string `json:"service_token"`
}

func parseConfig(data []byte) (config, error) {
	var cfg config
	if len(data) == 0 {
		// Match native cmd/server/main.go: JWT_SECRET is RequireEnv
		// (fatal if unset), SERVICE_SECRET has a dev default. Preserve
		// that asymmetry here so a cell running without a manifest
		// [config] block still fails with the same signal (missing JWT)
		// as the native binary.
		return cfg, fmt.Errorf("missing [config] — manifest must set jwt_secret")
	}
	var raw map[string]any
	if err := decodeMsgpack(data, &raw); err != nil {
		return cfg, err
	}
	jbytes, err := json.Marshal(raw)
	if err != nil {
		return cfg, fmt.Errorf("re-marshal config: %w", err)
	}
	if err := json.Unmarshal(jbytes, &cfg); err != nil {
		return cfg, fmt.Errorf("decode config: %w", err)
	}
	if cfg.JWTSecret == "" {
		return cfg, fmt.Errorf("jwt_secret missing from [config]")
	}
	// Fall back to the alias if the canonical key wasn't set. Mirrors
	// native's SERVICE_SECRET default of "dev-service-secret" so the
	// /internal routes are always reachable with a known token during
	// development and under the parity harness.
	if cfg.ServiceSecret == "" {
		cfg.ServiceSecret = cfg.ServiceTokenAlias
	}
	if cfg.ServiceSecret == "" {
		cfg.ServiceSecret = "dev-service-secret"
	}
	return cfg, nil
}
