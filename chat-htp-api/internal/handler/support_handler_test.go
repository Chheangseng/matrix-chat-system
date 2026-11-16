package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/handler"

	"github.com/gin-gonic/gin"
)

func TestSupportHandler_GetSupportInfo_FallbackToConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		SynapseServerName:  "localhost",
		SupportUser:        "support",
		SupportDisplayName: "Customer Support Staff",
		SupportAvatarURL:   "mxc://localhost/avatar123",
		SupportAdmin:       false,
	}

	h := handler.NewSupportHandler(cfg, nil)

	r := gin.New()
	r.GET("/api-server/support/info", h.GetSupportInfo)

	req, _ := http.NewRequest(http.MethodGet, "/api-server/support/info", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if resp["support_user_id"] != cfg.GetSupportUserID() {
		t.Errorf("expected support_user_id '%s', got %v", cfg.GetSupportUserID(), resp["support_user_id"])
	}
	if resp["username"] != "support" {
		t.Errorf("expected username 'support', got %v", resp["username"])
	}
	if resp["display_name"] != "Customer Support Staff" {
		t.Errorf("expected display_name 'Customer Support Staff', got %v", resp["display_name"])
	}
	if resp["avatar_url"] != "mxc://localhost/avatar123" {
		t.Errorf("expected avatar_url 'mxc://localhost/avatar123', got %v", resp["avatar_url"])
	}
	if resp["status"] != "active" {
		t.Errorf("expected status 'active', got %v", resp["status"])
	}
}
