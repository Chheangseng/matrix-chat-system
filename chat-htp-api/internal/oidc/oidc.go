package oidc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"chat-htp-api/internal/config"
)

// PKCEEntry stores verifier, nonce and creation time for a pending state
type PKCEEntry struct {
	Verifier  string
	Nonce     string
	CreatedAt time.Time
}

// PKCEStore provides thread-safe in-memory storage for PKCE verification
type PKCEStore struct {
	mu      sync.RWMutex
	entries map[string]*PKCEEntry
}

// NewPKCEStore creates and starts a new PKCEStore with automatic TTL cleanup
func NewPKCEStore() *PKCEStore {
	store := &PKCEStore{
		entries: make(map[string]*PKCEEntry),
	}
	go store.cleanupLoop()
	return store
}

func (s *PKCEStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for state, entry := range s.entries {
			if now.Sub(entry.CreatedAt) > 15*time.Minute {
				delete(s.entries, state)
			}
		}
		s.mu.Unlock()
	}
}

// Set saves state and its PKCE verifier
func (s *PKCEStore) Set(state, verifier, nonce string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[state] = &PKCEEntry{
		Verifier:  verifier,
		Nonce:     nonce,
		CreatedAt: time.Now(),
	}
}

// GetAndDelete retrieves and removes the PKCEEntry for a given state
func (s *PKCEStore) GetAndDelete(state string) (*PKCEEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[state]
	if ok {
		delete(s.entries, state)
		if time.Since(entry.CreatedAt) > 15*time.Minute {
			return nil, false
		}
		return entry, true
	}
	return nil, false
}

// AuthorizeRequestResult contains the generated authorization URL, state, and PKCE details
type AuthorizeRequestResult struct {
	URL      string `json:"url"`
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
}

