package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"chat-htp-api/internal/synapse"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UserHandler handles user management endpoints
type UserHandler struct {
	adminClient *synapse.AdminClient
	db          *gorm.DB
}

// NewUserHandler creates a new UserHandler instance
func NewUserHandler(adminClient *synapse.AdminClient, db *gorm.DB) *UserHandler {
	return &UserHandler{
		adminClient: adminClient,
		db:          db,
	}
}

// CreateUserRequest represents the incoming JSON body to create a user
type CreateUserRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url"`
	Admin       bool   `json:"admin"`
}

// extractToken extracts optional Bearer token from the incoming Authorization header
func extractToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return ""
}

// CreateUser handles POST /api-server/users
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"errcode": "M_BAD_JSON",
			"error":   "Missing or invalid required fields (username, password)",
		})
		return
	}

	token := extractToken(c)
	payload := synapse.CreateUserPayload{
		Password:    req.Password,
		DisplayName: req.DisplayName,
		AvatarURL:   req.AvatarURL,
		Admin:       req.Admin,
	}

	user, err := h.adminClient.CreateOrUpdateUser(c.Request.Context(), token, req.Username, payload)
	if err != nil {
		var apiErr *synapse.AdminAPIError
		if errors.As(err, &apiErr) {
			c.JSON(apiErr.StatusCode, gin.H{
				"errcode": apiErr.ErrCode,
				"error":   apiErr.ErrorMsg,
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"errcode": "M_UNKNOWN",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, user)
}

// GetUser handles GET /api-server/users/:id
func (h *UserHandler) GetUser(c *gin.Context) {
	userID := c.Param("id")
	if strings.TrimSpace(userID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"errcode": "M_INVALID_PARAM",
			"error":   "User ID parameter is required",
		})
		return
	}

	token := extractToken(c)
	user, err := h.adminClient.GetUser(c.Request.Context(), token, userID)
	if err != nil {
		var apiErr *synapse.AdminAPIError
		if errors.As(err, &apiErr) {
			c.JSON(apiErr.StatusCode, gin.H{
				"errcode": apiErr.ErrCode,
				"error":   apiErr.ErrorMsg,
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"errcode": "M_UNKNOWN",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, user)
}

// ListUsers handles GET /api-server/users
func (h *UserHandler) ListUsers(c *gin.Context) {
	from := c.Query("from")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	guests := c.Query("guests") == "true"
	deactivated := c.Query("deactivated") == "true"

	token := extractToken(c)
	users, err := h.adminClient.ListUsers(c.Request.Context(), token, from, limit, guests, deactivated)
	if err != nil {
		var apiErr *synapse.AdminAPIError
		if errors.As(err, &apiErr) {
			c.JSON(apiErr.StatusCode, gin.H{
				"errcode": apiErr.ErrCode,
				"error":   apiErr.ErrorMsg,
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"errcode": "M_UNKNOWN",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, users)
}
