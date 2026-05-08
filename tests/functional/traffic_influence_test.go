package functional

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/LABORA-INF-UFG/ctrail/internal/nef"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules"
	"github.com/gin-gonic/gin"
)

// MockNEFServer creates a mock NEF server for testing
type MockNEFServer struct {
	*httptest.Server
	subscriptions map[string]*nef.TrafficInfluSub
	nextID        int
}

// NewMockNEFServer creates and starts a new mock NEF server
func NewMockNEFServer() *MockNEFServer {
	mock := &MockNEFServer{
		subscriptions: make(map[string]*nef.TrafficInfluSub),
		nextID:        1,
	}

	handler := http.NewServeMux()

	// Handle all 3GPP Traffic Influence API paths
	// Format: /3gpp-traffic-influence/v1/{afId}/subscriptions[/{subId}]
	handler.HandleFunc("/3gpp-traffic-influence/v1/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Check if path contains /subscriptions/
		if strings.Contains(path, "/subscriptions/") {
			// Operations on specific subscription: GET, PUT, DELETE
			switch r.Method {
			case http.MethodGet:
				mock.handleGet(w, r)
			case http.MethodPut:
				mock.handleUpdate(w, r)
			case http.MethodDelete:
				mock.handleDelete(w, r)
			default:
				http.NotFound(w, r)
			}
			return
		}

		// POST to create subscription (path ends with /subscriptions)
		if r.Method == http.MethodPost && strings.HasSuffix(path, "/subscriptions") {
			mock.handleCreate(w, r)
			return
		}

		http.NotFound(w, r)
	})

	mock.Server = httptest.NewServer(handler)
	return mock
}

func (m *MockNEFServer) handleCreate(w http.ResponseWriter, r *http.Request) {
	var sub nef.TrafficInfluSub
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	subID := fmt.Sprintf("sub-%d", m.nextID)
	m.nextID++

	sub.SubscriptionId = subID
	sub.Self = fmt.Sprintf("%s/3gpp-traffic-influence/v1/subscriptions/%s", m.Server.URL, subID)
	m.subscriptions[subID] = &sub

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(sub)
}

func (m *MockNEFServer) handleGet(w http.ResponseWriter, r *http.Request) {
	subID := extractSubscriptionID(r.URL.Path)
	sub, exists := m.subscriptions[subID]
	if !exists {
		http.Error(w, "Subscription not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sub)
}

func (m *MockNEFServer) handleUpdate(w http.ResponseWriter, r *http.Request) {
	subID := extractSubscriptionID(r.URL.Path)
	_, exists := m.subscriptions[subID]
	if !exists {
		http.Error(w, "Subscription not found", http.StatusNotFound)
		return
	}

	var sub nef.TrafficInfluSub
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sub.SubscriptionId = subID
	sub.Self = fmt.Sprintf("%s/3gpp-traffic-influence/v1/subscriptions/%s", m.Server.URL, subID)
	m.subscriptions[subID] = &sub

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sub)
}

func (m *MockNEFServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	subID := extractSubscriptionID(r.URL.Path)
	_, exists := m.subscriptions[subID]
	if !exists {
		http.Error(w, "Subscription not found", http.StatusNotFound)
		return
	}

	delete(m.subscriptions, subID)
	w.WriteHeader(http.StatusNoContent)
}

func extractSubscriptionID(path string) string {
	// Extract subscription ID from path like:
	// /3gpp-traffic-influence/v1/{afId}/subscriptions/{subId}
	// Find the last segment after /subscriptions/
	idx := strings.LastIndex(path, "/subscriptions/")
	if idx != -1 {
		subPath := path[idx+len("/subscriptions/"):]
		// Remove any trailing slashes
		return strings.TrimSuffix(subPath, "/")
	}
	return ""
}

// TestContext holds test infrastructure
type TestContext struct {
	MockNEF      *MockNEFServer
	Gateway      *modules.GatewayOrchestrator
	Router       *gin.Engine
}

// SetupTest creates a new test context with mock NEF and gateway
func SetupTest(t *testing.T) *TestContext {
	gin.SetMode(gin.TestMode)

	mockNEF := NewMockNEFServer()
	gateway := modules.NewGatewayOrchestrator(mockNEF.URL, "test-token")

	router := gin.New()
	gateway.SetupRoutes(router)

	return &TestContext{
		MockNEF: mockNEF,
		Gateway: gateway,
		Router:  router,
	}
}

// TeardownTest cleans up test resources
func (tc *TestContext) TeardownTest() {
	tc.MockNEF.Close()
}

// Helper function to make requests
func (tc *TestContext) makeRequest(method, path string, body any) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		jsonBody, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(jsonBody)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CTRAIL-Test-Client/1.0")
	// Use Basic Auth with admin credentials (password must be >= 8 chars)
	req.SetBasicAuth("admin", "adminpassword123")

	w := httptest.NewRecorder()
	tc.Router.ServeHTTP(w, req)
	return w
}

