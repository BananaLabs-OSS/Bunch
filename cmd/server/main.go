package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/bananalabs-oss/bunch/internal/blocks"
	"github.com/bananalabs-oss/bunch/internal/friends"
	"github.com/bananalabs-oss/bunch/internal/models"
	"github.com/bananalabs-oss/bunch/internal/presence"
	"github.com/bananalabs-oss/potassium/config"
	"github.com/bananalabs-oss/potassium/database"
	"github.com/bananalabs-oss/potassium/middleware"
	"github.com/bananalabs-oss/potassium/server"
	"github.com/gin-gonic/gin"
)

func main() {
	log.Printf("Starting Bunch")

	jwtSecret := config.RequireEnv("JWT_SECRET")
	serviceSecret := config.EnvOrDefault("SERVICE_SECRET", "dev-service-secret")
	databaseURL := config.EnvOrDefault("DATABASE_URL", "sqlite://bunch.db")
	host := config.EnvOrDefault("HOST", "0.0.0.0")
	port := config.EnvOrDefault("PORT", "8002")

	log.Printf("Bunch Configuration:")
	log.Printf("  Host:     %s", host)
	log.Printf("  Port:     %s", port)
	log.Printf("  Database: %s", databaseURL)

	ctx := context.Background()

	db, err := database.Connect(databaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(ctx, db, []interface{}{
		(*models.Friendship)(nil),
		(*models.Block)(nil),
	}, []database.Index{
		{Name: "idx_friendships_requester", Query: "CREATE INDEX IF NOT EXISTS idx_friendships_requester ON friendships (requester_id)"},
		{Name: "idx_friendships_addressee", Query: "CREATE INDEX IF NOT EXISTS idx_friendships_addressee ON friendships (addressee_id)"},
		{Name: "idx_friendships_status", Query: "CREATE INDEX IF NOT EXISTS idx_friendships_status ON friendships (status)"},
		{Name: "idx_friendships_pair", Query: "CREATE UNIQUE INDEX IF NOT EXISTS idx_friendships_pair ON friendships (requester_id, addressee_id)"},
		{Name: "idx_blocks_blocker", Query: "CREATE INDEX IF NOT EXISTS idx_blocks_blocker ON blocks (blocker_id)"},
		{Name: "idx_blocks_pair", Query: "CREATE UNIQUE INDEX IF NOT EXISTS idx_blocks_pair ON blocks (blocker_id, blocked_id)"},
	}); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Handlers
	friendsHandler := friends.NewHandler(db)
	blocksHandler := blocks.NewHandler(db, friendsHandler)

	// Presence
	presenceHub := presence.NewHub(friendsHandler)
	presenceHandler := presence.NewHandler(presenceHub, []byte(jwtSecret))

	// Router
	router := gin.Default()

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service":      "bunch",
			"status":       "healthy",
			"online_count": presenceHub.OnlineCount(),
		})
	})

	// WebSocket — JWT via query param, no middleware
	router.GET("/ws", presenceHandler.WebSocket)

	// Player-facing routes (JWT auth via Potassium)
	authed := router.Group("/")
	authed.Use(middleware.JWTAuth(middleware.JWTConfig{
		Secret: []byte(jwtSecret),
	}))
	{
		f := authed.Group("/friends")
		{
			f.POST("/request", friendsHandler.SendRequest)
			f.POST("/accept", friendsHandler.AcceptRequest)
			f.POST("/decline", friendsHandler.DeclineRequest)
			f.DELETE("/:friendId", friendsHandler.RemoveFriend)
			f.GET("", friendsHandler.ListFriends)
			f.GET("/requests", friendsHandler.ListRequests)
		}

		b := authed.Group("/blocks")
		{
			b.POST("", blocksHandler.BlockUser)
			b.DELETE("/:accountId", blocksHandler.UnblockUser)
			b.GET("", blocksHandler.ListBlocked)
		}
	}

	// Internal service routes (service token auth)
	internal := router.Group("/internal")
	internal.Use(middleware.ServiceAuth(serviceSecret))
	{
		internal.GET("/presence/:userId", presenceHandler.GetPresence)
		internal.POST("/presence/bulk", presenceHandler.BulkPresence)
		internal.GET("/presence/count", presenceHandler.OnlineCount)
	}

	addr := fmt.Sprintf("%s:%s", host, port)
	server.ListenAndShutdown(addr, router, "Bunch")
}
