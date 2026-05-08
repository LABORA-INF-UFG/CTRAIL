package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
)

// ValidateRequestSecurity runs all request-level security checks.
func (dsm *DefaultSecurityManager) ValidateRequestSecurity(r *http.Request) error {
	if err := dsm.ValidateRequestSize(r); err != nil {
		return err
	}

	if origin := r.Header.Get("Origin"); origin != "" {
		if err := dsm.ValidateOrigin(origin); err != nil {
			return err
		}
	}

	if err := dsm.validateSecurityHeaders(r); err != nil {
		return err
	}

	return nil
}

// ValidateOrigin checks whether the request origin is in the allowed list.
func (dsm *DefaultSecurityManager) ValidateOrigin(origin string) error {
	for _, allowed := range dsm.config.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return nil
		}
	}

	return fmt.Errorf("origin not allowed: %s", origin)
}

// ValidateIPAddress checks whether the client IP is permitted by the blocklist and whitelist.
func (dsm *DefaultSecurityManager) ValidateIPAddress(ip string) error {
	for _, blocked := range dsm.config.BlockedIPs {
		if ip == blocked {
			return fmt.Errorf("IP address blocked: %s", ip)
		}
	}

	if dsm.config.EnableIPWhitelisting {
		for _, allowed := range dsm.config.WhitelistedIPs {
			if ip == allowed {
				return nil
			}
		}
		return fmt.Errorf("IP address not whitelisted: %s", ip)
	}

	return nil
}

// ValidateRequestSize returns an error if the Content-Length exceeds the configured maximum.
func (dsm *DefaultSecurityManager) ValidateRequestSize(r *http.Request) error {
	if r.ContentLength > dsm.config.MaxRequestSize {
		return fmt.Errorf("request too large: %d bytes (max: %d)",
			r.ContentLength, dsm.config.MaxRequestSize)
	}
	return nil
}

func (dsm *DefaultSecurityManager) validateSecurityHeaders(r *http.Request) error {
	if r.UserAgent() == "" {
		return fmt.Errorf("User-Agent header required")
	}

	if r.Method == "POST" || r.Method == "PUT" {
		contentType := r.Header.Get("Content-Type")
		if contentType == "" {
			return fmt.Errorf("Content-Type header required for %s requests", r.Method)
		}
	}

	return nil
}

func (dsm *DefaultSecurityManager) validateBasicAuth(username, password string) bool {
	return username != "" && password != "" && len(password) >= 8
}

// GenerateSecureHash returns the SHA-256 hex digest of the given string.
func (dsm *DefaultSecurityManager) GenerateSecureHash(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// VerifyHash checks whether data hashes to the given hash string.
func (dsm *DefaultSecurityManager) VerifyHash(data, hash string) bool {
	expectedHash := dsm.GenerateSecureHash(data)
	return expectedHash == hash
}

// GenerateNonce returns a 16-byte random hex string suitable for use as a request ID.
func (dsm *DefaultSecurityManager) GenerateNonce() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// UpdateConfig replaces the active security configuration.
func (dsm *DefaultSecurityManager) UpdateConfig(config *SecurityConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	dsm.config = config
	log.Printf("Security configuration updated")
	return nil
}

// GetSecurityLevel returns the current enforcement level.
func (dsm *DefaultSecurityManager) GetSecurityLevel() SecurityLevel {
	return dsm.level
}

// SetSecurityLevel sets the enforcement level.
func (dsm *DefaultSecurityManager) SetSecurityLevel(level SecurityLevel) {
	dsm.level = level
	log.Printf("Security level set to: %s", level)
}

// getClientIP extracts the real client IP, honoring standard proxy headers.
func getClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return strings.Split(ip, ",")[0]
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}

	parts := strings.Split(r.RemoteAddr, ":")
	if len(parts) > 0 {
		return parts[0]
	}

	return r.RemoteAddr
}

func getStringFromClaims(claims map[string]any, key string) string {
	if val, ok := claims[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getPermissionsFromClaims(claims map[string]any) []string {
	if val, ok := claims["permissions"]; ok {
		if perms, ok := val.([]any); ok {
			result := make([]string, len(perms))
			for i, perm := range perms {
				if str, ok := perm.(string); ok {
					result[i] = str
				}
			}
			return result
		}
	}
	return []string{}
}

func generateRandomSecret(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return base64.URLEncoding.EncodeToString(bytes)
}
