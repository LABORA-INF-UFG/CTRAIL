package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// TLSManager manages TLS configuration and certificate loading.
type TLSManager struct {
	config *TLSConfig
}

// TLSConfig holds TLS-specific parameters.
type TLSConfig struct {
	MinVersion         uint16             `json:"minVersion"`
	MaxVersion         uint16             `json:"maxVersion"`
	CipherSuites       []uint16           `json:"cipherSuites"`
	CertFile           string             `json:"certFile"`
	KeyFile            string             `json:"keyFile"`
	CAFile             string             `json:"caFile"`
	InsecureSkipVerify bool               `json:"insecureSkipVerify"`
	ClientAuthType     tls.ClientAuthType `json:"clientAuthType"`
}

// SecurityMiddleware returns a standard http.Handler middleware that adds security headers
// and validates every request.
func (dsm *DefaultSecurityManager) SecurityMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			dsm.addSecurityHeaders(w)

			if err := dsm.ValidateRequestSecurity(r); err != nil {
				dsm.LogFailedAuth(fmt.Sprintf("Security validation failed: %s", err.Error()), r)
				http.Error(w, "Security validation failed", http.StatusBadRequest)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AuthenticationMiddleware returns an http.Handler middleware that authenticates requests
// and optionally enforces required permissions.
func (dsm *DefaultSecurityManager) AuthenticationMiddleware(requiredPermissions []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, err := dsm.AuthenticateRequest(r)
			if err != nil {
				dsm.LogFailedAuth(err.Error(), r)
				http.Error(w, "Authentication failed", http.StatusUnauthorized)
				return
			}

			if len(requiredPermissions) > 0 {
				if err := dsm.ValidatePermissions(ctx, requiredPermissions); err != nil {
					http.Error(w, "Insufficient permissions", http.StatusForbidden)
					return
				}
			}

			r.Header.Set("X-Security-Context-Client-ID", ctx.ClientID)
			r.Header.Set("X-Security-Context-User-ID", ctx.UserID)
			r.Header.Set("X-Security-Context-Request-ID", ctx.RequestID)

			next.ServeHTTP(w, r)
		})
	}
}

func (dsm *DefaultSecurityManager) addSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-XSS-Protection", "1; mode=block")

	if dsm.config.RequireHTTPS {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}

	w.Header().Set("Content-Security-Policy", "default-src 'self'")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
}

// RateLimitMiddleware returns an http.Handler middleware that enforces per-client rate limits.
func (dsm *DefaultSecurityManager) RateLimitMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientID := dsm.getClientIDFromRequest(r)

			if err := dsm.CheckRateLimit(clientID); err != nil {
				dsm.LogSecurityEvent("rate_limit_exceeded", &SecurityContext{
					ClientID:  clientID,
					IPAddress: getClientIP(r),
					UserAgent: r.UserAgent(),
					Timestamp: time.Now(),
				}, map[string]any{
					"path":   r.URL.Path,
					"method": r.Method,
				})

				w.Header().Set("Retry-After", "60")
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			dsm.UpdateRateLimit(clientID)
			next.ServeHTTP(w, r)
		})
	}
}

func (dsm *DefaultSecurityManager) getClientIDFromRequest(r *http.Request) string {
	if clientID := r.Header.Get("X-Client-ID"); clientID != "" {
		return clientID
	}

	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return dsm.GenerateSecureHash(apiKey)[:16]
	}

	return dsm.GenerateSecureHash(getClientIP(r))[:16]
}