// UserProfile contains standard OIDC identity claims
type UserProfile struct {
	Sub         string `json:"sub"`
	Name        string `json:"name"`
	Nickname    string `json:"nickname,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Picture     string `json:"picture,omitempty"`
	AccessToken string `json:"access_token,omitempty"`
	IDToken     string `json:"id_token,omitempty"`
}

// MobilePreAuthResult represents the result of the seamless mobile pre-authorization flow
type MobilePreAuthResult struct {
	Challenge  string `json:"challenge"`
	RedirectTo string `json:"redirect_to"`
	State      string `json:"state"`
	Code       string `json:"code,omitempty"`
}

// AllowedScopes defines the strict whitelist of permitted OIDC scopes
var AllowedScopes = []string{"openid", "profile", "phone"}

var allowedScopesMap = map[string]bool{
	"openid":  true,
	"profile": true,
	"phone":   true,
}

// FilterAllowedScopes enforces that only "openid", "profile", and "phone" are permitted.
// Any other requested scopes are strictly stripped out.
// If the input is empty or contains no allowed scopes, it defaults to "openid profile phone".
func FilterAllowedScopes(requested string) string {
	if strings.TrimSpace(requested) == "" {
		return "openid profile phone"
	}

	tokens := strings.FieldsFunc(requested, func(r rune) bool {
		return r == ' ' || r == ','
	})

	var filtered []string
	seen := make(map[string]bool)
	for _, tok := range tokens {
		t := strings.TrimSpace(strings.ToLower(tok))
		if allowedScopesMap[t] && !seen[t] {
			seen[t] = true
			filtered = append(filtered, t)
		}
	}

	if len(filtered) == 0 {
		return "openid profile phone"
	}

	// Ensure openid is always present as required by OIDC spec
	if !seen["openid"] {
		filtered = append([]string{"openid"}, filtered...)
	}

	return strings.Join(filtered, " ")
}

// Service provides OIDC authentication flows matching HTP OIDC Issuer
type Service struct {
	issuer       string
	clientID     string
	clientSecret string
	redirectURI  string
	scopes       string
	pkceStore    *PKCEStore
	httpClient   *http.Client
}

// NewService initializes a new OIDCService instance
func NewService(cfg *config.Config) *Service {
	issuer := strings.TrimRight(cfg.HTPOIDCIssuer, "/")
	if issuer == "" {
		issuer = "http://localhost:5054"
	}

	redirectURI := cfg.OIDCRedirectURI
	if redirectURI == "" {
		redirectURI = "http://localhost:8080/api-server/auth/callback"
	}

	return &Service{
		issuer:       issuer,
		clientID:     cfg.OIDCClientID,
		clientSecret: cfg.OIDCClientSecret,
		redirectURI:  redirectURI,
		scopes:       FilterAllowedScopes("openid profile phone"),
		pkceStore:    NewPKCEStore(),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// SetHTTPClient allows overriding the HTTP client for testing or custom proxies
func (s *Service) SetHTTPClient(client *http.Client) {
	s.httpClient = client
}

// Base64URL encodes bytes to base64url string without padding
func Base64URL(buf []byte) string {
	return base64.RawURLEncoding.EncodeToString(buf)
}

// GeneratePKCE creates a secure random code_verifier and its SHA256 code_challenge
func (s *Service) GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate random bytes for pkce: %w", err)
	}
	verifier = Base64URL(b)

	h := sha256.Sum256([]byte(verifier))
	challenge = Base64URL(h[:])
	return verifier, challenge, nil
}

// CreateAuthorizeRequest generates PKCE, state, nonce, and the authorization URL.
// Any requested custom scopes are strictly sanitized to allow only "openid", "profile", and "phone".
func (s *Service) CreateAuthorizeRequest(customScopes ...string) (*AuthorizeRequestResult, error) {
	stateBytes := make([]byte, 24)
	if _, err := rand.Read(stateBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random state: %w", err)
	}
	state := Base64URL(stateBytes)

	nonceBytes := make([]byte, 24)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random nonce: %w", err)
	}
	nonce := Base64URL(nonceBytes)

	verifier, challenge, err := s.GeneratePKCE()
	if err != nil {
		return nil, err
	}

	s.pkceStore.Set(state, verifier, nonce)

	scope := s.scopes
	if len(customScopes) > 0 && strings.TrimSpace(customScopes[0]) != "" {
		scope = FilterAllowedScopes(customScopes[0])
	} else {
		scope = FilterAllowedScopes(scope)
	}

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", s.clientID)
	params.Set("redirect_uri", s.redirectURI)
	params.Set("scope", scope)
	params.Set("state", state)
	params.Set("nonce", nonce)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")

	authURL := fmt.Sprintf("%s/oauth2/authorize?%s", s.issuer, params.Encode())

	return &AuthorizeRequestResult{
		URL:      authURL,
		State:    state,
		Verifier: verifier,
		Nonce:    nonce,
	}, nil
}

// DecodeJWTPayload extracts and parses the claims JSON payload from a JWT token
func DecodeJWTPayload(tokenStr string) (map[string]interface{}, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) < 2 {
		return nil, errors.New("invalid jwt token format: missing payload segment")
	}

	payloadSegment := parts[1]
	data, err := base64.RawURLEncoding.DecodeString(payloadSegment)
	if err != nil {
		// Fallback to padded decoding if needed
		if rem := len(payloadSegment) % 4; rem > 0 {
			payloadSegment += strings.Repeat("=", 4-rem)
		}
		data, err = base64.URLEncoding.DecodeString(payloadSegment)
		if err != nil {
			return nil, fmt.Errorf("failed to base64 decode jwt payload: %w", err)
		}
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, fmt.Errorf("failed to json unmarshal jwt payload: %w", err)
	}

	return claims, nil
}

// ExchangeCode exchanges an authorization code for access and ID tokens, and fetches userinfo
func (s *Service) ExchangeCode(ctx context.Context, code, state string) (*UserProfile, error) {
	if code == "" {
		return nil, errors.New("missing authorization code")
	}

	var verifier string
	if state != "" {
		if stored, ok := s.pkceStore.GetAndDelete(state); ok && stored != nil {
			verifier = stored.Verifier
		}
	}

	tokenParams := url.Values{}
	tokenParams.Set("grant_type", "authorization_code")
	tokenParams.Set("code", code)
	tokenParams.Set("redirect_uri", s.redirectURI)
	if verifier != "" {
		tokenParams.Set("code_verifier", verifier)
	}

	tokenURL := fmt.Sprintf("%s/oauth2/token", s.issuer)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(tokenParams.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.clientID, s.clientSecret)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc token exchange returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	// Decode ID token payload claims
	claims := make(map[string]interface{})
	if tokenResp.IDToken != "" {
		if c, err := DecodeJWTPayload(tokenResp.IDToken); err == nil {
			claims = c
		} else {
			log.Printf("[WARN] Failed to parse id_token payload: %v", err)
		}
	}

	// Fetch /oauth2/userinfo
	userinfo := make(map[string]interface{})
	if tokenResp.AccessToken != "" {
		userinfoURL := fmt.Sprintf("%s/oauth2/userinfo", s.issuer)
		userinfoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoURL, nil)
		if err == nil {
			userinfoReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
			if uResp, err := s.httpClient.Do(userinfoReq); err == nil {
				defer uResp.Body.Close()
				if uResp.StatusCode == http.StatusOK {
					_ = json.NewDecoder(uResp.Body).Decode(&userinfo)
				}
			} else {
				log.Printf("[WARN] Failed to fetch /oauth2/userinfo: %v", err)
			}
		}
	}

	getStringClaim := func(primary, fallback string) string {
		if val, ok := userinfo[primary].(string); ok && strings.TrimSpace(val) != "" {
			return val
		}
		if val, ok := claims[fallback].(string); ok && strings.TrimSpace(val) != "" {
			return val
		}
		return ""
	}

	sub := getStringClaim("sub", "sub")
	name := getStringClaim("name", "name")
	if name == "" {
		name = getStringClaim("nickname", "nickname")
	}
	if name == "" && sub != "" {
		name = fmt.Sprintf("HTP User %s", sub)
	}

	nickname := getStringClaim("nickname", "nickname")
	phone := getStringClaim("phone_number", "phone_number")
	picture := getStringClaim("picture", "picture")

	return &UserProfile{
		Sub:         sub,
		Name:        name,
		Nickname:    nickname,
		PhoneNumber: phone,
		Picture:     picture,
		AccessToken: tokenResp.AccessToken,
		IDToken:     tokenResp.IDToken,
	}, nil
}

// SimulateMobilePreAuth performs programmatic pre-authorization using user's existing HTP Bearer token
// and returns the challenge and redirect_to URL for seamless mobile app single sign-on.
func (s *Service) SimulateMobilePreAuth(ctx context.Context, htpBearerToken string) (*MobilePreAuthResult, error) {
	if strings.TrimSpace(htpBearerToken) == "" {
		return nil, errors.New("missing HTP bearer token")
	}

	authReq, err := s.CreateAuthorizeRequest()
	if err != nil {
		return nil, err
	}

	// 1. Hit GET /oauth2/authorize without following redirects to extract login_challenge
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authReq.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create authorize request: %w", err)
	}

	resp, err := noRedirectClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("authorize request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound &&
		resp.StatusCode != http.StatusSeeOther &&
		resp.StatusCode != http.StatusTemporaryRedirect &&
		resp.StatusCode != http.StatusPermanentRedirect {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected response from /oauth2/authorize (status %d): %s", resp.StatusCode, string(body))
	}

	locHeader := resp.Header.Get("Location")
	if locHeader == "" {
		return nil, errors.New("no Location header received in authorize redirect")
	}

	parsedLoc, err := url.Parse(locHeader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redirect location header: %w", err)
	}

	challenge := parsedLoc.Query().Get("login_challenge")
	if challenge == "" {
		return nil, errors.New("failed to extract login_challenge from HTP authorize redirect Location")
	}

	// 2. Call POST /api/v2/oidc/login-requests/:challenge/accept with user's token
	cleanToken := strings.TrimSpace(strings.TrimPrefix(htpBearerToken, "Bearer "))
	cleanToken = strings.TrimSpace(strings.TrimPrefix(cleanToken, "bearer "))

	acceptURL := fmt.Sprintf("%s/api/v2/oidc/login-requests/%s/accept", s.issuer, url.PathEscape(challenge))
	acceptReq, err := http.NewRequestWithContext(ctx, http.MethodPost, acceptURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("failed to create accept request: %w", err)
	}

	acceptReq.Header.Set("Authorization", "Bearer "+cleanToken)
	acceptReq.Header.Set("Content-Type", "application/json")

	acceptResp, err := s.httpClient.Do(acceptReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call accept login request: %w", err)
	}
	defer acceptResp.Body.Close()

	acceptBody, err := io.ReadAll(acceptResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read accept response body: %w", err)
	}

	if acceptResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("accept login request failed (status %d): %s", acceptResp.StatusCode, string(acceptBody))
	}

	var acceptData struct {
		Data struct {
			RedirectTo string `json:"redirect_to"`
		} `json:"data"`
	}
	if err := json.Unmarshal(acceptBody, &acceptData); err != nil {
		return nil, fmt.Errorf("failed to parse accept login response: %w", err)
	}

	redirectTo := acceptData.Data.RedirectTo
	if redirectTo == "" {
		return nil, fmt.Errorf("accept request succeeded but returned empty redirect_to: %s", string(acceptBody))
	}

	// Parse code from redirect_to if already present
	var code string
	if parsedRedirect, err := url.Parse(redirectTo); err == nil {
		code = parsedRedirect.Query().Get("code")
	}

	return &MobilePreAuthResult{
		Challenge:  challenge,
		RedirectTo: redirectTo,
		State:      authReq.State,
		Code:       code,
	}, nil
}
