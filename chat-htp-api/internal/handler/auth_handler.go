package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/models"
	"chat-htp-api/internal/oidc"
	"chat-htp-api/internal/synapse"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var nonAlphaNumDash = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// AuthHandler handles OIDC authentication and Matrix user provisioning
type AuthHandler struct {
	oidcService *oidc.Service
	adminClient *synapse.AdminClient
	cfg         *config.Config
	db          *gorm.DB
}

// NewAuthHandler creates a new AuthHandler instance
func NewAuthHandler(oidcService *oidc.Service, adminClient *synapse.AdminClient, cfg *config.Config, db *gorm.DB) *AuthHandler {
	return &AuthHandler{
		oidcService: oidcService,
		adminClient: adminClient,
		cfg:         cfg,
		db:          db,
	}
}

type preAuthBody struct {
	Token    string `json:"token"`
	HTPToken string `json:"htp_token"`
}

// provisionMatrixUserAndLogin provisions or updates the user profile in Synapse and logs them in
// using deterministic credentials derived from the OIDC subject ID.
func (h *AuthHandler) provisionMatrixUserAndLogin(ctx context.Context, profile *oidc.UserProfile) (*synapse.MatrixLoginResponse, error) {
	rawSub := nonAlphaNumDash.ReplaceAllString(profile.Sub, "_")
	username := fmt.Sprintf("htp_%s", rawSub)
	deterministicPassword := fmt.Sprintf("Pass_%s_Secret987!", rawSub)

	displayName := profile.Name
	if displayName == "" {
		displayName = profile.Nickname
	}
	if displayName == "" {
		displayName = "HTP User " + rawSub
	}

	createPayload := synapse.CreateUserPayload{
		Password:    deterministicPassword,
		DisplayName: displayName,
		AvatarURL:   profile.Picture,
	}

	// 1. Create or update user in Synapse
	_, err := h.adminClient.CreateOrUpdateUser(ctx, "", username, createPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to provision user in Synapse: %w", err)
	}

	// 2. Obtain Matrix access token via Synapse Admin login
	matrixSession, err := h.adminClient.LoginAsUser(ctx, "", username)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Matrix session for user: %w", err)
	}

	// 3. Ensure default 1-on-1 direct message room with customer support
	supportRoomID, err := h.ensureCustomerSupportRoom(ctx, matrixSession.AccessToken, matrixSession.UserID)
	if err != nil {
		log.Printf("[WARN] Failed to ensure default customer support room for %s: %v", matrixSession.UserID, err)
	} else {
		matrixSession.SupportRoomID = supportRoomID
	}

	return matrixSession, nil
}

// ensureCustomerSupportRoom ensures that a 1-on-1 direct message room exists between the user
// and the customer support account. If a room already exists, it is reused instead of creating a duplicate.
func (h *AuthHandler) ensureCustomerSupportRoom(ctx context.Context, userToken, userID string) (string, error) {
	supportUserID := h.cfg.GetSupportUserID()
	if supportUserID == "" {
		return "", fmt.Errorf("customer support user ID is not configured")
	}

	// 1. Check PostgreSQL Database if available
	if h.db != nil {
		var roomRecord models.CustomerSupportRoom
		err := h.db.WithContext(ctx).
			Where("user_id = ? AND support_user_id = ?", userID, supportUserID).
			First(&roomRecord).Error
		if err == nil && roomRecord.RoomID != "" {
			return roomRecord.RoomID, nil
		}
	}

	// 2. Check Matrix m.direct account data on the user
	directData, err := h.adminClient.GetUserDirectRooms(ctx, userToken, userID)
	if err == nil {
		if rooms, ok := directData[supportUserID]; ok && len(rooms) > 0 {
			existingRoomID := rooms[0]
			if h.db != nil {
				_ = h.db.WithContext(ctx).Create(&models.CustomerSupportRoom{
					RoomID:        existingRoomID,
					UserID:        userID,
					SupportUserID: supportUserID,
				}).Error
			}
			return existingRoomID, nil
		}
	}

	// 3. No room exists: create a new 1-on-1 direct message room
	roomName := fmt.Sprintf("Support: %s", h.cfg.SupportDisplayName)
	roomTopic := "Direct 1-on-1 customer support chat"
	roomID, err := h.adminClient.CreateDirectRoom(ctx, userToken, supportUserID, roomName, roomTopic)
	if err != nil {
		return "", fmt.Errorf("failed to create 1:1 support room: %w", err)
	}

	// 4. Force-join support account to the newly created room
	if err := h.adminClient.JoinUserToRoom(ctx, "", roomID, supportUserID); err != nil {
		log.Printf("[WARN] Failed to auto-join support user %s to room %s: %v", supportUserID, roomID, err)
	}

	// 5. Record in m.direct account data for both user and support
	_ = h.adminClient.AddUserDirectRoom(ctx, userToken, userID, supportUserID, roomID)
	_ = h.adminClient.AddUserDirectRoom(ctx, "", supportUserID, userID, roomID)

	// 6. Persist to PostgreSQL database
	if h.db != nil {
		roomRecord := models.CustomerSupportRoom{
			RoomID:        roomID,
			UserID:        userID,
			SupportUserID: supportUserID,
		}
		if err := h.db.WithContext(ctx).Create(&roomRecord).Error; err != nil {
			log.Printf("[WARN] Failed to save customer support room to database: %v", err)
		}
	}

	log.Printf("[INFO] Established default 1:1 customer support room: %s (user: %s, support: %s)", roomID, userID, supportUserID)
	return roomID, nil
}

