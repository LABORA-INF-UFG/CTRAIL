package functional

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/LABORA-INF-UFG/ctrail/internal/mcpserver"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules"
	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This test needs only committed gateway interfaces, not optional cluster or
// endpoint registry helpers from a local checkout.
func TestMCPGatewayTrafficInfluence(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate topology fixture")
	}
	t.Setenv("TOPOLOGY_CONFIG_FILE", filepath.Join(filepath.Dir(source), "..", "..", "config", "topology.json"))
	gin.SetMode(gin.TestMode)
	nef := NewMockNEFServer()
	defer nef.Close()
	gateway := modules.NewGatewayOrchestrator(nef.URL, "test-token")
	router := gin.New()
	gateway.SetupSecureRoutes(router)
	host := httptest.NewServer(router)
	defer host.Close()
	adapter, err := mcpserver.NewServer(mcpserver.Config{
		BaseURL: host.URL, Username: "admin", Password: "adminpassword123", Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := adapter.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "gateway-integration", Version: "1"}, nil)
	session, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(name string, arguments map[string]any, status int) mcpserver.APIResponse {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		data, _ := json.Marshal(result.StructuredContent)
		var response mcpserver.APIResponse
		if err := json.Unmarshal(data, &response); err != nil || result.IsError || response.Status != status {
			t.Fatalf("%s: HTTP %d, error=%v, response=%s", name, response.Status, err, data)
		}
		return response
	}
	call("ctrail_get_health", map[string]any{}, http.StatusOK)
	call("ctrail_list_edge_cloud_zones", map[string]any{}, http.StatusOK)
	request := map[string]any{
		"appId": "agent-app", "edgeCloudZoneId": "zone-north",
		"device": map[string]any{"ipv4Address": map[string]any{"publicAddress": "10.0.0.1"}},
	}
	ti := call("ctrail_create_traffic_influence", map[string]any{"request": request}, http.StatusCreated)
	tiID := ti.Body.(map[string]any)["trafficInfluenceID"].(string)
	call("ctrail_get_traffic_influence", map[string]any{"trafficInfluenceId": tiID}, http.StatusOK)
	call("ctrail_list_traffic_influences", map[string]any{"appId": "agent-app"}, http.StatusOK)
	call("ctrail_update_traffic_influence", map[string]any{"trafficInfluenceId": tiID, "request": request}, http.StatusOK)
	call("ctrail_delete_traffic_influence", map[string]any{"trafficInfluenceId": tiID}, http.StatusNoContent)
}
