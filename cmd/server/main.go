package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bananalabs-oss/bunch/internal/blocks"
	"github.com/bananalabs-oss/bunch/internal/database"
	"github.com/bananalabs-oss/bunch/internal/friends"
	"github.com/bananalabs-oss/bunch/internal/presence"
	potassium "github.com/bananalabs-oss/potassium/middleware"
	"github.com/gin-gonic/gin"
)

func main() {
	log.Printf("Starting Bunch")

	jwtSecret := requireEnv("JWT_SECRET")
	serviceSecret := envOrDefault("SERVICE_SECRET", "dev-service-secret")
	databaseURL := envOrDefault("DATABASE_URL", "sqlite://bunch.db")
	host := envOrDefault("HOST", "0.0.0.0")
	port := envOrDefault("PORT", "8002")

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

	if err := database.Migrate(ctx, db); err != nil {
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
	authed.Use(potassium.JWTAuth(potassium.JWTConfig{
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
	internal.Use(potassium.ServiceAuth(serviceSecret))
	{
		internal.GET("/presence/:userId", presenceHandler.GetPresence)
		internal.POST("/presence/bulk", presenceHandler.BulkPresence)
		internal.GET("/presence/count", presenceHandler.OnlineCount)
	}

	// Start server with graceful shutdown
	addr := fmt.Sprintf("%s:%s", host, port)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		log.Printf("Bunch listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Printf("Shutting down Bunch...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Printf("Bunch stopped")
}

func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		log.Fatalf("%s is required", key)
	}
	return val
}

func envOrDefault(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}
