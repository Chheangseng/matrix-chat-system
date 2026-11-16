package config

import (
	"fmt"
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
	HTPOIDCIssuer     string
	OIDCClientID      string
	OIDCClientSecret  string
	OIDCRedirectURI   string

	// API PostgreSQL Database Configuration (GORM)
	APIDBHost     string
	APIDBPort     string
	APIDBUser     string
	APIDBPassword string
	APIDBName     string
	APIDBSSLMode  string
	APIDBDSN      string

	// Customer Support Account Configuration
	SupportUser        string
	SupportPassword    string
	SupportDisplayName string
	SupportAvatarURL   string
	SupportAdmin       bool

	// Frontend Client UI URL
	ClientUIURL string
}

// LoadConfig loads configuration from .env file and environment variables
func LoadConfig() (*Config, error) {
	// Attempt to load .env file if it exists, checking both current directory and chat-htp-api/.env
	_ = godotenv.Load(".env", "chat-htp-api/.env")

	port := getEnv("APP_PORT", "8080")
	env := getEnv("APP_ENV", "development")
	rawSynapseURL := getEnv("SYNAPSE_TARGET_URL", "http://localhost:8008")
	allowedOrigins := getEnv("ALLOWED_ORIGINS", "*")
	serverName := getEnv("SYNAPSE_SERVER_NAME", "localhost")
	adminUser := getEnv("SYNAPSE_ADMIN_USER", "admin")
	adminPassword := getEnv("SYNAPSE_ADMIN_PASSWORD", "admin_password_123")
	adminToken := getEnv("SYNAPSE_ADMIN_TOKEN", "")
	oidcIssuer := getEnv("HTP_OIDC_ISSUER", "http://localhost:5054")
	oidcClientID := getEnv("OIDC_CLIENT_ID", "")
	oidcClientSecret := getEnv("OIDC_CLIENT_SECRET", "")
	oidcRedirectURI := getEnv("OIDC_REDIRECT_URI", "http://localhost:8080/api-server/auth/callback")

	apiDBHost := getEnv("API_DB_HOST", "localhost")
	apiDBPort := getEnv("API_DB_PORT", "5433")
	apiDBUser := getEnv("API_DB_USER", "chat_api")
	apiDBPassword := getEnv("API_DB_PASSWORD", "chat_api_secret")
	apiDBName := getEnv("API_DB_NAME", "chat_api_db")
	apiDBSSLMode := getEnv("API_DB_SSLMODE", "disable")
	apiDBDSN := getEnv("API_DB_DSN", "")

	supportUser := getEnv("SUPPORT_USER", "support")
	supportPassword := getEnv("SUPPORT_PASSWORD", "support_password_123")
	supportDisplayName := getEnv("SUPPORT_DISPLAY_NAME", "Customer Support")
	supportAvatarURL := getEnv("SUPPORT_AVATAR_URL", "")
	supportAdmin := getEnv("SUPPORT_ADMIN", "false") == "true"
	clientUIURL := getEnv("CLIENT_UI_URL", "http://localhost:5001")

	parsedURL, err := url.Parse(rawSynapseURL)
	if err != nil {
		return nil, err
	}

	return &Config{
		Port:               port,
		Environment:        env,
		SynapseURL:         parsedURL,
		AllowedOrigins:     allowedOrigins,
		SynapseServerName:  serverName,
		AdminUser:          adminUser,
		AdminPassword:      adminPassword,
		AdminToken:         adminToken,
		HTPOIDCIssuer:      oidcIssuer,
		OIDCClientID:       oidcClientID,
		OIDCClientSecret:   oidcClientSecret,
		OIDCRedirectURI:    oidcRedirectURI,
		APIDBHost:          apiDBHost,
		APIDBPort:          apiDBPort,
		APIDBUser:          apiDBUser,
		APIDBPassword:      apiDBPassword,
		APIDBName:          apiDBName,
		APIDBSSLMode:       apiDBSSLMode,
		APIDBDSN:           apiDBDSN,
		SupportUser:        supportUser,
		SupportPassword:    supportPassword,
		SupportDisplayName: supportDisplayName,
		SupportAvatarURL:   supportAvatarURL,
		SupportAdmin:       supportAdmin,
		ClientUIURL:        clientUIURL,
	}, nil
}

// GetSupportUserID returns the formatted Matrix user ID for the customer support account
func (c *Config) GetSupportUserID() string {
	return fmt.Sprintf("@customer-support-%s:%s", c.SupportUser, c.SynapseServerName)
}

// GetAPIDBDSN returns the connection string for GORM PostgreSQL
func (c *Config) GetAPIDBDSN() string {
	if c.APIDBDSN != "" {
		return c.APIDBDSN
	}
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=UTC",
		c.APIDBHost, c.APIDBPort, c.APIDBUser, c.APIDBPassword, c.APIDBName, c.APIDBSSLMode)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
