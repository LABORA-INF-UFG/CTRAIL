package security

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// SecurityLevel represents the required security enforcement level.
type SecurityLevel string

const (
	SecurityLevelNone   SecurityLevel = "none"
	SecurityLevelBasic  SecurityLevel = "basic"
	SecurityLevelHigh   SecurityLevel = "high"
	SecurityLevelStrict SecurityLevel = "strict"
)

// AuthenticationType represents the supported authentication mechanisms.
type AuthenticationType string

const (
	AuthTypeNone      AuthenticationType = "none"
	AuthTypeAPIKey    AuthenticationType = "api_key"
	AuthTypeBearer    AuthenticationType = "bearer"
	AuthTypeOAuth2    AuthenticationType = "oauth2"
	AuthTypeMutualTLS AuthenticationType = "mutual_tls"
	AuthTypeBasic     AuthenticationType = "basic"
)

// SecurityContext carries authentication and authorisation metadata for a single request.
type SecurityContext struct {
	RequestID     string             `json:"requestId"`
	ClientID      string             `json:"clientId"`
	UserID        string             `json:"userId,omitempty"`
	AuthType      AuthenticationType `json:"authType"`
	SecurityLevel SecurityLevel      `json:"securityLevel"`
	Permissions   []string           `json:"permissions"`
	IPAddress     string             `json:"ipAddress"`
	UserAgent     string             `json:"userAgent"`
	Timestamp     time.Time          `json:"timestamp"`
	TokenHash     string             `json:"tokenHash,omitempty"`
	Metadata      map[string]any     `json:"metadata,omitempty"`
}

// SecurityConfig holds all tuneable security parameters.
type SecurityConfig struct {
	JWTSecret             string        `json:"jwtSecret"`
	TokenExpiration       time.Duration `json:"tokenExpiration"`
	AllowedOrigins        []string      `json:"allowedOrigins"`
	RateLimitPerMinute    int           `json:"rateLimitPerMinute"`
	RequireHTTPS          bool          `json:"requireHttps"`
	EnableAuditLog        bool          `json:"enableAuditLog"`
	MaxRequestSize        int64         `json:"maxRequestSize"`
	EnableIPWhitelisting  bool          `json:"enableIpWhitelisting"`
	WhitelistedIPs        []string      `json:"whitelistedIps"`
	BlockedIPs            []string      `json:"blockedIps"`
	SessionTimeout        time.Duration `json:"sessionTimeout"`
	EnableTLSVerification bool          `json:"enableTlsVerification"`
	RequiredTLSVersion    string        `json:"requiredTlsVersion"`
}

// SecurityManager defines the full security contract for the gateway.
type SecurityManager interface {
	// Authentication and authorisation
	AuthenticateRequest(r *http.Request) (*SecurityContext, error)
	ValidatePermissions(ctx *SecurityContext, requiredPermissions []string) error
	GenerateAPIKey(clientID string) (string, error)
	ValidateAPIKey(apiKey string) (*SecurityContext, error)

	// JWT
	GenerateJWT(claims map[string]any) (string, error)
	ValidateJWT(token string) (map[string]any, error)

	// Rate limiting
	CheckRateLimit(clientID string) error
	UpdateRateLimit(clientID string) error

	// Audit and logging
	LogSecurityEvent(event string, ctx *SecurityContext, details map[string]any)
	LogFailedAuth(reason string, request *http.Request)

	// Security validations
	ValidateRequestSecurity(r *http.Request) error
	ValidateOrigin(origin string) error
	ValidateIPAddress(ip string) error
	ValidateRequestSize(r *http.Request) error

	// Hashing and cryptography
	GenerateSecureHash(data string) string
	VerifyHash(data, hash string) bool
	GenerateNonce() string

	// Configuration
	UpdateConfig(config *SecurityConfig) error
	GetSecurityLevel() SecurityLevel
	SetSecurityLevel(level SecurityLevel)

	// Gin middleware
	SecurityGinMiddleware() gin.HandlerFunc
	AuthenticationGinMiddleware(requiredPermissions []string) gin.HandlerFunc
	RateLimitGinMiddleware() gin.HandlerFunc
	CORSGinMiddleware() gin.HandlerFunc
	RequestLoggerGinMiddleware() gin.HandlerFunc

	// Health and metrics
	SecurityHealthCheck() map[string]any
	ExportSecurityMetrics() map[string]any
	CleanupExpiredTokens() int
}

// DefaultSecurityManager is the production implementation of SecurityManager.
type DefaultSecurityManager struct {
	config      *SecurityConfig
	rateLimiter map[string]*RateLimitEntry
	apiKeys     map[string]*APIKeyEntry
	level       SecurityLevel
}

// RateLimitEntry tracks the request count and window reset time for one client.
type RateLimitEntry struct {
	Count     int
	ResetTime time.Time
}

