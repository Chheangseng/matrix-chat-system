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
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if pw, ok := body["password"].(string); ok && pw != "Pass_testuser_Secret987!" {
				t.Errorf("expected deterministic password Pass_testuser_Secret987!, got %s", pw)
			}
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

func setupAuthRouter(authHandler *handler.AuthHandler) *gin.Engine {
	r := gin.New()
	api := r.Group("/api-server/auth")
	{
		api.GET("/login", authHandler.Login)
		api.GET("/callback", authHandler.Callback)
		api.POST("/exchange", authHandler.Exchange)
		api.POST("/pre-auth", authHandler.PreAuth)
		api.POST("/mobile-login", authHandler.MobileLogin)
		api.POST("/support-login", authHandler.SupportLogin)
	}
	return r
}

func TestAuthHandler_Login(t *testing.T) {
	mockServer, cfg, oidcSvc, adminClient := setupOIDCAndSynapseMock(t)
	defer mockServer.Close()

	authHandler := handler.NewAuthHandler(oidcSvc, adminClient, cfg, nil)
	router := setupAuthRouter(authHandler)

	t.Run("Redirect to authorize URL by default", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api-server/auth/login", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusFound {
			t.Errorf("expected 302 redirect, got %d", w.Code)
		}
		loc := w.Header().Get("Location")
		if !strings.Contains(loc, "/oauth2/authorize") {
			t.Errorf("expected redirect location containing /oauth2/authorize, got %s", loc)
		}
	})

	t.Run("Return JSON authorize URL when format=json query is set", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api-server/auth/login?format=json", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d", w.Code)
		}

		var res map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["authorize_url"] == nil || res["state"] == nil {
			t.Errorf("expected authorize_url and state in response: %+v", res)
		}
	})
}

