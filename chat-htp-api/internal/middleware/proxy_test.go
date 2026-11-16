package middleware_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"chat-htp-api/internal/middleware"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestMatrixProxy_Forwarding(t *testing.T) {
	// Create mock Matrix backend server
	backendReceivedHeader := ""
	backendReceivedHost := ""
	backendReceivedForwardedHost := ""
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendReceivedHeader = r.Header.Get("X-Forwarded-Proto")
		backendReceivedHost = r.Host
		backendReceivedForwardedHost = r.Header.Get("X-Forwarded-Host")

		if r.URL.Path == "/_matrix/client/versions" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Server", "Synapse/1.161.0")
			w.Header().Set("Synapse-Trace-Id", "trace-xyz-123")
			w.Header().Set("Access-Control-Expose-Headers", "Synapse-Trace-Id, Server, Content-Type")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"versions":["v1.12"]}`))
			return
		}

		if r.URL.Path == "/redirect-test" {
			w.Header().Set("Location", "http://"+r.Host+"/redirect-target")
			w.WriteHeader(http.StatusFound)
			return
		}

		if r.Method == http.MethodPost && r.URL.Path == "/_matrix/client/v3/login" {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("failed to parse backend URL: %v", err)
	}

	router := gin.New()
	router.NoRoute(middleware.MatrixProxy(backendURL))

	t.Run("GET /_matrix/client/versions forwards properly and conceals backend headers", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/_matrix/client/versions", nil)
		req.Host = "public-chat.example.com"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "v1.12") {
			t.Fatalf("expected body to contain v1.12, got %s", rec.Body.String())
		}
		if backendReceivedHeader != "http" {
			t.Errorf("expected X-Forwarded-Proto http, got %s", backendReceivedHeader)
		}
		if backendReceivedHost != backendURL.Host {
			t.Errorf("expected Host %s, got %s", backendURL.Host, backendReceivedHost)
		}
		if backendReceivedForwardedHost != "public-chat.example.com" {
			t.Errorf("expected X-Forwarded-Host public-chat.example.com, got %s", backendReceivedForwardedHost)
		}

		// Verify backend server headers are stripped
		if serverHeader := rec.Header().Get("Server"); serverHeader != "" {
			t.Errorf("expected Server header to be stripped, got %s", serverHeader)
		}
		if traceHeader := rec.Header().Get("Synapse-Trace-Id"); traceHeader != "" {
			t.Errorf("expected Synapse-Trace-Id header to be stripped, got %s", traceHeader)
		}
		if expose := rec.Header().Get("Access-Control-Expose-Headers"); strings.Contains(expose, "Synapse") || strings.Contains(expose, "Server") {
			t.Errorf("expected exposed headers to be sanitized, got %s", expose)
		}
	})

	t.Run("Redirect Location header is rewritten to conceal backend address", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/redirect-test", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Fatalf("expected 302, got %d", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if strings.Contains(loc, backendURL.Host) {
			t.Fatalf("Location header leaked backend host: %s", loc)
		}
		if loc != "/redirect-target" {
			t.Errorf("expected relative /redirect-target, got %s", loc)
		}
	})

	t.Run("POST with body forwards transparently", func(t *testing.T) {
		payload := `{"type":"m.login.password","user":"test"}`
		req, _ := http.NewRequest(http.MethodPost, "/_matrix/client/v3/login", strings.NewReader(payload))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if rec.Body.String() != payload {
			t.Fatalf("expected echoed body, got %s", rec.Body.String())
		}
	})
}

func TestMatrixProxy_BadGateway(t *testing.T) {
	// Point to unreachable port
	unreachableURL, _ := url.Parse("http://127.0.0.1:54321")

	router := gin.New()
	router.NoRoute(middleware.MatrixProxy(unreachableURL))

	req, _ := http.NewRequest(http.MethodGet, "/_matrix/client/versions", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway, got %d", rec.Code)
	}

	var errResp middleware.MatrixErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if errResp.ErrCode != "M_BAD_GATEWAY" {
		t.Errorf("expected errcode M_BAD_GATEWAY, got %s", errResp.ErrCode)
	}
	// Verify error message doesn't leak internal IP or port
	if strings.Contains(errResp.Error, "54321") || strings.Contains(errResp.Error, "127.0.0.1") {
		t.Errorf("error message leaked backend address: %s", errResp.Error)
	}
}