// Callback handles GET /api-server/auth/callback
// Exchanges authorization code, syncs user with Matrix Synapse, and issues Matrix session
func (h *AuthHandler) Callback(c *gin.Context) {
	if errCode := c.Query("error"); errCode != "" {
		errDesc := c.DefaultQuery("error_description", "OIDC authorization rejected")
		c.JSON(http.StatusBadRequest, gin.H{
			"errcode": "M_OIDC_DENIED",
			"error":   fmt.Sprintf("%s: %s", errCode, errDesc),
		})
		return
	}

	code := c.Query("code")
	if strings.TrimSpace(code) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"errcode": "M_MISSING_PARAM",
			"error":   "Missing authorization code in query parameters",
		})
		return
	}

	state := c.Query("state")

	profile, err := h.oidcService.ExchangeCode(c.Request.Context(), code, state)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"errcode": "M_OIDC_EXCHANGE_FAILED",
			"error":   fmt.Sprintf("Failed to exchange OIDC code: %v", err),
		})
		return
	}

	matrixSession, err := h.provisionMatrixUserAndLogin(c.Request.Context(), profile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"errcode": "M_SYNAPSE_PROVISION_FAILED",
			"error":   fmt.Sprintf("Failed to initialize Matrix user account: %v", err),
		})
		return
	}

	responseData := gin.H{
		"matrix":          matrixSession,
		"support_room_id": matrixSession.SupportRoomID,
		"user": gin.H{
			"sub":          profile.Sub,
			"name":         profile.Name,
			"nickname":     profile.Nickname,
			"phone_number": profile.PhoneNumber,
			"picture":      profile.Picture,
		},
		"oidc": gin.H{
			"access_token": profile.AccessToken,
			"id_token":     profile.IDToken,
		},
	}

	// If browser popup or redirect flow expects HTML
	accept := c.GetHeader("Accept")
	if strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json") {
		clientRedirectURL := fmt.Sprintf("%s?auth_token=%s&room_id=%s&user_id=%s",
			h.cfg.ClientUIURL,
			url.QueryEscape(matrixSession.AccessToken),
			url.QueryEscape(matrixSession.SupportRoomID),
			url.QueryEscape(matrixSession.UserID),
		)

		c.Header("Content-Type", "text/html; charset=utf-8")
		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>HTP Login Successful</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; background: #f4f6f8; }
        .card { background: white; padding: 32px 40px; border-radius: 12px; box-shadow: 0 4px 12px rgba(0,0,0,0.08); text-align: center; max-width: 400px; }
        h1 { font-size: 20px; color: #111827; margin-bottom: 8px; }
        p { color: #6b7280; font-size: 14px; margin-bottom: 24px; }
        .btn { display: inline-block; padding: 10px 20px; background: #2563eb; color: white; text-decoration: none; border-radius: 6px; font-weight: 500; }
    </style>
</head>
<body>
    <div class="card">
        <h1>Authentication Successful</h1>
        <p>You have signed in as <strong>%s</strong>. Redirecting to Customer Support chat...</p>
        <a class="btn" href="%s">Open Customer Support</a>
    </div>
    <script>
        if (window.opener) {
            window.opener.postMessage({ type: 'matrix_oidc_success', data: %s }, '*');
            setTimeout(function() { window.close(); }, 1200);
        } else {
            window.location.href = %q;
        }
    </script>
</body>
</html>`, templateEscape(profile.Name), clientRedirectURL, mustJSON(responseData), clientRedirectURL)
		c.String(http.StatusOK, html)
		return
	}

	c.JSON(http.StatusOK, responseData)
}

// MobileLogin handles POST /api-server/auth/mobile-login
// Seamless single-call login for mobile applications: accepts HTP user token, runs pre-auth,
// exchanges code, auto-provisions user, and returns ready-to-use Matrix access token.
func (h *AuthHandler) MobileLogin(c *gin.Context) {
	token := extractBearerOrBodyToken(c)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"errcode": "M_MISSING_TOKEN",
			"error":   "HTP Bearer token is required in Authorization header or JSON body",
		})
		return
	}

	preAuth, err := h.oidcService.SimulateMobilePreAuth(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"errcode": "M_PREAUTH_FAILED",
			"error":   fmt.Sprintf("Seamless mobile login failed during pre-auth: %v", err),
		})
		return
	}

	code := preAuth.Code
	state := preAuth.State

	// If code wasn't parsed from redirect_to directly, try parsing redirect_to URL
	if code == "" && preAuth.RedirectTo != "" {
		if parsed, err := url.Parse(preAuth.RedirectTo); err == nil {
			code = parsed.Query().Get("code")
			if s := parsed.Query().Get("state"); s != "" {
				state = s
			}
		}
	}

	if code == "" {
		// Return redirect_to URL for client to complete authorization if code not directly returned
		c.JSON(http.StatusOK, gin.H{
			"challenge":   preAuth.Challenge,
			"redirect_to": preAuth.RedirectTo,
			"state":       preAuth.State,
		})
		return
	}

	// Complete code exchange
	profile, err := h.oidcService.ExchangeCode(c.Request.Context(), code, state)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"errcode": "M_OIDC_EXCHANGE_FAILED",
			"error":   fmt.Sprintf("Failed to exchange code during mobile login: %v", err),
		})
		return
	}

	matrixSession, err := h.provisionMatrixUserAndLogin(c.Request.Context(), profile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"errcode": "M_SYNAPSE_PROVISION_FAILED",
			"error":   fmt.Sprintf("Failed to initialize Matrix user account: %v", err),
		})
		return
	}

	clientRedirectURL := fmt.Sprintf("%s?auth_token=%s&room_id=%s&user_id=%s",
		h.cfg.ClientUIURL,
		url.QueryEscape(matrixSession.AccessToken),
		url.QueryEscape(matrixSession.SupportRoomID),
		url.QueryEscape(matrixSession.UserID),
	)

	c.JSON(http.StatusOK, gin.H{
		"redirect_to":     clientRedirectURL,
		"matrix":          matrixSession,
		"support_room_id": matrixSession.SupportRoomID,
		"user": gin.H{
			"sub":          profile.Sub,
			"name":         profile.Name,
			"nickname":     profile.Nickname,
			"phone_number": profile.PhoneNumber,
			"picture":      profile.Picture,
		},
		"oidc": gin.H{
			"access_token": profile.AccessToken,
			"id_token":     profile.IDToken,
		},
	})
}

// extractBearerOrBodyToken extracts token from Authorization header or request body
func extractBearerOrBodyToken(c *gin.Context) string {
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}

	var req preAuthBody
	if err := c.ShouldBindJSON(&req); err == nil {
		if req.HTPToken != "" {
			return strings.TrimSpace(req.HTPToken)
		}
		if req.Token != "" {
			return strings.TrimSpace(req.Token)
		}
	}

	return ""
}

func templateEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "<", "&lt;"), ">", "&gt;")
}

func mustJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
