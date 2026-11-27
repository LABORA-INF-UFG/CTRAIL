package mcpserver

import (
	"strings"
	"testing"
	"time"
)

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"CTRAIL_BASE_URL", "CTRAIL_BEARER_TOKEN", "CTRAIL_USERNAME", "CTRAIL_PASSWORD", "CTRAIL_MCP_TIMEOUT", "CTRAIL_MCP_READ_ONLY", "CTRAIL_MCP_TRANSPORT", "CTRAIL_MCP_LISTEN_ADDR", "CTRAIL_MCP_HTTP_TOKEN"} {
		t.Setenv(name, "")
	}
}

func TestConfigDefaultsAndOverrides(t *testing.T) {
	clearConfigEnv(t)
	cfg, err := LoadConfig()
	if err != nil || cfg.BaseURL != "http://localhost:8080" || cfg.Timeout != 30*time.Second || cfg.Transport != "stdio" || cfg.ReadOnly {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	t.Setenv("CTRAIL_MCP_READ_ONLY", "true")
	t.Setenv("CTRAIL_MCP_TIMEOUT", "1m")
	t.Setenv("CTRAIL_MCP_TRANSPORT", "http")
	t.Setenv("CTRAIL_MCP_HTTP_TOKEN", "adapter-token")
	t.Setenv("CTRAIL_BASE_URL", "https://api.example.test/prefix")
	cfg, err = LoadConfig()
	if err != nil || !cfg.ReadOnly || cfg.Timeout != time.Minute || cfg.Transport != "http" {
		t.Fatalf("overrides: %+v %v", cfg, err)
	}
}

func TestInvalidConfigAndSecretRedaction(t *testing.T) {
	for _, tt := range []struct{ name, value string }{
		{"CTRAIL_BASE_URL", "http://operator:private-secret@example.test"},
		{"CTRAIL_BASE_URL", "http://[invalid-private-secret"},
		{"CTRAIL_BASE_URL", "file:///private-secret"},
		{"CTRAIL_BASE_URL", "https://example.test?token=private-secret"},
		{"CTRAIL_BASE_URL", "https://example.test#private-secret"},
		{"CTRAIL_MCP_TIMEOUT", "private-secret"},
		{"CTRAIL_MCP_TIMEOUT", "0s"},
		{"CTRAIL_MCP_TIMEOUT", "-1s"},
		{"CTRAIL_MCP_READ_ONLY", "private-secret"},
		{"CTRAIL_MCP_TRANSPORT", "private-secret"},
		{"CTRAIL_MCP_TRANSPORT", "http"},
		{"CTRAIL_USERNAME", "private-secret"},
		{"CTRAIL_PASSWORD", "private-secret"},
		{"CTRAIL_BEARER_TOKEN", "private-secret\r\n"},
	} {
		t.Run(tt.name+"/"+tt.value, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv(tt.name, tt.value)
			_, err := LoadConfig()
			if err == nil || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("expected redacted config error, got %v", err)
			}
		})
	}
	clearConfigEnv(t)
	t.Setenv("CTRAIL_BEARER_TOKEN", "test-token")
	t.Setenv("CTRAIL_USERNAME", "user")
	t.Setenv("CTRAIL_PASSWORD", "password")
	if _, err := LoadConfig(); err == nil {
		t.Error("conflicting authentication was accepted")
	}
}
