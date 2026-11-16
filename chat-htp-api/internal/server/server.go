package server

import (
	"net/http"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/handler"
	"chat-htp-api/internal/middleware"
	"chat-htp-api/internal/oidc"
	"chat-htp-api/internal/synapse"

	"github.com/gin-gonic/gin"
)

// Server wraps the Gin engine and application configuration
type Server struct {
	Engine *gin.Engine
	Config *config.Config
}

// New creates and configures a new Server instance
func New(cfg *config.Config) *Server {
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()

	// Global Middlewares
	engine.Use(middleware.RequestLogger())
	engine.Use(gin.Recovery())
	engine.Use(middleware.CORSMiddleware(cfg.AllowedOrigins))

	// Synapse Admin Client & Handlers
	adminClient := synapse.NewAdminClient(cfg)
	userHandler := handler.NewUserHandler(adminClient)
	oidcService := oidc.NewService(cfg)
	authHandler := handler.NewAuthHandler(oidcService, adminClient, cfg)

	// Setup custom /api-server routes
	registerAPIRoutes(engine, userHandler, authHandler)

	// Well-known Matrix client discovery: advertise the gateway address, never the internal Synapse address
	engine.GET("/.well-known/matrix/client", func(c *gin.Context) {
		scheme := "http"
		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		baseURL := scheme + "://" + c.Request.Host
		c.JSON(http.StatusOK, gin.H{
			"m.homeserver": gin.H{
				"base_url": baseURL,
			},
		})
	})

	// Fallback: Any request not matching /api-server (and not .well-known above)
	// passes through the MatrixGuard middleware before being proxied to Matrix Synapse
	engine.NoRoute(
		middleware.MatrixGuard(),
		middleware.MatrixProxy(cfg.SynapseURL),
	)

	return &Server{
		Engine: engine,
		Config: cfg,
	}
}

// registerAPIRoutes sets up endpoints reserved for the custom backend API under /api-server
func registerAPIRoutes(engine *gin.Engine, userHandler *handler.UserHandler, authHandler *handler.AuthHandler) {
	api := engine.Group("/api-server")
	{
		// Health check endpoint
		api.GET("/healthz", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status":  "ok",
				"service": "chat-htp-api",
			})
		})

		// API Info endpoint
		api.GET("/info", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"version":     "1.0.0",
				"description": "Custom Chat API Gateway with Matrix Synapse Proxy",
			})
		})

		// Authentication & OIDC endpoints
		auth := api.Group("/auth")
		{
			auth.GET("/login", authHandler.Login)
			auth.GET("/callback", authHandler.Callback)
			auth.POST("/exchange", authHandler.Exchange)
			auth.POST("/pre-auth", authHandler.PreAuth)
			auth.POST("/mobile-login", authHandler.MobileLogin)
		}

		// User Management endpoints (Synapse User Admin API)
		users := api.Group("/users")
		{
			users.POST("", userHandler.CreateUser)
			users.GET("", userHandler.ListUsers)
			users.GET("/:id", userHandler.GetUser)
		}
	}
}

// Start runs the HTTP server on the configured port
func (s *Server) Start() error {
	addr := ":" + s.Config.Port
	return s.Engine.Run(addr)
}
