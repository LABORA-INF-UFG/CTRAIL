package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHTTPAuthenticationOriginAndProtocol(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-test-token" {
			t.Error("MCP token leaked upstream or gateway token missing")
		}
		_, _ = io.WriteString(w, `{"status":"healthy"}`)
	}))
	defer gateway.Close()
	server, err := NewServer(testConfig(gateway.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewHTTPHandler(server, ""); err == nil {
		t.Fatal("HTTP transport accepted empty token")
	}
	handler, _ := NewHTTPHandler(server, "adapter-test-token")
	host := httptest.NewServer(handler)
	defer host.Close()
	for _, tt := range []struct {
		token, origin, path string
		status              int
	}{
		{"", "", "/mcp", 401},
		{"wrong-token", "", "/mcp", 401},
		{"adapter-test-token", "https://untrusted.example", "/mcp", 403},
		{"adapter-test-token", "null", "/mcp", 403},
		{"adapter-test-token", "", "/other", 404},
	} {
		req, _ := http.NewRequest("POST", host.URL+tt.path, strings.NewReader(`{}`))
		if tt.token != "" {
			req.Header.Set("Authorization", "Bearer "+tt.token)
		}
		req.Header.Set("Origin", tt.origin)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tt.status {
			t.Errorf("token=%q origin=%q path=%s: status=%d want=%d", tt.token, tt.origin, tt.path, resp.StatusCode, tt.status)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "http-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: host.URL + "/mcp", HTTPClient: &http.Client{Transport: tokenTransport{token: "adapter-test-token"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result := callTool(t, ctx, session, "ctrail_get_health", `{}`)
	if result.IsError || decodeResponse(t, result).Status != 200 {
		t.Fatal("HTTP tool call failed")
	}
	// Older clients must still be able to initialize and invoke tools.
	for _, payload := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"legacy-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ctrail_get_health","arguments":{}}}`,
	} {
		req, _ := http.NewRequestWithContext(ctx, "POST", host.URL+"/mcp", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer adapter-test-token")
		req.Header.Set("Origin", host.URL)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2025-06-18")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		var rpc map[string]any
		if err := json.Unmarshal(data, &rpc); err != nil || resp.StatusCode != 200 || rpc["error"] != nil {
			t.Fatalf("legacy HTTP response: %d %s (%v)", resp.StatusCode, data, err)
		}
	}
}

type tokenTransport struct{ token string }

func (rt tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+rt.token)
	return http.DefaultTransport.RoundTrip(req)
}
