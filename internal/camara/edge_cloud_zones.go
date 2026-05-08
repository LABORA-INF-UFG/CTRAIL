package camara

// EdgeCloudZoneStatus represents the operational status of an edge cloud zone.
type EdgeCloudZoneStatus string

const (
	EdgeCloudZoneStatusActive   EdgeCloudZoneStatus = "active"
	EdgeCloudZoneStatusInactive EdgeCloudZoneStatus = "inactive"
	EdgeCloudZoneStatusUnknown  EdgeCloudZoneStatus = "unknown"
)

// EdgeCloudZone represents an edge cloud zone as defined by the CAMARA
// Edge Application Management API.
type EdgeCloudZone struct {
	EdgeCloudZoneId     string              `json:"edgeCloudZoneId"`
	EdgeCloudZoneName   string              `json:"edgeCloudZoneName"`
	EdgeCloudProvider   string              `json:"edgeCloudProvider"`
	EdgeCloudZoneStatus EdgeCloudZoneStatus `json:"edgeCloudZoneStatus,omitempty"`
	EdgeCloudRegion     string              `json:"edgeCloudRegion,omitempty"`
}
