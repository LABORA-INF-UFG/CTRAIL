package functional

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
)

// =============================================================================
// TEST: GET /edge-cloud-zones
// =============================================================================

// TestListEdgeCloudZones_ReturnsAllZones verifies the endpoint returns all zones
// loaded from the topology engine (default mappings + topology.json).
func TestListEdgeCloudZones_ReturnsAllZones(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(zones) == 0 {
		t.Fatal("Expected at least one edge cloud zone, got empty array")
	}
}

// TestListEdgeCloudZones_ResponseShape verifies every returned zone carries the
// three required CAMARA fields: edgeCloudZoneId, edgeCloudZoneName,
// edgeCloudProvider.
func TestListEdgeCloudZones_ResponseShape(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	for i, z := range zones {
		if z.EdgeCloudZoneId == "" {
			t.Errorf("zone[%d]: edgeCloudZoneId must not be empty", i)
		}
		if z.EdgeCloudZoneName == "" {
			t.Errorf("zone[%d]: edgeCloudZoneName must not be empty (zoneId=%s)", i, z.EdgeCloudZoneId)
		}
		if z.EdgeCloudProvider == "" {
			t.Errorf("zone[%d]: edgeCloudProvider must not be empty (zoneId=%s)", i, z.EdgeCloudZoneId)
		}
	}
}

// TestListEdgeCloudZones_StatusField verifies every zone carries a valid
// edgeCloudZoneStatus value when present.
func TestListEdgeCloudZones_StatusField(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	validStatuses := map[camara.EdgeCloudZoneStatus]bool{
		camara.EdgeCloudZoneStatusActive:   true,
		camara.EdgeCloudZoneStatusInactive: true,
		camara.EdgeCloudZoneStatusUnknown:  true,
	}

	for i, z := range zones {
		if z.EdgeCloudZoneStatus != "" && !validStatuses[z.EdgeCloudZoneStatus] {
			t.Errorf("zone[%d] (id=%s): invalid edgeCloudZoneStatus %q", i, z.EdgeCloudZoneId, z.EdgeCloudZoneStatus)
		}
	}
}

// TestListEdgeCloudZones_KnownZonesPresent verifies that the two zones defined
// in the hardcoded default mappings (always loaded regardless of config file)
// are present in the response.
func TestListEdgeCloudZones_KnownZonesPresent(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	index := make(map[string]camara.EdgeCloudZone, len(zones))
	for _, z := range zones {
		index[z.EdgeCloudZoneId] = z
	}

	// These zones come from loadDefaultMappings() and are always present.
	required := []string{
		"camara-mec-zone-1",
		"camara-edge-zone-central",
	}
	for _, id := range required {
		if _, found := index[id]; !found {
			t.Errorf("Expected zone %q to be present in response", id)
		}
	}
}

// =============================================================================
// TEST: GET /edge-cloud-zones?region=
// =============================================================================

// TestListEdgeCloudZones_FilterByRegion_North verifies that filtering by
// region=north returns only north zones and none from other regions.
func TestListEdgeCloudZones_FilterByRegion_North(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones?region=north", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(zones) == 0 {
		t.Fatal("Expected at least one north zone, got empty array")
	}

	for i, z := range zones {
		if z.EdgeCloudRegion != "north" {
			t.Errorf("zone[%d] (id=%s): expected region 'north', got %q", i, z.EdgeCloudZoneId, z.EdgeCloudRegion)
		}
	}
}

// TestListEdgeCloudZones_FilterByRegion_Central verifies central region filter.
func TestListEdgeCloudZones_FilterByRegion_Central(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones?region=central", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(zones) == 0 {
		t.Fatal("Expected at least one central zone, got empty array")
	}

	for i, z := range zones {
		if z.EdgeCloudRegion != "central" {
			t.Errorf("zone[%d] (id=%s): expected region 'central', got %q", i, z.EdgeCloudZoneId, z.EdgeCloudRegion)
		}
	}
}

// TestListEdgeCloudZones_FilterByRegion_NonExistent verifies that a region
// with no zones returns an empty array (not an error).
func TestListEdgeCloudZones_FilterByRegion_NonExistent(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones?region=does-not-exist", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		// empty JSON array "[]" is valid
		t.Fatalf("Failed to parse response: %v. Body: %s", err, w.Body.String())
	}

	if len(zones) != 0 {
		t.Errorf("Expected empty array for unknown region, got %d zones", len(zones))
	}
}

