package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"chat-htp-api/internal/middleware"

	"github.com/gin-gonic/gin"
)

func TestMatrixGuard_PassThrough(t *testing.T) {
	router := gin.New()
	router.Use(middleware.MatrixGuard())

	passedThrough := false
	router.GET("/test-guard", func(c *gin.Context) {
		passedThrough = true
		c.String(http.StatusOK, "ok")
	})

	req, _ := http.NewRequest(http.MethodGet, "/test-guard", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !passedThrough {
		t.Fatalf("expected request to pass through guard middleware")
	}
}
