package middleware

import (
	"log"

	"github.com/gin-gonic/gin"
)

// MatrixGuard inspects and authorizes requests before they reach the Matrix homeserver proxy.
// Currently logs the user's IP address and permits all traffic.
func MatrixGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		log.Printf("[PROXY GUARD] Incoming request from IP: %s | %s %s\n", clientIP, c.Request.Method, c.Request.URL.Path)

		// Placeholder for future guard logic (e.g. IP whitelist/blacklist, rate-limiting, custom token verification).
		// Example blocking:
		// if isBlocked(clientIP) {
		//     c.AbortWithStatusJSON(http.StatusForbidden, MatrixErrorResponse{
		//         ErrCode: "M_FORBIDDEN",
		//         Error:   "Access denied by proxy guard",
		//     })
		//     return
		// }

		c.Next()
	}
}