// =============================================================================
// TEST: POST /traffic-influences (Create)
// =============================================================================

func TestCreateTrafficInfluence_Success(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	request := camara.TrafficInfluenceRequest{
		AppId:           "test-app-001",
		EdgeCloudZoneId: "zone-north",
		Device: camara.Device{
			IPv4Address: &camara.IPv4Address{
				PublicAddress: "10.0.0.1",
			},
		},
	}

	w := tc.makeRequest(http.MethodPost, "/traffic-influences", request)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var response camara.TrafficInfluenceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TrafficInfluenceID == "" {
		t.Error("Expected TrafficInfluenceID to be set")
	}
	if response.AppId != "test-app-001" {
		t.Errorf("Expected AppId 'test-app-001', got '%s'", response.AppId)
	}
}

func TestCreateTrafficInfluence_WithFilters(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	request := camara.TrafficInfluenceRequest{
		AppId:           "test-app-002",
		EdgeCloudZoneId: "zone-central",
		Device: camara.Device{
			IPv4Address: &camara.IPv4Address{
				PublicAddress: "192.168.1.100",
			},
		},
		SourceTrafficFilters: &camara.SourceTrafficFilter{
			SourcePort: 8080,
		},
		DestinationTrafficFilters: &camara.DestinationTrafficFilter{
			DestinationPort:     443,
			DestinationProtocol: "TCP",
		},
		NotificationUri: "https://example.com/callback",
	}

	w := tc.makeRequest(http.MethodPost, "/traffic-influences", request)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var response camara.TrafficInfluenceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TrafficInfluenceID == "" {
		t.Error("Expected TrafficInfluenceID to be set")
	}
}

func TestCreateTrafficInfluence_MissingAppId(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	request := camara.TrafficInfluenceRequest{
		EdgeCloudZoneId: "zone-north",
	}

	w := tc.makeRequest(http.MethodPost, "/traffic-influences", request)

	// Should fail validation - appId is required
	if w.Code == http.StatusCreated {
		t.Error("Expected validation error for missing appId")
	}
}

func TestCreateTrafficInfluence_InvalidJSON(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	req := httptest.NewRequest(http.MethodPost, "/traffic-influences", bytes.NewBufferString("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")

	w := httptest.NewRecorder()
	tc.Router.ServeHTTP(w, req)

	if w.Code == http.StatusCreated {
		t.Error("Expected error for invalid JSON")
	}
}

// =============================================================================
// TEST: GET /traffic-influences/:id (Read Single)
// =============================================================================

func TestGetTrafficInfluence_Success(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	// First create a subscription
	createRequest := camara.TrafficInfluenceRequest{
		AppId:           "test-app-get",
		EdgeCloudZoneId: "zone-north",
	}

	createResp := tc.makeRequest(http.MethodPost, "/traffic-influences", createRequest)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("Failed to create subscription: %s", createResp.Body.String())
	}

	var created camara.TrafficInfluenceResponse
	json.Unmarshal(createResp.Body.Bytes(), &created)

	// Now get the subscription
	w := tc.makeRequest(http.MethodGet, "/traffic-influences/"+created.TrafficInfluenceID, nil)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var response camara.TrafficInfluenceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TrafficInfluenceID != created.TrafficInfluenceID {
		t.Errorf("Expected ID '%s', got '%s'", created.TrafficInfluenceID, response.TrafficInfluenceID)
	}
	if response.AppId != "test-app-get" {
		t.Errorf("Expected AppId 'test-app-get', got '%s'", response.AppId)
	}
}

