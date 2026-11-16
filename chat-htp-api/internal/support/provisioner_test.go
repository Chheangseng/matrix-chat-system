package support_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/support"
	"chat-htp-api/internal/synapse"
)

func TestProvisioner_EnsureSupportAccount_Success(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Admin login
		if r.Method == http.MethodPost && r.URL.Path == "/_matrix/client/v3/login" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"mock_admin_token"}`))
			return
		}

		// Create/Update user
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v2/users/") {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)

			if body["displayname"] != "Customer Support" {
				t.Errorf("expected displayname 'Customer Support', got %v", body["displayname"])
			}
			if body["password"] != "support_password_123" {
				t.Errorf("expected password 'support_password_123', got %v", body["password"])
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":        "@support:localhost",
				"displayname": "Customer Support",
				"admin":       false,
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	parsedURL, _ := url.Parse(mockServer.URL)
	cfg := &config.Config{
		SynapseURL:         parsedURL,
		SynapseServerName:  "localhost",
		AdminUser:          "admin",
		AdminPassword:      "admin_password",
		AdminToken:         "mock_admin_token",
		SupportUser:        "support",
		SupportPassword:    "support_password_123",
		SupportDisplayName: "Customer Support",
		SupportAdmin:       false,
	}

	adminClient := synapse.NewAdminClient(cfg)
	provisioner := support.NewProvisioner(adminClient, cfg, nil)
	provisioner.SetRetryPolicy(3, 10*time.Millisecond)

	user, err := provisioner.EnsureSupportAccount(context.Background())
	if err != nil {
		t.Fatalf("unexpected error provisioning support account: %v", err)
	}

	if user.Name != "@support:localhost" {
		t.Errorf("expected user name @support:localhost, got %s", user.Name)
	}
	if user.DisplayName != "Customer Support" {
		t.Errorf("expected display name 'Customer Support', got %s", user.DisplayName)
	}
}

func TestProvisioner_EnsureSupportAccount_RetryRecovery(t *testing.T) {
	var attempts int32

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost && r.URL.Path == "/_matrix/client/v3/login" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"mock_admin_token"}`))
			return
		}

		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/_synapse/admin/v2/users/") {
			count := atomic.AddInt32(&attempts, 1)
			if count < 3 {
				// Simulate Synapse temporary 503 during cold boot
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"errcode":"M_UNKNOWN","error":"Starting up"}`))
				return
			}

			// Succeed on 3rd attempt
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":        "@support:localhost",
				"displayname": "Customer Support",
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	parsedURL, _ := url.Parse(mockServer.URL)
	cfg := &config.Config{
		SynapseURL:         parsedURL,
		SynapseServerName:  "localhost",
		AdminUser:          "admin",
		AdminPassword:      "admin_password",
		AdminToken:         "mock_admin_token",
		SupportUser:        "support",
		SupportPassword:    "support_password_123",
		SupportDisplayName: "Customer Support",
	}

	adminClient := synapse.NewAdminClient(cfg)
	provisioner := support.NewProvisioner(adminClient, cfg, nil)
	provisioner.SetRetryPolicy(5, 10*time.Millisecond)

	user, err := provisioner.EnsureSupportAccount(context.Background())
	if err != nil {
		t.Fatalf("expected recovery after retries, got error: %v", err)
	}

	if user == nil || user.Name != "@support:localhost" {
		t.Errorf("expected recovered support user, got %+v", user)
	}

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts before success, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestProvisioner_EnsureSupportAccount_ExhaustedRetries(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errcode":"M_UNKNOWN","error":"Fatal Synapse Error"}`))
	}))
	defer mockServer.Close()

	parsedURL, _ := url.Parse(mockServer.URL)
	cfg := &config.Config{
		SynapseURL:        parsedURL,
		SynapseServerName: "localhost",
		AdminToken:        "mock_admin_token",
		SupportUser:       "support",
	}

	adminClient := synapse.NewAdminClient(cfg)
	provisioner := support.NewProvisioner(adminClient, cfg, nil)
	provisioner.SetRetryPolicy(2, 5*time.Millisecond)

	_, err := provisioner.EnsureSupportAccount(context.Background())
	if err == nil {
		t.Fatalf("expected error when retries exhausted, got nil")
	}
}
