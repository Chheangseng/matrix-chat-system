package middleware

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// MatrixErrorResponse represents standard Matrix error JSON format
type MatrixErrorResponse struct {
	ErrCode string `json:"errcode"`
	Error   string `json:"error"`
}

// safeResponseWriter wraps the underlying ResponseWriter without exposing http.CloseNotifier.
// Gin's responseWriter has a known issue where it declares CloseNotify() but panics with a
// type assertion error if the underlying writer (such as in httptest or HTTP/2) doesn't implement it.
type safeResponseWriter struct {
	writer gin.ResponseWriter
}

func (s *safeResponseWriter) Header() http.Header {
	return s.writer.Header()
}

func (s *safeResponseWriter) Write(b []byte) (int, error) {
	return s.writer.Write(b)
}

func (s *safeResponseWriter) WriteHeader(statusCode int) {
	s.writer.WriteHeader(statusCode)
}

func (s *safeResponseWriter) Flush() {
	s.writer.Flush()
}

// MatrixProxy returns a Gin middleware that forwards requests to the Matrix Synapse server
// while concealing the internal backend address and software details from the client.
func MatrixProxy(target *url.URL) gin.HandlerFunc {
	proxy := httputil.NewSingleHostReverseProxy(target)

	// FlushInterval enables immediate streaming for Matrix /sync long-polling and SSE
	proxy.FlushInterval = 50 * time.Millisecond

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		// Capture original host requested by the client before reassigning req.Host
		clientHost := req.Host
		if clientHost == "" {
			clientHost = req.Header.Get("Host")
		}

		originalDirector(req)

		// Set target host so Synapse's HTTP server accepts the internal request
		req.Host = target.Host

		// Inform Synapse of the public client host so any generated links use the proxy address
		req.Header.Set("X-Forwarded-Host", clientHost)

		// Set forwarding headers for proper client identification in Synapse
		clientIP, _, err := net.SplitHostPort(req.RemoteAddr)
		if err == nil {
			if prior, ok := req.Header["X-Forwarded-For"]; ok {
				clientIP = stringsJoin(prior, ", ") + ", " + clientIP
			}
			req.Header.Set("X-Forwarded-For", clientIP)
		}

		if req.TLS != nil {
			req.Header.Set("X-Forwarded-Proto", "https")
		} else if proto := req.Header.Get("X-Forwarded-Proto"); proto == "" {
			req.Header.Set("X-Forwarded-Proto", "http")
		}
	}

	// ModifyResponse sanitizes backend response headers to prevent leaking Synapse info or addresses
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Strip or disguise headers that reveal Synapse backend presence or version
		resp.Header.Del("Server")
		resp.Header.Del("Synapse-Trace-Id")

		// Strip backend CORS headers to prevent duplicate or conflicting headers with gateway CORS
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Credentials")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")

		// Sanitize Access-Control-Expose-Headers
		if expose := resp.Header.Get("Access-Control-Expose-Headers"); expose != "" {
			cleaned := sanitizeExposeHeaders(expose)
			if cleaned != "" {
				resp.Header.Set("Access-Control-Expose-Headers", cleaned)
			} else {
				resp.Header.Del("Access-Control-Expose-Headers")
			}
		}

		// Rewrite Location header in redirects so internal backend address/port is never exposed
		if loc := resp.Header.Get("Location"); loc != "" {
			if locURL, err := url.Parse(loc); err == nil {
				// If redirect points to the backend Synapse address, rewrite to relative path
				if locURL.Host == target.Host {
					locURL.Scheme = ""
					locURL.Host = ""
					resp.Header.Set("Location", locURL.String())
				}
			}
		}

		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[PROXY ERROR] Failed to proxy %s %s: %v\n", r.Method, r.URL.Path, err)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)

		resp := MatrixErrorResponse{
			ErrCode: "M_BAD_GATEWAY",
			Error:   "Chat gateway service is currently unable to reach the homeserver",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}

	return func(c *gin.Context) {
		writer := &safeResponseWriter{writer: c.Writer}
		proxy.ServeHTTP(writer, c.Request)
		c.Abort()
	}
}

// sanitizeExposeHeaders removes sensitive headers (Server, Synapse-Trace-Id) from CORS exposure
func sanitizeExposeHeaders(headerVal string) string {
	parts := strings.Split(headerVal, ",")
	var kept []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		lower := strings.ToLower(trimmed)
		if lower == "server" || lower == "synapse-trace-id" {
			continue
		}
		if trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, ", ")
}

func stringsJoin(elems []string, sep string) string {
	switch len(elems) {
	case 0:
		return ""
	case 1:
		return elems[0]
	}
	n := len(sep) * (len(elems) - 1)
	for i := 0; i < len(elems); i++ {
		n += len(elems[i])
	}

	b := make([]byte, n)
	bp := copy(b, elems[0])
	for _, s := range elems[1:] {
		bp += copy(b[bp:], sep)
		bp += copy(b[bp:], s)
	}
	return string(b)
}