func TestGetTrafficInfluence_NotFound(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/traffic-influences/non-existent-id", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// =============================================================================
// TEST: GET /traffic-influences (List All)
// =============================================================================

func TestListTrafficInfluences_Empty(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/traffic-influences", nil)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var response []camara.TrafficInfluenceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		// May return null for empty list, which is fine
		if w.Body.String() != "null" {
			t.Fatalf("Failed to parse response: %v", err)
		}
	}
}

func TestListTrafficInfluences_WithSubscriptions(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	// Create multiple subscriptions
	apps := []string{"app-1", "app-2", "app-3"}
	for _, appId := range apps {
		request := camara.TrafficInfluenceRequest{
			AppId:           appId,
			EdgeCloudZoneId: "zone-north",
		}
		resp := tc.makeRequest(http.MethodPost, "/traffic-influences", request)
		if resp.Code != http.StatusCreated {
			t.Fatalf("Failed to create subscription for %s: %s", appId, resp.Body.String())
		}
	}

	// List all
	w := tc.makeRequest(http.MethodGet, "/traffic-influences", nil)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response []camara.TrafficInfluenceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response) != 3 {
		t.Errorf("Expected 3 subscriptions, got %d", len(response))
	}
}

func TestListTrafficInfluences_WithAppIdFilter(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	// Create subscriptions with different appIds
	apps := []string{"filter-app-1", "filter-app-2", "filter-app-1"}
	for _, appId := range apps {
		request := camara.TrafficInfluenceRequest{
			AppId:           appId,
			EdgeCloudZoneId: "zone-north",
		}
		tc.makeRequest(http.MethodPost, "/traffic-influences", request)
	}

	// Filter by appId
	w := tc.makeRequest(http.MethodGet, "/traffic-influences?appId=filter-app-1", nil)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response []camara.TrafficInfluenceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("Expected 2 subscriptions with appId 'filter-app-1', got %d", len(response))
	}
}

// =============================================================================
// TEST: PUT /traffic-influences/:id (Update)
// =============================================================================

func TestUpdateTrafficInfluence_Success(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	// First create a subscription
	createRequest := camara.TrafficInfluenceRequest{
		AppId:           "test-app-update",
		EdgeCloudZoneId: "zone-north",
	}

	createResp := tc.makeRequest(http.MethodPost, "/traffic-influences", createRequest)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("Failed to create subscription: %s", createResp.Body.String())
	}

	var created camara.TrafficInfluenceResponse
	json.Unmarshal(createResp.Body.Bytes(), &created)

	// Update the subscription
	updateRequest := camara.TrafficInfluenceRequest{
		AppId:           "test-app-update",
		EdgeCloudZoneId: "zone-south", // Changed zone
		NotificationUri: "https://updated.example.com/callback",
	}

	w := tc.makeRequest(http.MethodPut, "/traffic-influences/"+created.TrafficInfluenceID, updateRequest)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var response camara.TrafficInfluenceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TrafficInfluenceID != created.TrafficInfluenceID {
		t.Errorf("Expected ID to remain '%s', got '%s'", created.TrafficInfluenceID, response.TrafficInfluenceID)
	}
}

func TestUpdateTrafficInfluence_NotFound(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	updateRequest := camara.TrafficInfluenceRequest{
		AppId:           "test-app",
		EdgeCloudZoneId: "zone-north",
	}

	w := tc.makeRequest(http.MethodPut, "/traffic-influences/non-existent-id", updateRequest)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// =============================================================================
// TEST: DELETE /traffic-influences/:id (Delete)
// =============================================================================

func TestDeleteTrafficInfluence_Success(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	// First create a subscription
	createRequest := camara.TrafficInfluenceRequest{
		AppId:           "test-app-delete",
		EdgeCloudZoneId: "zone-north",
	}

	createResp := tc.makeRequest(http.MethodPost, "/traffic-influences", createRequest)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("Failed to create subscription: %s", createResp.Body.String())
	}

	var created camara.TrafficInfluenceResponse
	json.Unmarshal(createResp.Body.Bytes(), &created)

	// Delete the subscription
	w := tc.makeRequest(http.MethodDelete, "/traffic-influences/"+created.TrafficInfluenceID, nil)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusNoContent, w.Code, w.Body.String())
	}

	// Verify it's deleted - GET should return 404
	getResp := tc.makeRequest(http.MethodGet, "/traffic-influences/"+created.TrafficInfluenceID, nil)
	if getResp.Code != http.StatusNotFound {
		t.Errorf("Expected subscription to be deleted, but GET returned %d", getResp.Code)
	}
}

