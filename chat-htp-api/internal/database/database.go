package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect initializes and returns a GORM PostgreSQL database connection
func Connect(cfg *config.Config) (*gorm.DB, error) {
	dsn := cfg.GetAPIDBDSN()

	logLevel := logger.Info
	if cfg.Environment == "production" {
		logLevel = logger.Warn
	}

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	}

	db, err := gorm.Open(postgres.Open(dsn), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres database connection: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve underlying sql.DB: %w", err)
	}

	// Configure connection pool settings
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(15 * time.Minute)

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("database ping check failed: %w", err)
	}

	log.Printf("[INFO] Connected to PostgreSQL API database (%s:%s/%s)",
		cfg.APIDBHost, cfg.APIDBPort, cfg.APIDBName)

	return db, nil
}

// AutoMigrate applies schema migrations when entity models are defined
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	if err := db.AutoMigrate(
		&models.CustomerSupport{},
		&models.CustomerSupportRoom{},
	); err != nil {
		return fmt.Errorf("failed to auto-migrate database entities: %w", err)
	}
	log.Println("[INFO] Database schema migration completed for models")
	return nil
}
