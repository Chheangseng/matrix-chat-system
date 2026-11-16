package config

import (
	"log"
	"net/url"
	"os"

	"github.com/joho/godotenv"
)

// Config holds the application configuration settings
type Config struct {
	Port              string
	Environment       string
	SynapseURL        *url.URL
	AllowedOrigins    string
	SynapseServerName string
	AdminUser         string
	AdminPassword     string
	AdminToken        string
}

// LoadConfig loads configuration from .env file and environment variables
func LoadConfig() (*Config, error) {
	// Attempt to load .env file if it exists, ignore error if missing
	if err := godotenv.Load(); err != nil {
		log.Println("[INFO] No .env file found or error reading, using environment variables")
	}

	port := getEnv("APP_PORT", "8080")
	env := getEnv("APP_ENV", "development")
	rawSynapseURL := getEnv("SYNAPSE_TARGET_URL", "http://localhost:8008")
	allowedOrigins := getEnv("ALLOWED_ORIGINS", "*")
	serverName := getEnv("SYNAPSE_SERVER_NAME", "localhost")
	adminUser := getEnv("SYNAPSE_ADMIN_USER", "admin")
	adminPassword := getEnv("SYNAPSE_ADMIN_PASSWORD", "admin_password_123")
	adminToken := getEnv("SYNAPSE_ADMIN_TOKEN", "")

	parsedURL, err := url.Parse(rawSynapseURL)
	if err != nil {
		return nil, err
	}

	return &Config{
		Port:              port,
		Environment:       env,
		SynapseURL:        parsedURL,
		AllowedOrigins:    allowedOrigins,
		SynapseServerName: serverName,
		AdminUser:         adminUser,
		AdminPassword:     adminPassword,
		AdminToken:        adminToken,
	}, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