// TestListEdgeCloudZones_FilterByRegion_IsolatesCorrectly verifies that zones
// from other regions are NOT included when a region filter is applied.
func TestListEdgeCloudZones_FilterByRegion_IsolatesCorrectly(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones?region=south", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	for i, z := range zones {
		if z.EdgeCloudRegion != "south" {
			t.Errorf("zone[%d] (id=%s): leaked non-south zone with region=%q", i, z.EdgeCloudZoneId, z.EdgeCloudRegion)
		}
	}
}

// =============================================================================
// TEST: GET /edge-cloud-zones?status=
// =============================================================================

// TestListEdgeCloudZones_FilterByStatus_Active verifies filtering by status=active.
func TestListEdgeCloudZones_FilterByStatus_Active(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones?status=active", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// All zones in topology.json are active — the list must not be empty
	if len(zones) == 0 {
		t.Fatal("Expected active zones, got empty array")
	}

	for i, z := range zones {
		if z.EdgeCloudZoneStatus != camara.EdgeCloudZoneStatusActive {
			t.Errorf("zone[%d] (id=%s): expected status 'active', got %q", i, z.EdgeCloudZoneId, z.EdgeCloudZoneStatus)
		}
	}
}

// TestListEdgeCloudZones_FilterByStatus_Inactive verifies filtering by
// status=inactive returns an empty array (no inactive zones configured).
func TestListEdgeCloudZones_FilterByStatus_Inactive(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones?status=inactive", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(zones) != 0 {
		t.Errorf("Expected empty array for status=inactive, got %d zones", len(zones))
	}
}

// =============================================================================
// TEST: GET /edge-cloud-zones combined filters
// =============================================================================

// TestListEdgeCloudZones_FilterByRegionAndStatus verifies that both filters
// can be combined and both constraints are applied.
func TestListEdgeCloudZones_FilterByRegionAndStatus(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones?region=north&status=active", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(zones) == 0 {
		t.Fatal("Expected north+active zones, got empty array")
	}

	for i, z := range zones {
		if z.EdgeCloudRegion != "north" {
			t.Errorf("zone[%d] (id=%s): expected region 'north', got %q", i, z.EdgeCloudZoneId, z.EdgeCloudRegion)
		}
		if z.EdgeCloudZoneStatus != camara.EdgeCloudZoneStatusActive {
			t.Errorf("zone[%d] (id=%s): expected status 'active', got %q", i, z.EdgeCloudZoneId, z.EdgeCloudZoneStatus)
		}
	}
}

// TestListEdgeCloudZones_FilterByRegionAndStatus_NoMatch verifies that
// combining filters that yield no results returns an empty array.
func TestListEdgeCloudZones_FilterByRegionAndStatus_NoMatch(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones?region=north&status=inactive", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(zones) != 0 {
		t.Errorf("Expected empty array for north+inactive, got %d zones", len(zones))
	}
}

// =============================================================================
// TEST: Zone IDs usable in Traffic Influence subscriptions
// =============================================================================

// TestListEdgeCloudZones_ZoneIdsAreUsableInTrafficInfluence verifies the
// golden path: discover zones, pick one, create a Traffic Influence
// subscription with it.
func TestListEdgeCloudZones_ZoneIdsAreUsableInTrafficInfluence(t *testing.T) {
	tc := SetupTest(t)
	defer tc.TeardownTest()

	// Step 1: discover zones
	w := tc.makeRequest(http.MethodGet, "/edge-cloud-zones", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /edge-cloud-zones failed: %d %s", w.Code, w.Body.String())
	}

	var zones []camara.EdgeCloudZone
	if err := json.Unmarshal(w.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Failed to parse zones: %v", err)
	}
	if len(zones) == 0 {
		t.Fatal("No zones available to test with")
	}

	// Step 2: use the first returned zone ID in a Traffic Influence subscription
	zoneId := zones[0].EdgeCloudZoneId

	createReq := map[string]any{
		"appId":           "zone-discovery-test-app",
		"edgeCloudZoneId": zoneId,
	}

	createResp := tc.makeRequest(http.MethodPost, "/traffic-influences", createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("POST /traffic-influences with zoneId=%q failed: %d %s",
			zoneId, createResp.Code, createResp.Body.String())
	}

	t.Logf("Successfully created Traffic Influence subscription using discovered zone %q", zoneId)
}
