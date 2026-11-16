package synapse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"chat-htp-api/internal/config"
)

// SynapseUser represents a Matrix user profile in Synapse Admin API
type SynapseUser struct {
	Name         string  `json:"name"`
	Admin        bool    `json:"admin"`
	Deactivated  bool    `json:"deactivated"`
	Locked       bool    `json:"locked,omitempty"`
	ShadowBanned bool    `json:"shadow_banned,omitempty"`
	CreationTS   int64   `json:"creation_ts,omitempty"`
	DisplayName  string  `json:"displayname,omitempty"`
	AvatarURL    string  `json:"avatar_url,omitempty"`
	UserType     *string `json:"user_type,omitempty"`
	IsGuest      bool    `json:"is_guest,omitempty"`
	Suspended    bool    `json:"suspended,omitempty"`
}

// CreateUserPayload represents the fields to create or modify a user
type CreateUserPayload struct {
	Password    string `json:"password,omitempty"`
	DisplayName string `json:"displayname,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	Admin       bool   `json:"admin,omitempty"`
	Deactivated bool   `json:"deactivated,omitempty"`
}

// UserListResponse represents paginated list of users from Synapse
type UserListResponse struct {
	Users     []SynapseUser `json:"users"`
	NextToken string        `json:"next_token,omitempty"`
	Total     int           `json:"total"`
}

// AdminAPIError represents an error returned by Synapse
type AdminAPIError struct {
	StatusCode int
	ErrCode    string `json:"errcode"`
	ErrorMsg   string `json:"error"`
}

func (e *AdminAPIError) Error() string {
	if e.ErrCode != "" {
		return fmt.Sprintf("synapse error (HTTP %d - %s): %s", e.StatusCode, e.ErrCode, e.ErrorMsg)
	}
	return fmt.Sprintf("synapse error (HTTP %d): %s", e.StatusCode, e.ErrorMsg)
}

// AdminClient interacts with Synapse's Admin API
type AdminClient struct {
	baseURL       string
	serverName    string
	adminUser     string
	adminPassword string
	cachedToken   string
	tokenFile     string
	mu            sync.RWMutex
	httpClient    *http.Client
}

// NewAdminClient creates a new Synapse Admin API client
func NewAdminClient(cfg *config.Config) *AdminClient {
	baseURL := strings.TrimRight(cfg.SynapseURL.String(), "/")
	tokenFile := ".admin_token"
	initialToken := cfg.AdminToken
	if initialToken == "" {
		if data, err := os.ReadFile(tokenFile); err == nil {
			initialToken = strings.TrimSpace(string(data))
		}
	}

	return &AdminClient{
		baseURL:       baseURL,
		serverName:    cfg.SynapseServerName,
		adminUser:     cfg.AdminUser,
		adminPassword: cfg.AdminPassword,
		cachedToken:   initialToken,
		tokenFile:     tokenFile,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// FormatUserID ensures the user ID follows the Matrix standard format (@localpart:server_name)
func (c *AdminClient) FormatUserID(input string) string {
	trimmed := strings.TrimSpace(input)
	if strings.HasPrefix(trimmed, "@") && strings.Contains(trimmed, ":") {
		return trimmed
	}
	localpart := strings.TrimPrefix(trimmed, "@")
	return fmt.Sprintf("@%s:%s", localpart, c.serverName)
}

// getAdminToken retrieves an admin access token (uses cached or dynamically logs in)
func (c *AdminClient) getAdminToken(ctx context.Context, overrideToken string) (string, error) {
	if overrideToken != "" {
		return overrideToken, nil
	}

	c.mu.RLock()
	if c.cachedToken != "" {
		token := c.cachedToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double check under write lock
	if c.cachedToken != "" {
		return c.cachedToken, nil
	}

	loginPayload := map[string]interface{}{
		"type": "m.login.password",
		"identifier": map[string]string{
			"type": "m.id.user",
			"user": c.adminUser,
		},
		"password": c.adminPassword,
	}

	jsonBytes, err := json.Marshal(loginPayload)
	if err != nil {
		return "", err
	}

	loginURL := fmt.Sprintf("%s/_matrix/client/v3/login", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Synapse login: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("synapse admin login failed (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var loginResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(bodyBytes, &loginResp); err != nil {
		return "", fmt.Errorf("failed to decode login response: %w", err)
	}

	c.cachedToken = loginResp.AccessToken
	if c.tokenFile != "" {
		_ = os.WriteFile(c.tokenFile, []byte(c.cachedToken), 0600)
	}
	return c.cachedToken, nil
}

func (c *AdminClient) invalidateToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedToken = ""
	if c.tokenFile != "" {
		_ = os.Remove(c.tokenFile)
	}
}

// doRequest sends an authenticated HTTP request to Synapse Admin API with automatic token retry
func (c *AdminClient) doRequest(ctx context.Context, token, method, path string, body interface{}) ([]byte, int, error) {
	authToken, err := c.getAdminToken(ctx, token)
	if err != nil {
		return nil, 0, err
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		bodyReader = bytes.NewReader(b)
	}

	reqURL := fmt.Sprintf("%s%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Authorization", "Bearer "+authToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	// If token expired, clear cache and retry once
	if resp.StatusCode == http.StatusUnauthorized && token == "" {
		c.invalidateToken()
		newAuthToken, err := c.getAdminToken(ctx, "")
		if err == nil {
			if body != nil {
				b, _ := json.Marshal(body)
				bodyReader = bytes.NewReader(b)
			}
			retryReq, _ := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
			retryReq.Header.Set("Authorization", "Bearer "+newAuthToken)
			if body != nil {
				retryReq.Header.Set("Content-Type", "application/json")
			}
			retryResp, err := c.httpClient.Do(retryReq)
			if err == nil {
				defer retryResp.Body.Close()
				retryBytes, _ := io.ReadAll(retryResp.Body)
				return retryBytes, retryResp.StatusCode, nil
			}
		}
	}

	return respBody, resp.StatusCode, nil
}

// CreateOrUpdateUser creates or updates a user via PUT /_synapse/admin/v2/users/<user_id>
func (c *AdminClient) CreateOrUpdateUser(ctx context.Context, token, rawUserID string, payload CreateUserPayload) (*SynapseUser, error) {
	fullUserID := c.FormatUserID(rawUserID)
	path := fmt.Sprintf("/_synapse/admin/v2/users/%s", url.PathEscape(fullUserID))

	respBytes, statusCode, err := c.doRequest(ctx, token, http.MethodPut, path, payload)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusCreated {
		var apiErr AdminAPIError
		apiErr.StatusCode = statusCode
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.ErrorMsg == "" {
			apiErr.ErrorMsg = string(respBytes)
		}
		return nil, &apiErr
	}

	var user SynapseUser
	if err := json.Unmarshal(respBytes, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUser queries a user's details via GET /_synapse/admin/v2/users/<user_id>
func (c *AdminClient) GetUser(ctx context.Context, token, rawUserID string) (*SynapseUser, error) {
	fullUserID := c.FormatUserID(rawUserID)
	path := fmt.Sprintf("/_synapse/admin/v2/users/%s", url.PathEscape(fullUserID))

	respBytes, statusCode, err := c.doRequest(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		var apiErr AdminAPIError
		apiErr.StatusCode = statusCode
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.ErrorMsg == "" {
			apiErr.ErrorMsg = string(respBytes)
		}
		return nil, &apiErr
	}

	var user SynapseUser
	if err := json.Unmarshal(respBytes, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// ListUsers queries multiple users via GET /_synapse/admin/v2/users
func (c *AdminClient) ListUsers(ctx context.Context, token, from string, limit int, guests, deactivated bool) (*UserListResponse, error) {
	vals := url.Values{}
	if from != "" {
		vals.Set("from", from)
	}
	if limit > 0 {
		vals.Set("limit", fmt.Sprintf("%d", limit))
	}
	vals.Set("guests", fmt.Sprintf("%t", guests))
	vals.Set("deactivated", fmt.Sprintf("%t", deactivated))

	path := fmt.Sprintf("/_synapse/admin/v2/users?%s", vals.Encode())
	respBytes, statusCode, err := c.doRequest(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		var apiErr AdminAPIError
		apiErr.StatusCode = statusCode
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.ErrorMsg == "" {
			apiErr.ErrorMsg = string(respBytes)
		}
		return nil, &apiErr
	}

	var listResp UserListResponse
	if err := json.Unmarshal(respBytes, &listResp); err != nil {
		return nil, err
	}
	return &listResp, nil
}

// MatrixLoginResponse represents the session response returned when logging in as a user
type MatrixLoginResponse struct {
	AccessToken   string `json:"access_token"`
	DeviceID      string `json:"device_id"`
	HomeServer    string `json:"home_server"`
	UserID        string `json:"user_id"`
	SupportRoomID string `json:"support_room_id,omitempty"`
}

// LoginAsUser logs in as a specified user using the Synapse Admin API (POST /_synapse/admin/v1/users/<user_id>/login)
// and returns an access token and session details without requiring the user's password.
func (c *AdminClient) LoginAsUser(ctx context.Context, token, rawUserID string) (*MatrixLoginResponse, error) {
	fullUserID := c.FormatUserID(rawUserID)
	path := fmt.Sprintf("/_synapse/admin/v1/users/%s/login", url.PathEscape(fullUserID))

	respBytes, statusCode, err := c.doRequest(ctx, token, http.MethodPost, path, map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		var apiErr AdminAPIError
		apiErr.StatusCode = statusCode
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.ErrorMsg == "" {
			apiErr.ErrorMsg = string(respBytes)
		}
		return nil, &apiErr
	}

	var loginResp MatrixLoginResponse
	if err := json.Unmarshal(respBytes, &loginResp); err != nil {
		return nil, err
	}
	return &loginResp, nil
}

// CreateDirectRoom creates a 1-on-1 private direct message room inviting the target user
func (c *AdminClient) CreateDirectRoom(ctx context.Context, userToken, targetUserID, name, topic string) (string, error) {
	createPayload := map[string]interface{}{
		"is_direct":  true,
		"preset":     "trusted_private_chat",
		"visibility": "private",
		"invite":     []string{c.FormatUserID(targetUserID)},
		"name":       name,
		"topic":      topic,
		"initial_state": []map[string]interface{}{
			{
				"type":      "m.room.guest_access",
				"state_key": "",
				"content":   map[string]string{"guest_access": "can_join"},
			},
			{
				"type":      "m.room.history_visibility",
				"state_key": "",
				"content":   map[string]string{"history_visibility": "shared"},
			},
		},
	}

	respBytes, statusCode, err := c.doRequest(ctx, userToken, http.MethodPost, "/_matrix/client/v3/createRoom", createPayload)
	if err != nil {
		return "", err
	}

	if statusCode != http.StatusOK {
		var apiErr AdminAPIError
		apiErr.StatusCode = statusCode
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.ErrorMsg == "" {
			apiErr.ErrorMsg = string(respBytes)
		}
		return "", &apiErr
	}

	var result struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", fmt.Errorf("failed to parse createRoom response: %w", err)
	}

	return result.RoomID, nil
}

// JoinUserToRoom forces a user to join a room via the Synapse Admin API
func (c *AdminClient) JoinUserToRoom(ctx context.Context, token, roomID, rawUserID string) error {
	fullUserID := c.FormatUserID(rawUserID)
	path := fmt.Sprintf("/_synapse/admin/v1/join/%s", url.PathEscape(roomID))
	payload := map[string]string{"user_id": fullUserID}

	respBytes, statusCode, err := c.doRequest(ctx, token, http.MethodPost, path, payload)
	if err != nil {
		return err
	}

	if statusCode == http.StatusOK {
		return nil
	}

	// Fallback to alternate room-specific admin join path if applicable
	altPath := fmt.Sprintf("/_synapse/admin/v1/rooms/%s/join", url.PathEscape(roomID))
	_, statusCode2, err2 := c.doRequest(ctx, token, http.MethodPost, altPath, payload)
	if err2 == nil && statusCode2 == http.StatusOK {
		return nil
	}

	var apiErr AdminAPIError
	apiErr.StatusCode = statusCode
	_ = json.Unmarshal(respBytes, &apiErr)
	if apiErr.ErrorMsg == "" {
		apiErr.ErrorMsg = string(respBytes)
	}
	return &apiErr
}

// GetUserJoinedRooms queries the room IDs that a user is currently joined to
func (c *AdminClient) GetUserJoinedRooms(ctx context.Context, token, rawUserID string) ([]string, error) {
	fullUserID := c.FormatUserID(rawUserID)
	path := fmt.Sprintf("/_synapse/admin/v1/users/%s/joined_rooms", url.PathEscape(fullUserID))

	respBytes, statusCode, err := c.doRequest(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		var apiErr AdminAPIError
		apiErr.StatusCode = statusCode
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.ErrorMsg == "" {
			apiErr.ErrorMsg = string(respBytes)
		}
		return nil, &apiErr
	}

	var resp struct {
		JoinedRooms []string `json:"joined_rooms"`
		Total       int      `json:"total"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	return resp.JoinedRooms, nil
}

