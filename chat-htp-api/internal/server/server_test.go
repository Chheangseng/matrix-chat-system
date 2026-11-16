package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/server"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestServerRouting(t *testing.T) {
	// Mock Matrix Synapse backend
	backendHits := make(map[string]int)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendHits[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"proxied_path": r.URL.Path,
		})
	}))
	defer backend.Close()

	backendURL, _ := url.Parse(backend.URL)
	cfg := &config.Config{
		Port:           "8080",
		Environment:    "test",
		SynapseURL:     backendURL,
		AllowedOrigins: "*",
	}

	srv := server.New(cfg)

	tests := []struct {
		name              string
		path              string
		method            string
		expectedStatus    int
		shouldHitBackend  bool
		expectedBodyField string
	}{
		{
			name:              "Custom API health check",
			path:              "/api-server/healthz",
			method:            http.MethodGet,
			expectedStatus:    http.StatusOK,
			shouldHitBackend:  false,
			expectedBodyField: "status",
		},
		{
			name:              "Custom API info",
			path:              "/api-server/info",
			method:            http.MethodGet,
			expectedStatus:    http.StatusOK,
			shouldHitBackend:  false,
			expectedBodyField: "version",
		},
		{
			name:              "Well-known client discovery served by gateway to hide synapse backend",
			path:              "/.well-known/matrix/client",
			method:            http.MethodGet,
			expectedStatus:    http.StatusOK,
			shouldHitBackend:  false,
			expectedBodyField: "m.homeserver",
		},
		{
			name:              "Matrix versions endpoint proxied",
			path:              "/_matrix/client/versions",
			method:            http.MethodGet,
			expectedStatus:    http.StatusOK,
			shouldHitBackend:  true,
			expectedBodyField: "proxied_path",
		},
		{
			name:              "Matrix login endpoint proxied",
			path:              "/_matrix/client/v3/login",
			method:            http.MethodPost,
			expectedStatus:    http.StatusOK,
			shouldHitBackend:  true,
			expectedBodyField: "proxied_path",
		},
		{
			name:              "Synapse admin endpoint proxied",
			path:              "/_synapse/admin/v1/users",
			method:            http.MethodGet,
			expectedStatus:    http.StatusOK,
			shouldHitBackend:  true,
			expectedBodyField: "proxied_path",
		},
		{
			name:              "Root path proxied",
			path:              "/",
			method:            http.MethodGet,
			expectedStatus:    http.StatusOK,
			shouldHitBackend:  true,
			expectedBodyField: "proxied_path",
		},
		{
			name:              "Arbitrary other path proxied",
			path:              "/custom-other/resource",
			method:            http.MethodGet,
			expectedStatus:    http.StatusOK,
			shouldHitBackend:  true,
			expectedBodyField: "proxied_path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, tc.path, nil)
			req.Host = "chat.example.com"
			rec := httptest.NewRecorder()
			srv.Engine.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("[%s] expected status %d, got %d", tc.name, tc.expectedStatus, rec.Code)
			}

			var bodyMap map[string]interface{}
			_ = json.Unmarshal(rec.Body.Bytes(), &bodyMap)

			if _, ok := bodyMap[tc.expectedBodyField]; !ok {
				t.Errorf("[%s] expected field %q in response, got %v", tc.name, tc.expectedBodyField, bodyMap)
			}

			if tc.shouldHitBackend && backendHits[tc.path] == 0 {
				t.Errorf("[%s] expected request to hit backend for path %s, but backend was not called", tc.name, tc.path)
			} else if !tc.shouldHitBackend && backendHits[tc.path] > 0 {
				t.Errorf("[%s] expected request NOT to hit backend for path %s, but backend was called", tc.name, tc.path)
			}
		})
	}
}
