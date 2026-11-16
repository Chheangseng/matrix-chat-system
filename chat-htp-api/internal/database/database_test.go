package database_test

import (
	"strings"
	"testing"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/database"
)

func TestGetAPIDBDSN(t *testing.T) {
	cfg := &config.Config{
		APIDBHost:     "127.0.0.1",
		APIDBPort:     "5433",
		APIDBUser:     "chat_api",
		APIDBPassword: "chat_api_secret",
		APIDBName:     "chat_api_db",
		APIDBSSLMode:  "disable",
	}

	dsn := cfg.GetAPIDBDSN()
	if !strings.Contains(dsn, "host=127.0.0.1") {
		t.Errorf("expected host in dsn, got %s", dsn)
	}
	if !strings.Contains(dsn, "port=5433") {
		t.Errorf("expected port in dsn, got %s", dsn)
	}
	if !strings.Contains(dsn, "dbname=chat_api_db") {
		t.Errorf("expected dbname in dsn, got %s", dsn)
	}

	// Test custom DSN override
	cfg.APIDBDSN = "postgres://custom_user:custom_pass@localhost:5432/custom_db?sslmode=require"
	customDSN := cfg.GetAPIDBDSN()
	if customDSN != cfg.APIDBDSN {
		t.Errorf("expected custom DSN override, got %s", customDSN)
	}
}

func TestAutoMigrate_NilDB(t *testing.T) {
	err := database.AutoMigrate(nil)
	if err != nil {
		t.Errorf("expected nil error for nil db, got %v", err)
	}
}
