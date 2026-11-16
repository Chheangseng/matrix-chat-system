package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"chat-htp-api/internal/config"
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
	Token string `json:"token"`
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

	return matrixSession, nil
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
		"matrix": matrixSession,
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
    </style>
</head>
<body>
    <div class="card">
        <h1>Authentication Successful</h1>
        <p>You have signed in as <strong>%s</strong>. You may close this window and return to the application.</p>
    </div>
    <script>
        if (window.opener) {
            window.opener.postMessage({ type: 'matrix_oidc_success', data: %s }, '*');
            setTimeout(function() { window.close(); }, 1200);
        }
    </script>
</body>
</html>`, templateEscape(profile.Name), mustJSON(responseData))
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
		"matrix": matrixSession,
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
		"matrix": matrixSession,
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
