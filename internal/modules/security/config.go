package security

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// LoadSecurityConfig loads security configuration from a file and environment variables.
// Environment variables take precedence over file values.
func LoadSecurityConfig(configPath string) (*SecurityConfig, error) {
	config := &SecurityConfig{
		TokenExpiration:       24 * time.Hour,
		AllowedOrigins:        []string{"https://localhost:3000"},
		RateLimitPerMinute:    1000,
		RequireHTTPS:          true,
		EnableAuditLog:        true,
		MaxRequestSize:        10 * 1024 * 1024, // 10MB
		EnableIPWhitelisting:  false,
		SessionTimeout:        8 * time.Hour,
		EnableTLSVerification: true,
		RequiredTLSVersion:    "1.2",
	}

	if configPath != "" {
		if err := loadFromFile(config, configPath); err != nil {
			return nil, fmt.Errorf("failed to load security config from file: %w", err)
		}
	}

	loadFromEnv(config)

	if err := validateSecurityConfig(config); err != nil {
		return nil, fmt.Errorf("invalid security configuration: %w", err)
	}

	return config, nil
}

func loadFromFile(config *SecurityConfig, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	return decoder.Decode(config)
}

func loadFromEnv(config *SecurityConfig) {
	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		config.JWTSecret = jwtSecret
	}

	if tokenExp := os.Getenv("TOKEN_EXPIRATION_HOURS"); tokenExp != "" {
		if hours, err := time.ParseDuration(tokenExp + "h"); err == nil {
			config.TokenExpiration = hours
		}
	}

	if requireHTTPS := os.Getenv("REQUIRE_HTTPS"); requireHTTPS == "false" {
		config.RequireHTTPS = false
	}

	if enableAudit := os.Getenv("ENABLE_AUDIT_LOG"); enableAudit == "false" {
		config.EnableAuditLog = false
	}

	if rateLimitStr := os.Getenv("RATE_LIMIT_PER_MINUTE"); rateLimitStr != "" {
		if rateLimit, err := parseIntEnv(rateLimitStr); err == nil && rateLimit > 0 {
			config.RateLimitPerMinute = rateLimit
		}
	}

	if maxSizeStr := os.Getenv("MAX_REQUEST_SIZE_MB"); maxSizeStr != "" {
		if maxSize, err := parseIntEnv(maxSizeStr); err == nil && maxSize > 0 {
			config.MaxRequestSize = int64(maxSize) * 1024 * 1024
		}
	}

	if allowedOrigins := os.Getenv("ALLOWED_ORIGINS"); allowedOrigins != "" {
		config.AllowedOrigins = parseStringSlice(allowedOrigins)
	}

	if whitelistEnabled := os.Getenv("ENABLE_IP_WHITELIST"); whitelistEnabled == "true" {
		config.EnableIPWhitelisting = true
		if whitelistIPs := os.Getenv("WHITELISTED_IPS"); whitelistIPs != "" {
			config.WhitelistedIPs = parseStringSlice(whitelistIPs)
		}
	}

	if blockedIPs := os.Getenv("BLOCKED_IPS"); blockedIPs != "" {
		config.BlockedIPs = parseStringSlice(blockedIPs)
	}
}

func validateSecurityConfig(config *SecurityConfig) error {
	if config.JWTSecret == "" {
		config.JWTSecret = generateRandomSecret(32)
	}

	if len(config.JWTSecret) < 16 {
		return fmt.Errorf("JWT secret must be at least 16 characters long")
	}

	if config.TokenExpiration <= 0 {
		return fmt.Errorf("token expiration must be positive")
	}

	if config.RateLimitPerMinute <= 0 {
		return fmt.Errorf("rate limit must be positive")
	}

	if config.MaxRequestSize <= 0 {
		return fmt.Errorf("max request size must be positive")
	}

	if config.SessionTimeout <= 0 {
		config.SessionTimeout = 8 * time.Hour
	}

	if len(config.AllowedOrigins) == 0 {
		config.AllowedOrigins = []string{"*"}
	}

	return nil
}

func parseIntEnv(value string) (int, error) {
	var result int
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		return 0, err
	}
	return result, nil
}

func parseStringSlice(value string) []string {
	var result []string
	if value != "" {
		return []string{value} // TODO: implement proper comma-separated parsing
	}
	return result
}

// CreateDefaultSecurityConfig returns a permissive configuration suitable for development.
func CreateDefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		JWTSecret:             generateRandomSecret(32),
		TokenExpiration:       24 * time.Hour,
		AllowedOrigins:        []string{"http://localhost:3000", "https://localhost:3000"},
		RateLimitPerMinute:    1000,
		RequireHTTPS:          false,
		EnableAuditLog:        true,
		MaxRequestSize:        10 * 1024 * 1024,
		EnableIPWhitelisting:  false,
		SessionTimeout:        8 * time.Hour,
		EnableTLSVerification: false,
		RequiredTLSVersion:    "1.2",
	}
}

// CreateProductionSecurityConfig returns a strict configuration for production use.
// AllowedOrigins must be populated explicitly before deploying.
func CreateProductionSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		JWTSecret:             generateRandomSecret(32),
		TokenExpiration:       8 * time.Hour,
		AllowedOrigins:        []string{},
		RateLimitPerMinute:    500,
		RequireHTTPS:          true,
		EnableAuditLog:        true,
		MaxRequestSize:        5 * 1024 * 1024,
		EnableIPWhitelisting:  true,
		SessionTimeout:        4 * time.Hour,
		EnableTLSVerification: true,
		RequiredTLSVersion:    "1.3",
	}
}

// ExportSecurityConfig writes the config to a JSON file, redacting the JWT secret.
func ExportSecurityConfig(config *SecurityConfig, path string) error {
	exportConfig := *config
	exportConfig.JWTSecret = "[REDACTED]"

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(&exportConfig)
}
