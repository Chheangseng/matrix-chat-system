package handler

import (
	"net/http"

	"chat-htp-api/internal/config"

	"github.com/gin-gonic/gin"
)

// SupportHandler handles customer support account discovery and information endpoints
type SupportHandler struct {
	cfg *config.Config
}

// NewSupportHandler creates a new SupportHandler instance
func NewSupportHandler(cfg *config.Config) *SupportHandler {
	return &SupportHandler{
		cfg: cfg,
	}
}

// GetSupportInfo handles GET /api-server/support/info
// Returns the public customer support account details for client applications to initiate support chats
func (h *SupportHandler) GetSupportInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"support_user_id": h.cfg.GetSupportUserID(),
		"username":        h.cfg.SupportUser,
		"display_name":    h.cfg.SupportDisplayName,
		"avatar_url":      h.cfg.SupportAvatarURL,
	})
}
