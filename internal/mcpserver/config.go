package mcpserver

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config configures the MCP adapter independently of the gateway and its NEF credentials.
type Config struct {
	BaseURL     string
	BearerToken string
	Username    string
	Password    string
	Timeout     time.Duration
	ReadOnly    bool
	Transport   string
	ListenAddr  string
	HTTPToken   string
}

// LoadConfig reads only the adapter's environment variables. It does not read .env files.
func LoadConfig() (Config, error) {
	cfg := Config{
		BaseURL:     envOr("CTRAIL_BASE_URL", "http://localhost:8080"),
		BearerToken: os.Getenv("CTRAIL_BEARER_TOKEN"),
		Username:    os.Getenv("CTRAIL_USERNAME"),
		Password:    os.Getenv("CTRAIL_PASSWORD"),
		Timeout:     30 * time.Second,
		Transport:   envOr("CTRAIL_MCP_TRANSPORT", "stdio"),
		ListenAddr:  envOr("CTRAIL_MCP_LISTEN_ADDR", "127.0.0.1:8081"),
		HTTPToken:   os.Getenv("CTRAIL_MCP_HTTP_TOKEN"),
	}
	var err error
	if value := os.Getenv("CTRAIL_MCP_TIMEOUT"); value != "" {
		cfg.Timeout, err = time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("CTRAIL_MCP_TIMEOUT must be a positive Go duration, for example 30s")
		}
	}
	if value := os.Getenv("CTRAIL_MCP_READ_ONLY"); value != "" {
		cfg.ReadOnly, err = strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("CTRAIL_MCP_READ_ONLY must be true or false")
		}
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	if cfg.Transport != "stdio" && cfg.Transport != "http" {
		return Config{}, fmt.Errorf("CTRAIL_MCP_TRANSPORT must be stdio or http")
	}
	if cfg.Transport == "http" && strings.TrimSpace(cfg.HTTPToken) == "" {
		return Config{}, fmt.Errorf("CTRAIL_MCP_HTTP_TOKEN is required for the HTTP transport")
	}
	return cfg, nil
}

func (cfg Config) validate() error {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("CTRAIL_BASE_URL must be an absolute http or https URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("CTRAIL_BASE_URL must not contain credentials, a query, or a fragment")
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("CTRAIL_MCP_TIMEOUT must be positive")
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		return fmt.Errorf("CTRAIL_USERNAME and CTRAIL_PASSWORD must be set together")
	}
	if cfg.BearerToken != "" && cfg.Username != "" {
		return fmt.Errorf("configure either CTRAIL_BEARER_TOKEN or Basic authentication, not both")
	}
	if strings.ContainsAny(cfg.BearerToken, "\r\n") {
		return fmt.Errorf("CTRAIL_BEARER_TOKEN must not contain newlines")
	}
	return nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
