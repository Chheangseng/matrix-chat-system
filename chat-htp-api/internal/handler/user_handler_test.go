package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/handler"
	"chat-htp-api/internal/synapse"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupMockSynapse(t *testing.T) (*httptest.Server, *synapse.AdminClient) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Handle login
		if r.Method == http.MethodPost && r.URL.Path == "/_matrix/client/v3/login" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"mock_admin_token"}`))
			return
		}

		// Verify auth header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer mock_admin_token" && auth != "Bearer custom_user_token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"errcode":"M_UNAUTHORIZED","error":"Invalid token"}`))
			return
		}

		// Handle Create / Update User (PUT)
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v2/users/") {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{
				"name": "@testuser:localhost",
				"admin": false,
				"deactivated": false,
				"displayname": "Test User"
			}`))
			return
		}

		// Handle Get User (GET)
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v2/users/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"name": "@testuser:localhost",
				"admin": false,
				"deactivated": false,
				"displayname": "Test User"
			}`))
			return
		}

		// Handle List Users (GET)
		if r.Method == http.MethodGet && r.URL.Path == "/_synapse/admin/v2/users" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"users": [
					{"name": "@testuser:localhost", "admin": false, "displayname": "Test User"}
				],
				"total": 1
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))

	parsedURL, _ := url.Parse(server.URL)
	cfg := &config.Config{
		SynapseURL:        parsedURL,
		SynapseServerName: "localhost",
		AdminUser:         "admin",
		AdminPassword:     "password",
		AdminToken:        "",
	}

	adminClient := synapse.NewAdminClient(cfg)
	return server, adminClient
}

func TestUserHandler_CreateUser(t *testing.T) {
	mockServer, adminClient := setupMockSynapse(t)
	defer mockServer.Close()

	userHandler := handler.NewUserHandler(adminClient, nil)
	router := gin.New()
	router.POST("/api-server/users", userHandler.CreateUser)

	t.Run("Create user successfully", func(t *testing.T) {
		body := `{"username":"testuser","password":"testpassword123","display_name":"Test User"}`
		req, _ := http.NewRequest(http.MethodPost, "/api-server/users", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
		}

		var user synapse.SynapseUser
		if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if user.Name != "@testuser:localhost" {
			t.Errorf("expected name @testuser:localhost, got %s", user.Name)
		}
	})

	t.Run("Create user missing required fields", func(t *testing.T) {
		body := `{"display_name":"Test User"}`
		req, _ := http.NewRequest(http.MethodPost, "/api-server/users", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request, got %d", rec.Code)
		}
	})
}

func TestUserHandler_GetUser(t *testing.T) {
	mockServer, adminClient := setupMockSynapse(t)
	defer mockServer.Close()

	userHandler := handler.NewUserHandler(adminClient, nil)
	router := gin.New()
	router.GET("/api-server/users/:id", userHandler.GetUser)

	t.Run("Get user by localpart", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api-server/users/testuser", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var user synapse.SynapseUser
		if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if user.Name != "@testuser:localhost" {
			t.Errorf("expected name @testuser:localhost, got %s", user.Name)
		}
	})

	t.Run("Get user by full matrix ID", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api-server/users/@testuser:localhost", nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestUserHandler_ListUsers(t *testing.T) {
	mockServer, adminClient := setupMockSynapse(t)
	defer mockServer.Close()

	userHandler := handler.NewUserHandler(adminClient, nil)
	router := gin.New()
	router.GET("/api-server/users", userHandler.ListUsers)

	req, _ := http.NewRequest(http.MethodGet, "/api-server/users?limit=10", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var listResp synapse.UserListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Users) != 1 {
		t.Errorf("expected 1 user, got %d", listResp.Total)
	}
}