func TestAuthHandler_Callback_Success(t *testing.T) {
	mockServer, cfg, oidcSvc, adminClient := setupOIDCAndSynapseMock(t)
	defer mockServer.Close()

	authHandler := handler.NewAuthHandler(oidcSvc, adminClient, cfg, nil)
	router := setupAuthRouter(authHandler)

	// Pre-create authorize request to register state
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

func TestAuthHandler_Exchange_Success(t *testing.T) {
	mockServer, cfg, oidcSvc, adminClient := setupOIDCAndSynapseMock(t)
	defer mockServer.Close()

	authHandler := handler.NewAuthHandler(oidcSvc, adminClient, cfg, nil)
	router := setupAuthRouter(authHandler)

	authReq, _ := oidcSvc.CreateAuthorizeRequest()

	bodyBytes, _ := json.Marshal(map[string]string{
		"code":  "mock_code",
		"state": authReq.State,
	})

	req, _ := http.NewRequest(http.MethodPost, "/api-server/auth/exchange", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var res map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	matrix := res["matrix"].(map[string]interface{})
	if matrix["access_token"] != "syt_mock_matrix_token_12345" {
		t.Errorf("expected valid matrix token, got %v", matrix["access_token"])
	}
}

func TestAuthHandler_MobileLogin(t *testing.T) {
	mockServer, cfg, oidcSvc, adminClient := setupOIDCAndSynapseMock(t)
	defer mockServer.Close()

	authHandler := handler.NewAuthHandler(oidcSvc, adminClient, cfg, nil)
	router := setupAuthRouter(authHandler)

	// Send request with Bearer token
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
	matrix := res["matrix"].(map[string]interface{})
	if matrix["access_token"] != "syt_mock_matrix_token_12345" {
		t.Errorf("expected valid matrix token from mobile login, got %v", matrix["access_token"])
	}

	redirectTo, ok := res["redirect_to"].(string)
	if !ok || !strings.Contains(redirectTo, "auth_token=") || !strings.Contains(redirectTo, "room_id=") {
		t.Errorf("expected valid redirect_to with auth_token and room_id, got %v", res["redirect_to"])
	}
}

func TestAuthHandler_CustomerSupportRoom_CreationAndDedup(t *testing.T) {
	var createRoomCalls int
	var storedDirectData = make(map[string]map[string][]string)

	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/_matrix/client/v3/login":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"mock_admin_token"}`))

		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v2/users/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"@htp_testuser:localhost","displayname":"HTP Test User"}`))

		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v1/users/") && strings.HasSuffix(r.URL.Path, "/login"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"syt_user_token","user_id":"@htp_testuser:localhost","home_server":"localhost"}`))

		case r.Method == http.MethodPost && r.URL.Path == "/oauth2/token":
			payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"testuser","name":"HTP Test User"}`))
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "oidc_token",
				"id_token":     fmt.Sprintf("header.%s.sig", payload),
				"token_type":   "Bearer",
			})

		case r.Method == http.MethodGet && r.URL.Path == "/oauth2/userinfo":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"sub":  "testuser",
				"name": "HTP Test User",
			})

		case r.Method == http.MethodPost && r.URL.Path == "/_matrix/client/v3/createRoom":
			createRoomCalls++
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["is_direct"] != true {
				t.Errorf("expected is_direct to be true")
			}
			if body["preset"] != "trusted_private_chat" {
				t.Errorf("expected preset trusted_private_chat, got %v", body["preset"])
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"room_id":"!mock_support_dm_room:localhost"}`))

		case r.Method == http.MethodPost && (strings.HasPrefix(r.URL.Path, "/_synapse/admin/v1/join/") || strings.HasPrefix(r.URL.Path, "/_synapse/admin/v1/rooms/")):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"room_id":"!mock_support_dm_room:localhost"}`))

		case strings.Contains(r.URL.Path, "/account_data/m.direct"):
			userPart := r.URL.Path
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusOK)
				data := storedDirectData[userPart]
				if data == nil {
					data = make(map[string][]string)
				}
				_ = json.NewEncoder(w).Encode(data)
			} else if r.Method == http.MethodPut {
				var data map[string][]string
				_ = json.NewDecoder(r.Body).Decode(&data)
				storedDirectData[userPart] = data
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v1/users/") && strings.HasSuffix(r.URL.Path, "/joined_rooms"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"joined_rooms":[],"total":0}`))

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v1/rooms/") && strings.HasSuffix(r.URL.Path, "/members"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"members":[],"total":0}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

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

	authHandler := handler.NewAuthHandler(oidcSvc, adminClient, cfg, nil)
	router := setupAuthRouter(authHandler)

	// --- 1st Login: Must create the 1-on-1 direct message room ---
	reqBody1, _ := json.Marshal(map[string]string{"code": "code_1", "state": "state_1"})
	req1, _ := http.NewRequest(http.MethodPost, "/api-server/auth/exchange", bytes.NewReader(reqBody1))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first exchange failed: %d, body: %s", w1.Code, w1.Body.String())
	}

	var res1 map[string]interface{}
	_ = json.Unmarshal(w1.Body.Bytes(), &res1)
	if res1["support_room_id"] != "!mock_support_dm_room:localhost" {
		t.Errorf("expected support_room_id '!mock_support_dm_room:localhost', got %v", res1["support_room_id"])
	}

	if createRoomCalls != 1 {
		t.Fatalf("expected 1 createRoom call, got %d", createRoomCalls)
	}

	// --- 2nd Login for same user: Must reuse existing room without calling createRoom again ---
	reqBody2, _ := json.Marshal(map[string]string{"code": "code_2", "state": "state_2"})
	req2, _ := http.NewRequest(http.MethodPost, "/api-server/auth/exchange", bytes.NewReader(reqBody2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second exchange failed: %d, body: %s", w2.Code, w2.Body.String())
	}

	var res2 map[string]interface{}
	_ = json.Unmarshal(w2.Body.Bytes(), &res2)
	if res2["support_room_id"] != "!mock_support_dm_room:localhost" {
		t.Errorf("expected reused support_room_id '!mock_support_dm_room:localhost', got %v", res2["support_room_id"])
	}

	if createRoomCalls != 1 {
		t.Errorf("expected createRoom to still be called only once (idempotent), got %d calls", createRoomCalls)
	}
}

func TestAuthHandler_SupportLogin(t *testing.T) {
	mockServer, cfg, oidcSvc, adminClient := setupOIDCAndSynapseMock(t)
	defer mockServer.Close()

	authHandler := handler.NewAuthHandler(oidcSvc, adminClient, cfg, nil)
	router := setupAuthRouter(authHandler)

	t.Run("Authorized support staff login succeeds", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]string{
			"username": "support",
			"password": "support_password_123",
		})
		req, _ := http.NewRequest(http.MethodPost, "/api-server/auth/support-login", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var res map[string]interface{}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		matrix, ok := res["matrix"].(map[string]interface{})
		if !ok || matrix["access_token"] == "" {
			t.Errorf("expected matrix access token in response, got %+v", res)
		}
		user, ok := res["user"].(map[string]interface{})
		if !ok || user["role"] != "support_agent" {
			t.Errorf("expected user role support_agent, got %+v", user)
		}
	})

	t.Run("Unauthorized regular user is rejected with 403 Forbidden", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]string{
			"username": "regular_customer",
			"password": "customer_password",
		})
		req, _ := http.NewRequest(http.MethodPost, "/api-server/auth/support-login", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden, got %d: %s", w.Code, w.Body.String())
		}
	})
}
