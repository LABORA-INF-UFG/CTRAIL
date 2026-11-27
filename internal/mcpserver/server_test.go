package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testConfig(baseURL string) Config {
	return Config{BaseURL: baseURL, BearerToken: "upstream-test-token", Timeout: 2 * time.Second}
}

func connectTestClient(t *testing.T, cfg Config) (context.Context, *mcp.ClientSession) {
	t.Helper()
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "ctrail-test", Version: "1.0.0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		_ = ss.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		_ = ss.Close()
		cancel()
	})
	return ctx, cs
}

func callTool(t *testing.T, ctx context.Context, session *mcp.ClientSession, name, arguments string) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: json.RawMessage(arguments)})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return result
}

func decodeResponse(t *testing.T, result *mcp.CallToolResult) APIResponse {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("missing text response")
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var response APIResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("decode structured response: %v; content=%v", err, result.Content)
	}
	if response.Status == 0 {
		t.Fatalf("missing HTTP status: %s; text=%v", data, result.Content)
	}
	return response
}

// Exercise every public tool over a real MCP connection, independently specifying
// the HTTP contract instead of invoking the adapter's request builders directly.
func TestToolAPIContracts(t *testing.T) {
	tests := []struct {
		name, args, method, path, query, body string
		status                                int
	}{
		{"ctrail_get_health", `{}`, "GET", "/health", "", "", 200},
		{"ctrail_list_edge_cloud_zones", `{"region":"BR SP&x=1","status":"active"}`, "GET", "/edge-cloud-zones", "region=BR+SP%26x%3D1&status=active", "", 200},
		{"ctrail_list_clusters", `{"region":"br","clusterRef":"edge","edgeCloudZoneId":"zone-1"}`, "GET", "/clusters", "clusterRef=edge&edgeCloudZoneId=zone-1&region=br", "", 200},
		{"ctrail_list_traffic_influences", `{"appId":"app-1","edgeCloudZoneId":"zone-1","state":"active"}`, "GET", "/traffic-influences", "appId=app-1&edgeCloudZoneId=zone-1&state=active", "", 200},
		{"ctrail_get_traffic_influence", `{"trafficInfluenceId":"ti-1"}`, "GET", "/traffic-influences/ti-1", "", "", 200},
		{"ctrail_create_traffic_influence", `{"request":{"appId":"app-1","edgeCloudZoneId":"zone-1","device":{"ipv4Address":{"publicAddress":"10.0.0.1"}}}}`, "POST", "/traffic-influences", "", `{"appId":"app-1","edgeCloudZoneId":"zone-1","device":{"ipv4Address":{"publicAddress":"10.0.0.1"}}}`, 201},
		{"ctrail_update_traffic_influence", `{"trafficInfluenceId":"ti-1","request":{"appId":"app-1","edgeCloudZoneId":"zone-2"}}`, "PUT", "/traffic-influences/ti-1", "", `{"appId":"app-1","edgeCloudZoneId":"zone-2","device":{}}`, 200},
		{"ctrail_delete_traffic_influence", `{"trafficInfluenceId":"ti-1"}`, "DELETE", "/traffic-influences/ti-1", "", "", 204},
		{"ctrail_list_apps", `{}`, "GET", "/apps", "", "", 200},
		{"ctrail_get_app", `{"appId":"app-1"}`, "GET", "/apps/app-1", "", "", 200},
		{"ctrail_submit_app", `{"manifest":{"name":"demo","appProvider":"lab","version":"1","packageType":"CONTAINER","appRepo":{"type":"PUBLICREPO","imagePath":"nginx:stable"},"requiredResources":{"infraKind":"kubernetes","numCPU":"500m","memory":128},"componentSpec":[],"placementPreference":"edge"}}`, "POST", "/apps", "", `{"name":"demo","appProvider":"lab","version":"1","packageType":"CONTAINER","appRepo":{"type":"PUBLICREPO","imagePath":"nginx:stable"},"requiredResources":{"infraKind":"kubernetes","numCPU":"500m","memory":128},"componentSpec":[],"placementPreference":"edge"}`, 201},
		{"ctrail_delete_app", `{"appId":"app-1"}`, "DELETE", "/apps/app-1", "", "", 204},
		{"ctrail_list_deployments", `{"appId":"app-1","appDeploymentId":"dep-1"}`, "GET", "/deployments", "appDeploymentId=dep-1&appId=app-1", "", 200},
		{"ctrail_get_deployment", `{"appDeploymentId":"dep-1"}`, "GET", "/deployments/dep-1", "", "", 200},
		{"ctrail_create_deployment", `{"request":{"appDeploymentName":"demo","appId":"app-1"}}`, "POST", "/deployments", "", `{"appDeploymentName":"demo","appId":"app-1"}`, 202},
		{"ctrail_update_deployment", `{"appDeploymentId":"dep-1","patch":{"edgeCloudZones":["zone-2"],"kubernetesClusterRefs":[]}}`, "PATCH", "/deployments/dep-1", "", `{"edgeCloudZones":["zone-2"],"kubernetesClusterRefs":[]}`, 200},
		{"ctrail_delete_deployment", `{"appDeploymentId":"dep-1"}`, "DELETE", "/deployments/dep-1", "", "", 202},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != tt.method || r.URL.Path != "/gateway"+tt.path || r.URL.RawQuery != tt.query {
					t.Errorf("request = %s %s; want %s /gateway%s?%s", r.Method, r.URL, tt.method, tt.path, tt.query)
				}
				if r.Header.Get("Authorization") != "Bearer upstream-test-token" || r.Header.Get("Accept") != "application/json" || !strings.HasPrefix(r.UserAgent(), "ctrail-mcp/") {
					t.Error("missing gateway authentication or required headers")
				}
				data, _ := io.ReadAll(r.Body)
				if tt.body == "" {
					if len(data) != 0 {
						t.Errorf("unexpected request body: %s", data)
					}
				} else {
					if r.Header.Get("Content-Type") != "application/json" {
						t.Error("incorrect request content type")
					}
					var got, want any
					if err := json.Unmarshal(data, &got); err != nil {
						t.Error(err)
					}
					_ = json.Unmarshal([]byte(tt.body), &want)
					if !reflect.DeepEqual(got, want) {
						t.Errorf("body = %s; want %s", data, tt.body)
					}
				}
				w.Header().Set("Location", "/deployments/dep-1")
				w.Header().Set("x-correlator", "correlation-1")
				w.WriteHeader(tt.status)
				if tt.status != 204 && !(tt.method == "DELETE" && tt.status == 202) {
					_, _ = io.WriteString(w, `{"id":"result-1"}`)
				}
			}))
			defer gateway.Close()
			ctx, session := connectTestClient(t, testConfig(gateway.URL+"/gateway/"))
			result := callTool(t, ctx, session, tt.name, tt.args)
			if result.IsError {
				t.Fatalf("tool error: %+v", result.Content)
			}
			response := decodeResponse(t, result)
			if response.Status != tt.status || response.Location != "/deployments/dep-1" || response.Correlator != "correlation-1" {
				t.Errorf("response = %+v", response)
			}
			if (tt.status == 204 || (tt.method == "DELETE" && tt.status == 202)) && response.Body != nil {
				t.Errorf("empty response body = %v", response.Body)
			}
			if calls.Load() != 1 {
				t.Errorf("gateway calls = %d; want 1", calls.Load())
			}
		})
	}
}

