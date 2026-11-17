package oidc_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/oidc"
)

func TestFilterAllowedScopes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Default when empty",
			input:    "",
			expected: "openid profile phone",
		},
		{
			name:     "Exact match space separated",
			input:    "openid profile phone",
			expected: "openid profile phone",
		},
		{
			name:     "Exact match comma separated",
			input:    "openid,profile,phone",
			expected: "openid profile phone",
		},
		{
			name:     "Strips unallowed scopes like email, offline_access, admin",
			input:    "openid email profile offline_access phone admin",
			expected: "openid profile phone",
		},
		{
			name:     "Input only unallowed scopes defaults to allowed set",
			input:    "email address offline_access",
			expected: "openid profile phone",
		},
		{
			name:     "Only profile and phone will auto-include openid",
			input:    "profile,phone",
			expected: "openid profile phone",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := oidc.FilterAllowedScopes(tc.input)
			if got != tc.expected {
				t.Errorf("FilterAllowedScopes(%q) = %q, expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestPKCEGeneration(t *testing.T) {
	cfg := &config.Config{
		HTPOIDCIssuer:    "http://mock-issuer:5054",
		OIDCClientID:     "test-client-id",
		OIDCClientSecret: "test-client-secret",
		OIDCRedirectURI:  "http://localhost:8080/api-server/auth/callback",
	}
	svc := oidc.NewService(cfg)

	verifier, challenge, err := svc.GeneratePKCE()
	if err != nil {
		t.Fatalf("unexpected error generating pkce: %v", err)
	}

	if verifier == "" || challenge == "" {
		t.Fatalf("expected non-empty verifier and challenge")
	}

	// Verify SHA-256 match
	h := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	if challenge != expectedChallenge {
		t.Errorf("challenge mismatch: got %s, expected %s", challenge, expectedChallenge)
	}
}

func TestPKCEStore(t *testing.T) {
	store := oidc.NewPKCEStore()
	store.Set("state123", "verifierABC", "nonceXYZ")

	entry, ok := store.GetAndDelete("state123")
	if !ok || entry == nil {
		t.Fatalf("expected to find state123")
	}
	if entry.Verifier != "verifierABC" || entry.Nonce != "nonceXYZ" {
		t.Errorf("unexpected entry values: %+v", entry)
	}

	// Double retrieval should return false
	_, ok = store.GetAndDelete("state123")
	if ok {
		t.Errorf("expected state123 to be deleted after retrieval")
	}
}

func TestCreateAuthorizeRequest(t *testing.T) {
	cfg := &config.Config{
		HTPOIDCIssuer:    "http://mock-issuer:5054",
		OIDCClientID:     "my-client-id",
		OIDCClientSecret: "my-secret",
		OIDCRedirectURI:  "http://localhost:8080/api-server/auth/callback",
	}
	svc := oidc.NewService(cfg)

	reqResult, err := svc.CreateAuthorizeRequest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reqResult.URL == "" || reqResult.State == "" || reqResult.Verifier == "" || reqResult.Nonce == "" {
		t.Fatalf("expected non-empty fields in AuthorizeRequestResult: %+v", reqResult)
	}

	parsed, err := url.Parse(reqResult.URL)
	if err != nil {
		t.Fatalf("failed to parse generated URL: %v", err)
	}

	q := parsed.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("expected response_type=code, got %s", q.Get("response_type"))
	}
	if q.Get("client_id") != "my-client-id" {
		t.Errorf("expected client_id=my-client-id, got %s", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://localhost:8080/api-server/auth/callback" {
		t.Errorf("expected redirect_uri match, got %s", q.Get("redirect_uri"))
	}
	if q.Get("scope") != "openid profile phone" {
		t.Errorf("expected scope 'openid profile phone', got %s", q.Get("scope"))
	}
	if q.Get("state") != reqResult.State {
		t.Errorf("expected state %s, got %s", reqResult.State, q.Get("state"))
	}
	if q.Get("nonce") != reqResult.Nonce {
		t.Errorf("expected nonce %s, got %s", reqResult.Nonce, q.Get("nonce"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("expected code_challenge_method=S256, got %s", q.Get("code_challenge_method"))
	}
}

func TestDecodeJWTPayload(t *testing.T) {
	// Construct a dummy JWT: header.payload.signature
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user123","name":"Test User","phone_number":"+84988888888"}`))
	dummyJWT := fmt.Sprintf("%s.%s.dummysig", header, payload)

	claims, err := oidc.DecodeJWTPayload(dummyJWT)
	if err != nil {
		t.Fatalf("unexpected error decoding payload: %v", err)
	}

	if claims["sub"] != "user123" {
		t.Errorf("expected sub user123, got %v", claims["sub"])
	}
	if claims["name"] != "Test User" {
		t.Errorf("expected name 'Test User', got %v", claims["name"])
	}
	if claims["phone_number"] != "+84988888888" {
		t.Errorf("expected phone_number '+84988888888', got %v", claims["phone_number"])
	}
}

func TestExchangeCode_Success(t *testing.T) {
	// Mock OIDC Server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/oauth2/token":
			// Verify basic auth
			user, pass, ok := r.BasicAuth()
			if !ok || user != "test-client" || pass != "test-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
				return
			}

			// Verify form parameters
			_ = r.ParseForm()
			if r.FormValue("grant_type") != "authorization_code" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant_type"}`))
				return
			}
			if r.FormValue("code") != "valid_code_123" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_code"}`))
				return
			}

			// Generate dummy ID token
			payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"htp-user-99","name":"HTP Claim Name"}`))
			idToken := fmt.Sprintf("eyJhbGciOiJIUzI1NiJ9.%s.signature", payload)

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock_access_token_xyz",
				"id_token":     idToken,
				"token_type":   "Bearer",
				"expires_in":   3600,
			})

		case "/oauth2/userinfo":
			auth := r.Header.Get("Authorization")
			if auth != "Bearer mock_access_token_xyz" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"sub":          "htp-user-99",
				"name":         "HTP User Info",
				"nickname":     "htpnick",
				"phone_number": "+84912345678",
				"picture":      "https://example.com/avatar.png",
			})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		HTPOIDCIssuer:    mockServer.URL,
		OIDCClientID:     "test-client",
		OIDCClientSecret: "test-secret",
		OIDCRedirectURI:  "http://localhost:8080/api-server/auth/callback",
	}
	svc := oidc.NewService(cfg)
	svc.SetHTTPClient(mockServer.Client())

	// Initialize authorize request to register state
	authReq, err := svc.CreateAuthorizeRequest()
	if err != nil {
		t.Fatalf("failed to create authorize request: %v", err)
	}

	profile, err := svc.ExchangeCode(context.Background(), "valid_code_123", authReq.State)
	if err != nil {
		t.Fatalf("unexpected exchange error: %v", err)
	}

	if profile.Sub != "htp-user-99" {
		t.Errorf("expected sub 'htp-user-99', got %s", profile.Sub)
	}
	if profile.Name != "HTP User Info" {
		t.Errorf("expected name 'HTP User Info', got %s", profile.Name)
	}
	if profile.Nickname != "htpnick" {
		t.Errorf("expected nickname 'htpnick', got %s", profile.Nickname)
	}
	if profile.PhoneNumber != "+84912345678" {
		t.Errorf("expected phone '+84912345678', got %s", profile.PhoneNumber)
	}
	if profile.Picture != "https://example.com/avatar.png" {
		t.Errorf("expected picture 'https://example.com/avatar.png', got %s", profile.Picture)
	}
	if profile.AccessToken != "mock_access_token_xyz" {
		t.Errorf("expected access_token 'mock_access_token_xyz', got %s", profile.AccessToken)
	}
}

func TestSimulateMobilePreAuth_Success(t *testing.T) {
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/authorize":
			// Return 302 redirect with login_challenge
			challenge := "mock_challenge_abc_123"
			redirectURL := fmt.Sprintf("%s/login?login_challenge=%s", mockServer.URL, challenge)
			http.Redirect(w, r, redirectURL, http.StatusFound)

		case strings.HasPrefix(r.URL.Path, "/api/v2/oidc/login-requests/"):
			auth := r.Header.Get("Authorization")
			if auth != "Bearer valid_mobile_bearer_token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"data": {
					"redirect_to": "http://localhost:8080/api-server/auth/callback?code=mobile_auth_code_999&state=mock_state"
				}
			}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	cfg := &config.Config{
		HTPOIDCIssuer:    mockServer.URL,
		OIDCClientID:     "test-client",
		OIDCClientSecret: "test-secret",
		OIDCRedirectURI:  "http://localhost:8080/api-server/auth/callback",
	}
	svc := oidc.NewService(cfg)
	svc.SetHTTPClient(mockServer.Client())

	res, err := svc.SimulateMobilePreAuth(context.Background(), "Bearer valid_mobile_bearer_token")
	if err != nil {
		t.Fatalf("unexpected error in pre-auth: %v", err)
	}

	if !res.Success {
		t.Errorf("expected Success true, got false")
	}
	if res.Challenge != "mock_challenge_abc_123" {
		t.Errorf("expected challenge 'mock_challenge_abc_123', got %s", res.Challenge)
	}
	if res.Code != "mobile_auth_code_999" {
		t.Errorf("expected code 'mobile_auth_code_999', got %s", res.Code)
	}
	if !strings.Contains(res.RedirectTo, "mobile_auth_code_999") {
		t.Errorf("expected redirect_to containing code, got %s", res.RedirectTo)
	}
}
