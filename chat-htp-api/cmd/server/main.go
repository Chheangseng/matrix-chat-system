package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/database"
	"chat-htp-api/internal/server"

	"gorm.io/gorm"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[FATAL] Failed to load configuration: %v\n", err)
	}

	// Initialize API PostgreSQL Database (GORM)
	var db *gorm.DB
	db, err = database.Connect(cfg)
	if err != nil {
		if cfg.Environment == "production" {
			log.Fatalf("[FATAL] Failed to connect to API PostgreSQL database: %v\n", err)
		}
		log.Printf("[WARN] Failed to connect to API PostgreSQL database: %v. Running in degraded mode without database.", err)
	} else {
		if err := database.AutoMigrate(db); err != nil {
			log.Fatalf("[FATAL] Database auto-migration failed: %v\n", err)
		}
	}

	srvInstance := server.New(cfg, db)

	httpServer := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: srvInstance.Engine,
	}

	// Channel to listen for errors coming from the listener
	serverErrors := make(chan error, 1)

	go func() {
		log.Printf("==================================================")
		log.Printf("  Chat API Gateway starting on port :%s", cfg.Port)
		log.Printf("  Target Matrix Synapse URL: %s", cfg.SynapseURL.String())
		log.Printf("  Routing: /api-server/* -> Handled by Gin")
		log.Printf("  Routing: /*            -> Proxied to Matrix Synapse")
		log.Printf("==================================================")

		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	// Channel to listen for interrupt signals for graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		log.Fatalf("[FATAL] Server error: %v\n", err)

	case sig := <-shutdown:
		log.Printf("[INFO] Shutdown signal received: %v. Initiating graceful shutdown...\n", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("[ERROR] Graceful shutdown failed, forcing exit: %v\n", err)
			_ = httpServer.Close()
		}

		log.Println("[INFO] Server stopped cleanly")
	}
}
