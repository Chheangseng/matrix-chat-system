package handler

import (
	"net/http"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SupportHandler handles customer support account discovery and information endpoints
type SupportHandler struct {
	cfg *config.Config
	db  *gorm.DB
}

// NewSupportHandler creates a new SupportHandler instance
func NewSupportHandler(cfg *config.Config, db *gorm.DB) *SupportHandler {
	return &SupportHandler{
		cfg: cfg,
		db:  db,
	}
}

// GetSupportInfo handles GET /api-server/support/info
// Returns the synchronized customer support account details from the database,
// falling back cleanly to configuration defaults if DB is unavailable.
func (h *SupportHandler) GetSupportInfo(c *gin.Context) {
	if h.db != nil {
		var cs models.CustomerSupport
		if err := h.db.WithContext(c.Request.Context()).Where("username = ?", h.cfg.SupportUser).First(&cs).Error; err == nil {
			c.JSON(http.StatusOK, gin.H{
				"id":              cs.ID,
				"support_user_id": cs.MatrixUserID,
				"username":        cs.Username,
				"display_name":    cs.DisplayName,
				"avatar_url":      cs.AvatarURL,
				"is_admin":        cs.IsAdmin,
				"status":          cs.Status,
				"updated_at":      cs.UpdatedAt,
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"support_user_id": h.cfg.GetSupportUserID(),
		"username":        h.cfg.SupportUser,
		"display_name":    h.cfg.SupportDisplayName,
		"avatar_url":      h.cfg.SupportAvatarURL,
		"is_admin":        h.cfg.SupportAdmin,
		"status":          "active",
	})
}
