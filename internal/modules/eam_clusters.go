package modules

import (
	"net/http"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Clusters (/clusters) — the Kubernetes clusters that back the edge cloud zones.
// CTRAIL derives cluster information from the topology engine: each zone that is
// mapped to an edge cloud maps to one cluster. This is the infrastructure view
// the MEAO exposes; it is read-only.

// HandleGetClusters lists available clusters (GET /clusters), optionally filtered
// by region, clusterRef, or edgeCloudZoneId query parameters.
func (e *EAMOrchestrator) HandleGetClusters(c *gin.Context) {
	regionFilter := c.Query("region")
	clusterRefFilter := c.Query("clusterRef")
	zoneFilter := c.Query("edgeCloudZoneId")

	out := make([]camara.ClusterInfo, 0)
	for zoneID, mapping := range e.topology.GetAllZoneMappings() {
		clusterRef := mapping.ClusterRef
		if clusterRef == "" {
			// Derive a stable cluster identifier for zones that do not declare one.
			clusterRef = uuid.NewSHA1(uuid.NameSpaceURL, []byte(zoneID)).String()
		}

		provider := mapping.Provider
		if provider == "" {
			provider = "operator"
		}
		name := mapping.Name
		if name == "" {
			name = zoneID
		}

		if regionFilter != "" && mapping.RegionID != regionFilter {
			continue
		}
		if clusterRefFilter != "" && clusterRef != clusterRefFilter {
			continue
		}
		if zoneFilter != "" && zoneID != zoneFilter {
			continue
		}

		out = append(out, camara.ClusterInfo{
			Name:            name,
			Provider:        provider,
			ClusterRef:      clusterRef,
			EdgeCloudZoneId: zoneID,
			EdgeCloudRegion: mapping.RegionID,
			Version:         mapping.K8sVersion,
		})
	}

	c.JSON(http.StatusOK, out)
}
