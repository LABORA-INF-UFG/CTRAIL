package functional

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
)

func getClusters(t *testing.T, tc *EAMTestContext, query string) []camara.ClusterInfo {
	t.Helper()
	w := tc.do(http.MethodGet, "/clusters"+query, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /clusters%s expected 200, got %d", query, w.Code)
	}
	var clusters []camara.ClusterInfo
	if err := json.Unmarshal(w.Body.Bytes(), &clusters); err != nil {
		t.Fatalf("parse clusters: %v", err)
	}
	return clusters
}

func TestGetClusters_ReturnsClustersForZones(t *testing.T) {
	tc := SetupEAMTest(t)
	clusters := getClusters(t, tc, "")
	if len(clusters) == 0 {
		t.Fatal("expected at least one cluster derived from topology zones")
	}
	for i, cl := range clusters {
		if cl.Name == "" || cl.ClusterRef == "" || cl.EdgeCloudZoneId == "" || cl.Provider == "" {
			t.Errorf("cluster[%d] missing required field: %+v", i, cl)
		}
	}
}

func TestGetClusters_FilterByZone(t *testing.T) {
	tc := SetupEAMTest(t)
	clusters := getClusters(t, tc, "?edgeCloudZoneId=camara-mec-zone-1")
	if len(clusters) != 1 {
		t.Fatalf("expected exactly one cluster for the zone, got %d", len(clusters))
	}
	if clusters[0].EdgeCloudZoneId != "camara-mec-zone-1" {
		t.Fatalf("unexpected zone: %s", clusters[0].EdgeCloudZoneId)
	}
}

func TestGetClusters_FilterByRegion(t *testing.T) {
	tc := SetupEAMTest(t)
	clusters := getClusters(t, tc, "?region=north")
	if len(clusters) == 0 {
		t.Fatal("expected clusters in region north")
	}
	for _, cl := range clusters {
		if cl.EdgeCloudRegion != "north" {
			t.Fatalf("region filter leaked a %q cluster", cl.EdgeCloudRegion)
		}
	}
}

func TestGetClusters_UnknownZoneReturnsEmpty(t *testing.T) {
	tc := SetupEAMTest(t)
	clusters := getClusters(t, tc, "?edgeCloudZoneId=no-such-zone")
	if len(clusters) != 0 {
		t.Fatalf("expected empty list for unknown zone, got %d", len(clusters))
	}
}

func TestGetClusters_StableClusterRef(t *testing.T) {
	tc := SetupEAMTest(t)
	first := getClusters(t, tc, "?edgeCloudZoneId=camara-mec-zone-1")
	second := getClusters(t, tc, "?edgeCloudZoneId=camara-mec-zone-1")
	if first[0].ClusterRef != second[0].ClusterRef {
		t.Fatal("clusterRef must be stable across calls")
	}
}
