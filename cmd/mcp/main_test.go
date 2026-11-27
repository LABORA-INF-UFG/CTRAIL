package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/LABORA-INF-UFG/ctrail/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The helper runs the real command entry point with pipes for stdin/stdout.
func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv("CTRAIL_MCP_TEST_HELPER") != "1" {
		return
	}
	if err := run(context.Background()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestStdioCommand(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" || r.Header.Get("Authorization") != "Bearer command-test-token" {
			t.Error("command did not use configured gateway and credentials")
		}
		_, _ = io.WriteString(w, `{"status":"healthy"}`)
	}))
	defer gateway.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestStdioHelperProcess$")
	command.Env = append(os.Environ(),
		"CTRAIL_MCP_TEST_HELPER=1", "CTRAIL_BASE_URL="+gateway.URL,
		"CTRAIL_BEARER_TOKEN=command-test-token", "CTRAIL_USERNAME=", "CTRAIL_PASSWORD=",
		"CTRAIL_MCP_TRANSPORT=stdio", "CTRAIL_MCP_READ_ONLY=true", "CTRAIL_MCP_TIMEOUT=5s",
	)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-command-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatalf("command handshake failed: %v", err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 9 {
		t.Fatalf("command ignored read-only config: %v %v", tools, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ctrail_get_health", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result.StructuredContent)
	var response mcpserver.APIResponse
	if err := json.Unmarshal(data, &response); err != nil || result.IsError || response.Status != 200 {
		t.Fatalf("stdio tool response: %s (%v)", data, err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("stdio command did not shut down cleanly: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected command diagnostics: %s", stderr.String())
	}
}