func TestCatalogueReadOnlyAndResource(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		cfg := testConfig("http://localhost:8080")
		cfg.ReadOnly = readOnly
		ctx, session := connectTestClient(t, cfg)
		listed, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := 17
		if readOnly {
			want = 9
		}
		if len(listed.Tools) != want {
			t.Fatalf("readOnly=%v tools=%d, want %d", readOnly, len(listed.Tools), want)
		}
		for _, tool := range listed.Tools {
			if tool.InputSchema == nil || tool.OutputSchema == nil || tool.Annotations == nil {
				t.Errorf("missing schema/annotations for %s", tool.Name)
			}
			if readOnly && !tool.Annotations.ReadOnlyHint {
				t.Errorf("mutating tool exposed: %s", tool.Name)
			}
			if strings.Contains(tool.Name, "delete") && (tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint) {
				t.Errorf("delete not annotated destructive: %s", tool.Name)
			}
		}
		if readOnly {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ctrail_delete_app", Arguments: map[string]any{"appId": "a"}})
			if err == nil && !result.IsError {
				t.Fatal("read-only mode allowed a mutating call")
			}
		}
		resources, err := session.ListResources(ctx, nil)
		if err != nil || len(resources.Resources) != 1 {
			t.Fatalf("resources: %v %v", resources, err)
		}
		resource, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "ctrail://api-guide"})
		if err != nil || len(resource.Contents) != 1 || !strings.Contains(resource.Contents[0].Text, "endpoint readiness") {
			t.Fatalf("guide: %v %v", resource, err)
		}
	}
}

func TestToolErrorsPreserveUpstreamResponse(t *testing.T) {
	for _, status := range []int{302, 400, 401, 403, 404, 409, 429, 502} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"code":"UPSTREAM_ERROR","message":"original"}`)
			}))
			defer gateway.Close()
			ctx, session := connectTestClient(t, testConfig(gateway.URL))
			result := callTool(t, ctx, session, "ctrail_list_apps", `{}`)
			response := decodeResponse(t, result)
			if !result.IsError || response.Status != status || response.RetryAfter != "60" {
				t.Fatalf("lost upstream error: %+v %+v", result, response)
			}
			body, ok := response.Body.(map[string]any)
			if !ok || body["code"] != "UPSTREAM_ERROR" || body["message"] != "original" {
				t.Fatalf("lost CAMARA error body: %v", response.Body)
			}
		})
	}
}

func TestInvalidArgumentsNeverReachGateway(t *testing.T) {
	var calls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer gateway.Close()
	ctx, session := connectTestClient(t, testConfig(gateway.URL))
	for _, args := range []string{
		`{}`, `{"appId":42}`, `{"appId":".."}`, `{"appId":"../deployments"}`,
		`{"appId":"%2e%2e%2fapps"}`, `{"appId":"a?admin=1"}`, `{"appId":"a#fragment"}`,
		`{"appId":"a\\b"}`, `{"appId":" "}`, `{"appId":"a\n"}`, `{"appId":"a","url":"http://other"}`,
	} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ctrail_get_app", Arguments: json.RawMessage(args)})
		if err == nil && !result.IsError {
			t.Errorf("accepted invalid arguments: %s", args)
		}
	}
	// Nested unknown parameters must also be rejected rather than silently dropped.
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ctrail_create_deployment", Arguments: json.RawMessage(`{"request":{"appDeploymentName":"demo","appId":"a","unknown":true}}`)})
	if err == nil && !result.IsError {
		t.Error("accepted unknown nested property")
	}
	if calls.Load() != 0 {
		t.Errorf("invalid arguments triggered %d upstream calls", calls.Load())
	}
}
