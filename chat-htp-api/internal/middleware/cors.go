package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware handles cross-origin resource sharing headers for web chat clients
func CORSMiddleware(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			if allowedOrigin == "*" {
				c.Header("Access-Control-Allow-Origin", origin)
			} else {
				c.Header("Access-Control-Allow-Origin", allowedOrigin)
			}
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, HEAD, PATCH")

			// Dynamically allow requested headers or provide comprehensive default
			reqHeaders := c.Request.Header.Get("Access-Control-Request-Headers")
			if reqHeaders != "" {
				c.Header("Access-Control-Allow-Headers", reqHeaders)
			} else {
				c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept, Accept-Language, Accept-Encoding, X-CSRF-Token, Authorization, X-Requested-With, X-Matrix-Device-Id")
			}
			c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Type, Authorization")
			c.Header("Access-Control-Max-Age", "86400")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
