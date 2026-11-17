package handler_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/handler"
	"chat-htp-api/internal/oidc"
	"chat-htp-api/internal/synapse"

	"github.com/gin-gonic/gin"
)

func setupAuthRouter(authHandler *handler.AuthHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api-server/auth")
	{
		api.GET("/callback", authHandler.Callback)
		api.POST("/mobile-login", authHandler.MobileLogin)
	}
	return r
}

func setupOIDCAndSynapseMock(t *testing.T) (*httptest.Server, *config.Config, *oidc.Service, *synapse.AdminClient) {
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		// Synapse Admin Login (to get admin token)
		case r.Method == http.MethodPost && r.URL.Path == "/_matrix/client/v3/login":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"mock_admin_token"}`))

		// Synapse Create or Update User (PUT)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v2/users/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"name": "@htp_testuser:localhost",
				"admin": false,
				"deactivated": false,
				"displayname": "HTP Test User"
			}`))

		// Synapse Admin Login As User (POST /_synapse/admin/v1/users/<id>/login)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v1/users/") && strings.HasSuffix(r.URL.Path, "/login"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"access_token": "syt_mock_matrix_token_12345",
				"device_id": "MOCK_DEVICE_ID",
				"home_server": "localhost",
				"user_id": "@htp_testuser:localhost"
			}`))

		// OIDC Authorize endpoint (GET)
		case r.Method == http.MethodGet && r.URL.Path == "/oauth2/authorize":
			challenge := "mock_login_challenge_777"
			redirectURL := fmt.Sprintf("%s/login?login_challenge=%s", mockServer.URL, challenge)
			http.Redirect(w, r, redirectURL, http.StatusFound)

		// OIDC Accept login request
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v2/oidc/login-requests/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"data": {
					"redirect_to": "http://localhost:8080/api-server/auth/callback?code=mock_code_mobile&state=mock_mobile_state"
				}
			}`))

		// OIDC Token exchange
		case r.Method == http.MethodPost && r.URL.Path == "/oauth2/token":
			payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"testuser","name":"HTP Test User"}`))
			idToken := fmt.Sprintf("header.%s.sig", payload)

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "oidc_access_token_123",
				"id_token":     idToken,
				"token_type":   "Bearer",
				"expires_in":   3600,
			})

		// OIDC Userinfo
		case r.Method == http.MethodGet && r.URL.Path == "/oauth2/userinfo":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"sub":          "testuser",
				"name":         "HTP Test User",
				"nickname":     "testuser",
				"phone_number": "+84999999999",
				"picture":      "https://example.com/pic.png",
			})

		// Matrix Create Room (POST /_matrix/client/v3/createRoom)
		case r.Method == http.MethodPost && r.URL.Path == "/_matrix/client/v3/createRoom":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"room_id":"!mock_support_room_1:localhost"}`))

		// Synapse Admin Join Room (POST /_synapse/admin/v1/join/...)
		case r.Method == http.MethodPost && (strings.HasPrefix(r.URL.Path, "/_synapse/admin/v1/join/") || strings.HasPrefix(r.URL.Path, "/_synapse/admin/v1/rooms/")):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"room_id":"!mock_support_room_1:localhost"}`))

		// Matrix Account Data (GET / PUT /_matrix/client/v3/user/.../account_data/m.direct)
		case strings.Contains(r.URL.Path, "/account_data/m.direct"):
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{}`))
			}

		// Synapse Admin joined rooms
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v1/users/") && strings.HasSuffix(r.URL.Path, "/joined_rooms"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"joined_rooms":[],"total":0}`))

		// Synapse Admin room members
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v1/rooms/") && strings.HasSuffix(r.URL.Path, "/members"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"members":[],"total":0}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	parsedURL, _ := url.Parse(mockServer.URL)
	cfg := &config.Config{
		SynapseURL:         parsedURL,
		SynapseServerName:  "localhost",
		AdminUser:          "admin",
		AdminPassword:      "secret",
		AdminToken:         "mock_admin_token",
		SupportUser:        "support",
		SupportDisplayName: "Customer Support",
		HTPOIDCIssuer:      mockServer.URL,
		OIDCClientID:       "test_client",
		OIDCClientSecret:   "test_secret",
		OIDCRedirectURI:    "http://localhost:8080/api-server/auth/callback",
	}

	adminClient := synapse.NewAdminClient(cfg)
	oidcSvc := oidc.NewService(cfg)
	oidcSvc.SetHTTPClient(mockServer.Client())

	return mockServer, cfg, oidcSvc, adminClient
}

func TestAuthHandler_Callback_Success(t *testing.T) {
	mockServer, cfg, oidcSvc, adminClient := setupOIDCAndSynapseMock(t)
	defer mockServer.Close()

	authHandler := handler.NewAuthHandler(oidcSvc, adminClient, cfg, nil)
	router := setupAuthRouter(authHandler)

	authReq, _ := oidcSvc.CreateAuthorizeRequest()

	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("/api-server/auth/callback?code=mock_code&state=%s", authReq.State), nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var res map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	matrix, ok := res["matrix"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing matrix session in response: %+v", res)
	}

	if matrix["access_token"] != "syt_mock_matrix_token_12345" {
		t.Errorf("expected access_token 'syt_mock_matrix_token_12345', got %v", matrix["access_token"])
	}
	if matrix["user_id"] != "@htp_testuser:localhost" {
		t.Errorf("expected user_id '@htp_testuser:localhost', got %v", matrix["user_id"])
	}

	user, ok := res["user"].(map[string]interface{})
	if !ok || user["sub"] != "testuser" {
		t.Errorf("expected user.sub 'testuser', got %+v", res["user"])
	}
}

func TestAuthHandler_MobileLogin(t *testing.T) {
	mockServer, cfg, oidcSvc, adminClient := setupOIDCAndSynapseMock(t)
	defer mockServer.Close()

	authHandler := handler.NewAuthHandler(oidcSvc, adminClient, cfg, nil)
	router := setupAuthRouter(authHandler)

	t.Run("MobileLogin success with Authorization Bearer header", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/api-server/auth/mobile-login", bytes.NewReader([]byte("{}")))
		req.Header.Set("Authorization", "Bearer valid_mobile_token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		matrix, ok := res["matrix"].(map[string]interface{})
		if !ok || matrix["access_token"] != "syt_mock_matrix_token_12345" {
			t.Errorf("expected valid matrix token from mobile login, got %v", matrix["access_token"])
		}

		redirectTo, ok := res["redirect_to"].(string)
		if !ok || !strings.Contains(redirectTo, "auth_token=") || !strings.Contains(redirectTo, "room_id=") {
			t.Errorf("expected valid redirect_to with auth_token and room_id, got %v", res["redirect_to"])
		}
	})

	t.Run("MobileLogin success with JSON body token", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"token": "valid_mobile_token"})
		req, _ := http.NewRequest(http.MethodPost, "/api-server/auth/mobile-login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		matrix, ok := res["matrix"].(map[string]interface{})
		if !ok || matrix["access_token"] != "syt_mock_matrix_token_12345" {
			t.Errorf("expected valid matrix token from mobile login, got %v", matrix["access_token"])
		}
	})

	t.Run("MobileLogin fails with missing token", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/api-server/auth/mobile-login", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d: %s", w.Code, w.Body.String())
		}
	})
}