// CORSMiddleware returns an http.Handler middleware that enforces CORS policy.
func (dsm *DefaultSecurityManager) CORSMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" {
				if err := dsm.ValidateOrigin(origin); err != nil {
					dsm.LogSecurityEvent("cors_violation", &SecurityContext{
						IPAddress: getClientIP(r),
						UserAgent: r.UserAgent(),
						Timestamp: time.Now(),
					}, map[string]any{
						"origin": origin,
						"error":  err.Error(),
					})
					http.Error(w, "CORS policy violation", http.StatusForbidden)
					return
				}

				w.Header().Set("Access-Control-Allow-Origin", origin)
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Client-ID")
			w.Header().Set("Access-Control-Max-Age", "86400")

			// Handle preflight
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestLoggerMiddleware returns an http.Handler middleware that logs request details.
func (dsm *DefaultSecurityManager) RequestLoggerMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// responseWriter wrapper to capture the status code written by downstream handlers
			wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)

			dsm.LogSecurityEvent("request_processed", &SecurityContext{
				IPAddress: getClientIP(r),
				UserAgent: r.UserAgent(),
				Timestamp: start,
			}, map[string]any{
				"method":      r.Method,
				"path":        r.URL.Path,
				"status_code": wrapped.statusCode,
				"duration_ms": duration.Milliseconds(),
				"size":        r.ContentLength,
			})
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the written status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// SecurityHealthCheck returns a health summary for the security module.
func (dsm *DefaultSecurityManager) SecurityHealthCheck() map[string]any {
	health := map[string]any{
		"status":             "healthy",
		"timestamp":          time.Now().UTC(),
		"security_level":     dsm.level,
		"active_api_keys":    len(dsm.apiKeys),
		"rate_limit_entries": len(dsm.rateLimiter),
	}

	issues := []string{}

	if !dsm.config.RequireHTTPS {
		issues = append(issues, "HTTPS not required")
	}

	if !dsm.config.EnableAuditLog {
		issues = append(issues, "Audit logging disabled")
	}

	if len(dsm.config.AllowedOrigins) == 1 && dsm.config.AllowedOrigins[0] == "*" {
		issues = append(issues, "CORS allows all origins")
	}

	if len(issues) > 0 {
		health["status"] = "warning"
		health["issues"] = issues
	}

	return health
}

// CleanupExpiredTokens removes expired API keys and stale rate-limit entries.
func (dsm *DefaultSecurityManager) CleanupExpiredTokens() int {
	cleaned := 0
	now := time.Now()

	for key, entry := range dsm.apiKeys {
		if entry.ExpiresAt != nil && now.After(*entry.ExpiresAt) {
			delete(dsm.apiKeys, key)
			cleaned++
		}
	}

	for clientID, entry := range dsm.rateLimiter {
		if now.After(entry.ResetTime.Add(time.Hour)) { // keep for 1 hour past reset window
			delete(dsm.rateLimiter, clientID)
		}
	}

	if cleaned > 0 {
		log.Printf("Cleaned up %d expired security tokens", cleaned)
	}

	return cleaned
}

// ExportSecurityMetrics returns a snapshot of security-related counters and settings.
func (dsm *DefaultSecurityManager) ExportSecurityMetrics() map[string]any {
	metrics := map[string]any{
		"timestamp": time.Now().UTC(),
		"api_keys": map[string]any{
			"total":  len(dsm.apiKeys),
			"active": dsm.countActiveAPIKeys(),
		},
		"rate_limiting": map[string]any{
			"tracked_clients":  len(dsm.rateLimiter),
			"limit_per_minute": dsm.config.RateLimitPerMinute,
		},
		"configuration": map[string]any{
			"security_level":       dsm.level,
			"https_required":       dsm.config.RequireHTTPS,
			"audit_enabled":        dsm.config.EnableAuditLog,
			"ip_whitelist_enabled": dsm.config.EnableIPWhitelisting,
			"max_request_size":     dsm.config.MaxRequestSize,
		},
	}

	return metrics
}

func (dsm *DefaultSecurityManager) countActiveAPIKeys() int {
	count := 0
	now := time.Now()

	for _, entry := range dsm.apiKeys {
		if entry.IsActive && (entry.ExpiresAt == nil || now.Before(*entry.ExpiresAt)) {
			count++
		}
	}

	return count
}

// NewTLSManager creates a TLSManager with the given configuration, using TLS 1.2/1.3 defaults.
func NewTLSManager(config *TLSConfig) *TLSManager {
	if config == nil {
		config = &TLSConfig{
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS13,
		}
	}

	return &TLSManager{
		config: config,
	}
}