func TestDeleteTrafficInfluence_NotFound(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodDelete, "/traffic-influences/non-existent-id", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

// =============================================================================
// TEST: Full CRUD Lifecycle
// =============================================================================

func TestTrafficInfluence_FullCRUDLifecycle(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	// 1. CREATE
	createRequest := camara.TrafficInfluenceRequest{
		AppId:           "lifecycle-test-app",
		EdgeCloudZoneId: "zone-north",
		Device: camara.Device{
			IPv4Address: &camara.IPv4Address{
				PublicAddress: "10.0.0.100",
			},
		},
		NotificationUri: "https://example.com/notify",
	}

	createResp := tc.makeRequest(http.MethodPost, "/traffic-influences", createRequest)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("CREATE failed: %s", createResp.Body.String())
	}

	var created camara.TrafficInfluenceResponse
	json.Unmarshal(createResp.Body.Bytes(), &created)
	subscriptionID := created.TrafficInfluenceID
	t.Logf("Created subscription: %s", subscriptionID)

	// 2. READ
	readResp := tc.makeRequest(http.MethodGet, "/traffic-influences/"+subscriptionID, nil)
	if readResp.Code != http.StatusOK {
		t.Fatalf("READ failed: %s", readResp.Body.String())
	}

	var read camara.TrafficInfluenceResponse
	json.Unmarshal(readResp.Body.Bytes(), &read)
	if read.AppId != "lifecycle-test-app" {
		t.Errorf("READ: Expected AppId 'lifecycle-test-app', got '%s'", read.AppId)
	}
	t.Log("Read subscription successfully")

	// 3. UPDATE
	updateRequest := camara.TrafficInfluenceRequest{
		AppId:           "lifecycle-test-app",
		EdgeCloudZoneId: "zone-south",
		NotificationUri: "https://updated.example.com/notify",
	}

	updateResp := tc.makeRequest(http.MethodPut, "/traffic-influences/"+subscriptionID, updateRequest)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("UPDATE failed: %s", updateResp.Body.String())
	}
	t.Log("Updated subscription successfully")

	// 4. LIST (verify it appears in list)
	listResp := tc.makeRequest(http.MethodGet, "/traffic-influences", nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("LIST failed: %s", listResp.Body.String())
	}

	var list []camara.TrafficInfluenceResponse
	json.Unmarshal(listResp.Body.Bytes(), &list)
	found := false
	for _, item := range list {
		if item.TrafficInfluenceID == subscriptionID {
			found = true
			break
		}
	}
	if !found {
		t.Error("LIST: Subscription not found in list")
	}
	t.Log("Listed subscriptions successfully")

	// 5. DELETE
	deleteResp := tc.makeRequest(http.MethodDelete, "/traffic-influences/"+subscriptionID, nil)
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("DELETE failed: %s", deleteResp.Body.String())
	}
	t.Log("Deleted subscription successfully")

	// 6. Verify DELETE (should return 404)
	verifyResp := tc.makeRequest(http.MethodGet, "/traffic-influences/"+subscriptionID, nil)
	if verifyResp.Code != http.StatusNotFound {
		t.Errorf("Expected 404 after delete, got %d", verifyResp.Code)
	}
	t.Log("Verified subscription deletion")

	t.Log("Full CRUD lifecycle test completed successfully")
}

// =============================================================================
// TEST: Health Check Endpoint
// =============================================================================

func TestHealthCheck(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/admin/health", nil)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%v'", response["status"])
	}
}
