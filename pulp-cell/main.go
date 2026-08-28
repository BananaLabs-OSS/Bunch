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
//	GOOS=wasip1 GOARCH=wasm go build -trimpath -buildmode=c-shared -o social.wasm .
package main

import (
	"fmt"
	"net/http"

	"github.com/BananaLabs-OSS/Fiber/pulp"
	"github.com/BananaLabs-OSS/Fiber/pulp/cellconfig"
	pulpgin "github.com/BananaLabs-OSS/Fiber/pulp/gin"
	"github.com/BananaLabs-OSS/Fiber/pulp/gin/middleware"
	"github.com/BananaLabs-OSS/Fiber/pulp/workflow"
)

func main() {}

func init() {
	pulp.OnInit(bootstrap)
}

func bootstrap(configBytes []byte) error {
	cfg, err := parseConfig(configBytes)
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	friends := NewFriendsHandler(workflow.NewClient("lua-orchestrator"))
	blocks := NewBlocksHandler(friends)
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

type config struct {
	JWTSecret string `json:"jwt_secret"`
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
	if err := cellconfig.Decode(data, &cfg); err != nil {
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