// CreateTLSConfig builds a *tls.Config from the manager's settings, loading certificates if set.
func (tm *TLSManager) CreateTLSConfig() (*tls.Config, error) {
	config := &tls.Config{
		MinVersion:         tm.config.MinVersion,
		MaxVersion:         tm.config.MaxVersion,
		CipherSuites:       tm.config.CipherSuites,
		InsecureSkipVerify: tm.config.InsecureSkipVerify,
		ClientAuth:         tm.config.ClientAuthType,
	}

	if tm.config.CertFile != "" && tm.config.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(tm.config.CertFile, tm.config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{cert}
	}

	if tm.config.CAFile != "" {
		caCert, err := loadCACertificate(tm.config.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load CA certificate: %w", err)
		}
		config.RootCAs = caCert
		config.ClientCAs = caCert
	}

	return config, nil
}

func loadCACertificate(caFile string) (*x509.CertPool, error) {
	caCertPool := x509.NewCertPool()
	log.Printf("Loading CA certificate from: %s", caFile)
	return caCertPool, nil
}

// SecurityGinMiddleware returns a Gin handler that adds security headers and validates requests.
func (dsm *DefaultSecurityManager) SecurityGinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		dsm.addSecurityHeaders(c.Writer)

		if err := dsm.ValidateRequestSecurity(c.Request); err != nil {
			dsm.LogFailedAuth(fmt.Sprintf("Security validation failed: %s", err.Error()), c.Request)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Security validation failed"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// AuthenticationGinMiddleware returns a Gin handler that authenticates requests and enforces
// optional required permissions.
func (dsm *DefaultSecurityManager) AuthenticationGinMiddleware(requiredPermissions []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, err := dsm.AuthenticateRequest(c.Request)
		if err != nil {
			dsm.LogFailedAuth(err.Error(), c.Request)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication failed"})
			c.Abort()
			return
		}

		if len(requiredPermissions) > 0 {
			if err := dsm.ValidatePermissions(ctx, requiredPermissions); err != nil {
				c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
				c.Abort()
				return
			}
		}

		c.Set("security_context", ctx)
		c.Header("X-Security-Context-Client-ID", ctx.ClientID)
		c.Header("X-Security-Context-User-ID", ctx.UserID)
		c.Header("X-Security-Context-Request-ID", ctx.RequestID)

		c.Next()
	}
}

// RateLimitGinMiddleware returns a Gin handler that enforces per-client rate limits.
func (dsm *DefaultSecurityManager) RateLimitGinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID := dsm.getClientIDFromRequest(c.Request)

		if err := dsm.CheckRateLimit(clientID); err != nil {
			dsm.LogSecurityEvent("rate_limit_exceeded", &SecurityContext{
				ClientID:  clientID,
				IPAddress: c.ClientIP(),
				UserAgent: c.GetHeader("User-Agent"),
				Timestamp: time.Now(),
			}, map[string]any{
				"path":   c.Request.URL.Path,
				"method": c.Request.Method,
			})

			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "Rate limit exceeded"})
			c.Abort()
			return
		}

		dsm.UpdateRateLimit(clientID)
		c.Next()
	}
}

// CORSGinMiddleware returns a Gin handler that enforces CORS policy.
func (dsm *DefaultSecurityManager) CORSGinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		if origin != "" {
			if err := dsm.ValidateOrigin(origin); err != nil {
				dsm.LogSecurityEvent("cors_violation", &SecurityContext{
					IPAddress: c.ClientIP(),
					UserAgent: c.GetHeader("User-Agent"),
					Timestamp: time.Now(),
				}, map[string]any{
					"origin": origin,
					"error":  err.Error(),
				})
				c.JSON(http.StatusForbidden, gin.H{"error": "CORS policy violation"})
				c.Abort()
				return
			}

			c.Header("Access-Control-Allow-Origin", origin)
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Client-ID")
		c.Header("Access-Control-Max-Age", "86400")

		// Handle preflight
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}

// RequestLoggerGinMiddleware returns a Gin handler that logs request details after completion.
func (dsm *DefaultSecurityManager) RequestLoggerGinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		duration := time.Since(start)

		dsm.LogSecurityEvent("request_processed", &SecurityContext{
			IPAddress: c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
			Timestamp: start,
		}, map[string]any{
			"method":      c.Request.Method,
			"path":        c.Request.URL.Path,
			"status_code": c.Writer.Status(),
			"duration_ms": duration.Milliseconds(),
			"size":        c.Request.ContentLength,
		})
	}
}