// APIKeyEntry stores metadata for a registered API key.
type APIKeyEntry struct {
	ClientID    string
	Permissions []string
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	IsActive    bool
	LastUsed    *time.Time
}

// NewDefaultSecurityManager creates a security manager with the given configuration.
func NewDefaultSecurityManager(config *SecurityConfig) *DefaultSecurityManager {
	if config == nil {
		config = &SecurityConfig{
			JWTSecret:             generateRandomSecret(32),
			TokenExpiration:       time.Hour * 24,
			AllowedOrigins:        []string{"*"},
			RateLimitPerMinute:    1000,
			RequireHTTPS:          true,
			EnableAuditLog:        true,
			MaxRequestSize:        10 * 1024 * 1024, // 10MB
			EnableIPWhitelisting:  false,
			SessionTimeout:        time.Hour * 8,
			EnableTLSVerification: true,
			RequiredTLSVersion:    "1.2",
		}
	}

	return &DefaultSecurityManager{
		config:      config,
		rateLimiter: make(map[string]*RateLimitEntry),
		apiKeys:     make(map[string]*APIKeyEntry),
		level:       SecurityLevelHigh,
	}
}

// AuthenticateRequest authenticates an HTTP request and returns the security context.
func (dsm *DefaultSecurityManager) AuthenticateRequest(r *http.Request) (*SecurityContext, error) {
	ctx := &SecurityContext{
		RequestID:     dsm.GenerateNonce(),
		IPAddress:     getClientIP(r),
		UserAgent:     r.UserAgent(),
		Timestamp:     time.Now(),
		SecurityLevel: dsm.level,
		Metadata:      make(map[string]any),
	}

	if dsm.config.RequireHTTPS && r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		return nil, fmt.Errorf("HTTPS required")
	}

	if err := dsm.ValidateIPAddress(ctx.IPAddress); err != nil {
		return nil, fmt.Errorf("IP validation failed: %w", err)
	}

	if err := dsm.tryAuthenticationMethods(r, ctx); err != nil {
		dsm.LogFailedAuth(err.Error(), r)
		return nil, err
	}

	if err := dsm.CheckRateLimit(ctx.ClientID); err != nil {
		return nil, fmt.Errorf("rate limit exceeded: %w", err)
	}

	dsm.UpdateRateLimit(ctx.ClientID)

	dsm.LogSecurityEvent("authentication_success", ctx, map[string]any{
		"auth_type": ctx.AuthType,
		"client_id": ctx.ClientID,
	})

	return ctx, nil
}

// tryAuthenticationMethods attempts Bearer JWT, API key, and Basic Auth in order.
func (dsm *DefaultSecurityManager) tryAuthenticationMethods(r *http.Request, ctx *SecurityContext) error {
	if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := dsm.ValidateJWT(token)
		if err == nil {
			ctx.AuthType = AuthTypeBearer
			ctx.ClientID = getStringFromClaims(claims, "client_id")
			ctx.UserID = getStringFromClaims(claims, "user_id")
			ctx.Permissions = getPermissionsFromClaims(claims)
			ctx.TokenHash = dsm.GenerateSecureHash(token)
			return nil
		}
	}

	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		apiCtx, err := dsm.ValidateAPIKey(apiKey)
		if err == nil {
			ctx.AuthType = AuthTypeAPIKey
			ctx.ClientID = apiCtx.ClientID
			ctx.Permissions = apiCtx.Permissions
			return nil
		}
	}

	if username, password, ok := r.BasicAuth(); ok {
		if dsm.validateBasicAuth(username, password) {
			ctx.AuthType = AuthTypeBasic
			ctx.ClientID = username

			switch username {
			case "admin":
				ctx.Permissions = []string{"admin", "api_access", "traffic_influence:create", "traffic_influence:read", "traffic_influence:update", "traffic_influence:delete"}
			case "apiuser":
				ctx.Permissions = []string{"api_access", "traffic_influence:create", "traffic_influence:read"}
			case "testuser":
				ctx.Permissions = []string{"api_access", "traffic_influence:create", "traffic_influence:read"}
			default:
				ctx.Permissions = []string{"basic_access"}
			}
			return nil
		}
	}

	return fmt.Errorf("authentication failed: no valid credentials provided")
}

// ValidatePermissions checks that the security context holds all required permissions.
func (dsm *DefaultSecurityManager) ValidatePermissions(ctx *SecurityContext, requiredPermissions []string) error {
	if ctx == nil {
		return fmt.Errorf("security context is nil")
	}

	// admin / super_admin bypass all permission checks
	for _, perm := range ctx.Permissions {
		if perm == "admin" || perm == "super_admin" {
			return nil
		}
	}

	for _, required := range requiredPermissions {
		found := false
		for _, userPerm := range ctx.Permissions {
			if userPerm == required || strings.HasPrefix(required, userPerm+":") {
				found = true
				break
			}
		}
		if !found {
			dsm.LogSecurityEvent("permission_denied", ctx, map[string]any{
				"required_permission": required,
				"user_permissions":    ctx.Permissions,
			})
			return fmt.Errorf("permission denied: missing %s", required)
		}
	}

	return nil
}

