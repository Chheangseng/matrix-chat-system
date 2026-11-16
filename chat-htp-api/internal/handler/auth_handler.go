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

// ExchangeRequest represents the payload for POST /api-server/auth/exchange
type ExchangeRequest struct {
	Code  string `json:"code" binding:"required"`
	State string `json:"state"`
}

// MobileLoginRequest represents the payload for POST /api-server/auth/mobile-login
type MobileLoginRequest struct {
	Token string `json:"htp_token"`
}

// SupportLoginRequest represents credentials for customer support staff login
type SupportLoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
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

	// 3. Check Synapse joined rooms for common membership
	userRooms, err := h.adminClient.GetUserJoinedRooms(ctx, "", userID)
	if err == nil {
		for _, roomID := range userRooms {
			members, err := h.adminClient.GetRoomMembers(ctx, "", roomID)
			if err == nil {
				hasUser := false
				hasSupport := false
				for _, m := range members {
					if m == userID {
						hasUser = true
					}
					if m == supportUserID {
						hasSupport = true
					}
				}
				if hasUser && hasSupport {
					_ = h.adminClient.AddUserDirectRoom(ctx, userToken, userID, supportUserID, roomID)
					_ = h.adminClient.AddUserDirectRoom(ctx, "", supportUserID, userID, roomID)
					if h.db != nil {
						_ = h.db.WithContext(ctx).Create(&models.CustomerSupportRoom{
							RoomID:        roomID,
							UserID:        userID,
							SupportUserID: supportUserID,
						}).Error
					}
					return roomID, nil
				}
			}
		}
	}

	// 4. No room exists: create a new 1-on-1 direct message room
	roomName := fmt.Sprintf("Support: %s", h.cfg.SupportDisplayName)
	roomTopic := "Direct 1-on-1 customer support chat"
	roomID, err := h.adminClient.CreateDirectRoom(ctx, userToken, supportUserID, roomName, roomTopic)
	if err != nil {
		return "", fmt.Errorf("failed to create 1:1 support room: %w", err)
	}

	// 5. Force-join support account to the newly created room
	if err := h.adminClient.JoinUserToRoom(ctx, "", roomID, supportUserID); err != nil {
		log.Printf("[WARN] Failed to auto-join support user %s to room %s: %v", supportUserID, roomID, err)
	}

	// 6. Record in m.direct account data for both user and support
	_ = h.adminClient.AddUserDirectRoom(ctx, userToken, userID, supportUserID, roomID)
	_ = h.adminClient.AddUserDirectRoom(ctx, "", supportUserID, userID, roomID)

	// 7. Persist to PostgreSQL database (triggers BeforeCreate for UUID v7)
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

// Login handles GET /api-server/auth/login
// Redirects the client to HTP OIDC authorize endpoint or returns the URL as JSON.
// Only scopes "openid", "profile", and "phone" are allowed.
func (h *AuthHandler) Login(c *gin.Context) {
	requestedScope := c.Query("scope")
	authReq, err := h.oidcService.CreateAuthorizeRequest(requestedScope)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"errcode": "M_OIDC_ERROR",
			"error":   fmt.Sprintf("Failed to generate authorize request: %v", err),
		})
		return
	}

	// Return JSON if requested
	if c.Query("format") == "json" || strings.Contains(c.GetHeader("Accept"), "application/json") {
		c.JSON(http.StatusOK, gin.H{
			"authorize_url": authReq.URL,
			"state":         authReq.State,
		})
		return
	}

	// Otherwise perform standard browser redirect
	c.Redirect(http.StatusFound, authReq.URL)
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

// Exchange handles POST /api-server/auth/exchange
// Direct endpoint to exchange authorization code and state for a Matrix session
func (h *AuthHandler) Exchange(c *gin.Context) {
	var req ExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"errcode": "M_BAD_JSON",
			"error":   "Missing or invalid required parameter: code",
		})
		return
	}

	profile, err := h.oidcService.ExchangeCode(c.Request.Context(), req.Code, req.State)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"errcode": "M_OIDC_EXCHANGE_FAILED",
			"error":   fmt.Sprintf("Failed to exchange authorization code: %v", err),
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

	c.JSON(http.StatusOK, gin.H{
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

// SupportLogin handles POST /api-server/auth/support-login
// Authenticates support staff with username and password, strictly verifying that
// the user is a designated customer support account, and returns Matrix session credentials.
func (h *AuthHandler) SupportLogin(c *gin.Context) {
	var req SupportLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"errcode": "M_BAD_JSON",
			"error":   "Missing or invalid required fields: username and password",
		})
		return
	}

	trimmedUser := strings.TrimSpace(req.Username)
	supportUserID := h.cfg.GetSupportUserID()

	// Verify authorization: must match configured support user or registered customer support record
	isAuthorized := false
	if trimmedUser == h.cfg.SupportUser || trimmedUser == supportUserID ||
		h.adminClient.FormatUserID(trimmedUser) == supportUserID {
		isAuthorized = true
	} else if h.db != nil {
		var cs models.CustomerSupport
		if err := h.db.WithContext(c.Request.Context()).
			Where("username = ? OR matrix_user_id = ?", trimmedUser, h.adminClient.FormatUserID(trimmedUser)).
			First(&cs).Error; err == nil && cs.Status == "active" {
			isAuthorized = true
		}
	}

	if !isAuthorized {
		c.JSON(http.StatusForbidden, gin.H{
			"errcode": "M_FORBIDDEN",
			"error":   "Account is not authorized for customer support access",
		})
		return
	}

	loginTarget := trimmedUser
	if !strings.HasPrefix(loginTarget, "@") {
		loginTarget = supportUserID
	}

	session, err := h.adminClient.LoginWithPassword(c.Request.Context(), loginTarget, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"errcode": "M_UNAUTHORIZED",
			"error":   fmt.Sprintf("Invalid username or password: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"matrix": session,
		"user": gin.H{
			"username":     h.cfg.SupportUser,
			"user_id":      session.UserID,
			"display_name": h.cfg.SupportDisplayName,
			"avatar_url":   h.cfg.SupportAvatarURL,
			"role":         "support_agent",
		},
	})
}

// PreAuth handles POST /api-server/auth/pre-auth
// Simulates mobile app pre-authorization and returns challenge and redirect_to
func (h *AuthHandler) PreAuth(c *gin.Context) {
	token := extractBearerOrBodyToken(c)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"errcode": "M_MISSING_TOKEN",
			"error":   "HTP Bearer token is required in Authorization header or JSON body",
		})
		return
	}

	result, err := h.oidcService.SimulateMobilePreAuth(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"errcode": "M_PREAUTH_FAILED",
			"error":   fmt.Sprintf("Mobile pre-authorization failed: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, result)
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

	c.JSON(http.StatusOK, gin.H{
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

	var req MobileLoginRequest
	if err := c.ShouldBindJSON(&req); err == nil && req.Token != "" {
		return strings.TrimSpace(req.Token)
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