// GetRoomMembers retrieves all user IDs joined in a given room
func (c *AdminClient) GetRoomMembers(ctx context.Context, token, roomID string) ([]string, error) {
	path := fmt.Sprintf("/_synapse/admin/v1/rooms/%s/members", url.PathEscape(roomID))

	respBytes, statusCode, err := c.doRequest(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		var apiErr AdminAPIError
		apiErr.StatusCode = statusCode
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.ErrorMsg == "" {
			apiErr.ErrorMsg = string(respBytes)
		}
		return nil, &apiErr
	}

	var resp struct {
		Members []string `json:"members"`
		Total   int      `json:"total"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, err
	}
	return resp.Members, nil
}

// GetUserDirectRooms queries a user's m.direct account data
func (c *AdminClient) GetUserDirectRooms(ctx context.Context, token, rawUserID string) (map[string][]string, error) {
	fullUserID := c.FormatUserID(rawUserID)
	path := fmt.Sprintf("/_matrix/client/v3/user/%s/account_data/m.direct", url.PathEscape(fullUserID))

	respBytes, statusCode, err := c.doRequest(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		return make(map[string][]string), nil
	}

	if statusCode != http.StatusOK {
		var apiErr AdminAPIError
		apiErr.StatusCode = statusCode
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.ErrorMsg == "" {
			apiErr.ErrorMsg = string(respBytes)
		}
		return nil, &apiErr
	}

	var directData map[string][]string
	if err := json.Unmarshal(respBytes, &directData); err != nil {
		return make(map[string][]string), nil
	}
	return directData, nil
}

// SetUserDirectRooms updates a user's m.direct account data
func (c *AdminClient) SetUserDirectRooms(ctx context.Context, token, rawUserID string, data map[string][]string) error {
	fullUserID := c.FormatUserID(rawUserID)
	path := fmt.Sprintf("/_matrix/client/v3/user/%s/account_data/m.direct", url.PathEscape(fullUserID))

	respBytes, statusCode, err := c.doRequest(ctx, token, http.MethodPut, path, data)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK {
		var apiErr AdminAPIError
		apiErr.StatusCode = statusCode
		_ = json.Unmarshal(respBytes, &apiErr)
		if apiErr.ErrorMsg == "" {
			apiErr.ErrorMsg = string(respBytes)
		}
		return &apiErr
	}
	return nil
}

// AddUserDirectRoom appends a room ID to the user's m.direct mapping for the specified target user
func (c *AdminClient) AddUserDirectRoom(ctx context.Context, token, rawUserID, targetUserID, roomID string) error {
	directData, err := c.GetUserDirectRooms(ctx, token, rawUserID)
	if err != nil {
		directData = make(map[string][]string)
	}

	fullTargetID := c.FormatUserID(targetUserID)
	rooms := directData[fullTargetID]
	for _, id := range rooms {
		if id == roomID {
			return nil // already recorded
		}
	}

	directData[fullTargetID] = append(rooms, roomID)
	return c.SetUserDirectRooms(ctx, token, rawUserID, directData)
}

// LoginWithPassword authenticates an arbitrary user via Matrix password login (POST /_matrix/client/v3/login)
func (c *AdminClient) LoginWithPassword(ctx context.Context, rawUserID, password string) (*MatrixLoginResponse, error) {
	fullUserID := c.FormatUserID(rawUserID)

	loginPayload := map[string]interface{}{
		"type": "m.login.password",
		"identifier": map[string]string{
			"type": "m.id.user",
			"user": fullUserID,
		},
		"password": password,
	}

	jsonBytes, err := json.Marshal(loginPayload)
	if err != nil {
		return nil, err
	}

	loginURL := fmt.Sprintf("%s/_matrix/client/v3/login", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Synapse login: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr AdminAPIError
		apiErr.StatusCode = resp.StatusCode
		_ = json.Unmarshal(bodyBytes, &apiErr)
		if apiErr.ErrorMsg == "" {
			apiErr.ErrorMsg = string(bodyBytes)
		}
		return nil, &apiErr
	}

	var loginResp MatrixLoginResponse
	if err := json.Unmarshal(bodyBytes, &loginResp); err != nil {
		return nil, fmt.Errorf("failed to decode login response: %w", err)
	}

	return &loginResp, nil
}