// GenerateAPIKey creates and stores a new API key for the given client ID.
func (dsm *DefaultSecurityManager) GenerateAPIKey(clientID string) (string, error) {
	if clientID == "" {
		return "", fmt.Errorf("clientID is required")
	}

	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}

	apiKey := base64.URLEncoding.EncodeToString(bytes)

	dsm.apiKeys[apiKey] = &APIKeyEntry{
		ClientID:    clientID,
		Permissions: []string{"api_access"},
		CreatedAt:   time.Now(),
		IsActive:    true,
	}

	log.Printf("Generated API key for client: %s", clientID)
	return apiKey, nil
}

// ValidateAPIKey validates an API key and returns the associated security context.
func (dsm *DefaultSecurityManager) ValidateAPIKey(apiKey string) (*SecurityContext, error) {
	entry, exists := dsm.apiKeys[apiKey]
	if !exists || !entry.IsActive {
		return nil, fmt.Errorf("invalid API key")
	}

	if entry.ExpiresAt != nil && time.Now().After(*entry.ExpiresAt) {
		return nil, fmt.Errorf("API key expired")
	}

	now := time.Now()
	entry.LastUsed = &now

	return &SecurityContext{
		ClientID:    entry.ClientID,
		AuthType:    AuthTypeAPIKey,
		Permissions: entry.Permissions,
		Timestamp:   time.Now(),
	}, nil
}

// GenerateJWT creates a signed JWT with the given claims, adding standard iat/exp/iss.
func (dsm *DefaultSecurityManager) GenerateJWT(claims map[string]any) (string, error) {
	now := time.Now()
	claims["iat"] = now.Unix()
	claims["exp"] = now.Add(dsm.config.TokenExpiration).Unix()
	claims["iss"] = "ctrail"

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(claims))
	return token.SignedString([]byte(dsm.config.JWTSecret))
}

// ValidateJWT validates a JWT string and returns its claims.
func (dsm *DefaultSecurityManager) ValidateJWT(tokenString string) (map[string]any, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(dsm.config.JWTSecret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return map[string]any(claims), nil
	}

	return nil, fmt.Errorf("invalid token")
}

// CheckRateLimit returns an error if the client has exceeded its per-minute request quota.
func (dsm *DefaultSecurityManager) CheckRateLimit(clientID string) error {
	now := time.Now()
	entry, exists := dsm.rateLimiter[clientID]

	if !exists {
		return nil // first request from this client
	}

	if now.After(entry.ResetTime) {
		return nil
	}

	if entry.Count >= dsm.config.RateLimitPerMinute {
		return fmt.Errorf("rate limit exceeded")
	}

	return nil
}

// UpdateRateLimit increments the request counter for the given client.
func (dsm *DefaultSecurityManager) UpdateRateLimit(clientID string) error {
	now := time.Now()
	entry, exists := dsm.rateLimiter[clientID]

	if !exists || now.After(entry.ResetTime) {
		dsm.rateLimiter[clientID] = &RateLimitEntry{
			Count:     1,
			ResetTime: now.Add(time.Minute),
		}
	} else {
		entry.Count++
	}

	return nil
}

// LogSecurityEvent writes a structured audit log entry.
func (dsm *DefaultSecurityManager) LogSecurityEvent(event string, ctx *SecurityContext, details map[string]any) {
	if !dsm.config.EnableAuditLog {
		return
	}

	logEntry := map[string]any{
		"timestamp":      time.Now().UTC(),
		"event":          event,
		"request_id":     ctx.RequestID,
		"client_id":      ctx.ClientID,
		"user_id":        ctx.UserID,
		"ip_address":     ctx.IPAddress,
		"user_agent":     ctx.UserAgent,
		"auth_type":      ctx.AuthType,
		"security_level": ctx.SecurityLevel,
		"details":        details,
	}

	log.Printf("SECURITY_EVENT: %+v", logEntry)
}

// LogFailedAuth logs a failed authentication attempt.
func (dsm *DefaultSecurityManager) LogFailedAuth(reason string, request *http.Request) {
	logEntry := map[string]any{
		"timestamp":  time.Now().UTC(),
		"event":      "authentication_failed",
		"reason":     reason,
		"ip_address": getClientIP(request),
		"user_agent": request.UserAgent(),
		"path":       request.URL.Path,
		"method":     request.Method,
	}

	log.Printf("FAILED_AUTH: %+v", logEntry)
}
