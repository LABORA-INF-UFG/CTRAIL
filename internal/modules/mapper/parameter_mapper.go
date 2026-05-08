package mapper

import (
	"fmt"
	"log"
	"strings"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/topology"
	"github.com/LABORA-INF-UFG/ctrail/internal/nef"
)

// MappingType defines the available mapping types
type MappingType int

const (
	DirectMapping MappingType = iota
	StructuralMapping
	LogicalMapping
)

// MappingResult encapsulates the result of a mapping operation
type MappingResult struct {
	Success      bool
	ErrorMessage string
	MappingType  MappingType
}

// ParameterMapper interface defines the mapping methods
type ParameterMapper interface {
	MapToNEF(request *camara.TrafficInfluenceRequest) (*nef.TrafficInfluSub, error)
	MapFromNEF(nefSub *nef.TrafficInfluSub, originalRequest *camara.TrafficInfluenceRequest) (*camara.TrafficInfluenceResponse, error)
	ValidateMapping(request *camara.TrafficInfluenceRequest) error
}

// TrafficInfluenceMapper implements parameter mapping for Traffic Influence
type TrafficInfluenceMapper struct {
	topologyEngine *topology.TopologyEngine
}

// NewTrafficInfluenceMapper creates a new mapper instance
func NewTrafficInfluenceMapper(topologyEngine *topology.TopologyEngine) *TrafficInfluenceMapper {
	return &TrafficInfluenceMapper{
		topologyEngine: topologyEngine,
	}
}

// MapToNEF converts a CAMARA request to a NEF subscription
func (tim *TrafficInfluenceMapper) MapToNEF(request *camara.TrafficInfluenceRequest) (*nef.TrafficInfluSub, error) {
	log.Printf("Starting parameter mapping for AppId: %s", request.AppId)

	nefSub := &nef.TrafficInfluSub{}

	// 1. Direct Mapping - fields with direct correspondence
	if err := tim.performDirectMapping(request, nefSub); err != nil {
		return nil, fmt.Errorf("direct mapping failed: %w", err)
	}

	// 2. Logical Mapping with Enrichment - use the topology engine
	networkIds, err := tim.performLogicalMapping(request, nefSub)
	if err != nil {
		return nil, fmt.Errorf("logical mapping failed: %w", err)
	}

	// 3. Structural Mapping - conversion of complex structures
	if err := tim.performStructuralMapping(request, nefSub, networkIds); err != nil {
		return nil, fmt.Errorf("structural mapping failed: %w", err)
	}

	log.Printf("Parameter mapping completed successfully for AppId: %s", request.AppId)
	return nefSub, nil
}

// performDirectMapping executes direct field-to-field mappings
func (tim *TrafficInfluenceMapper) performDirectMapping(request *camara.TrafficInfluenceRequest, nefSub *nef.TrafficInfluSub) error {
	log.Printf("Performing direct mapping")

	nefSub.AfServiceId = request.AppId

	// NotificationUri -> NotificationDestination (direct mapping)
	if request.NotificationUri != "" {
		nefSub.NotificationDestination = request.NotificationUri
	}

	log.Printf("Direct mapping completed: AfServiceId=%s", nefSub.AfServiceId)
	return nil
}

// performLogicalMapping executes mappings that require data enrichment
func (tim *TrafficInfluenceMapper) performLogicalMapping(request *camara.TrafficInfluenceRequest, nefSub *nef.TrafficInfluSub) (topology.NetworkIdentifiers, error) {
	log.Printf("Performing logical mapping with topology enrichment")

	// Resolve network identifiers using the topology engine
	networkIds, err := tim.topologyEngine.ResolveNetworkIdentifiers(
		request.EdgeCloudZoneId,
		request.EdgeCloudRegion,
		request.AppInstanceId,
	)
	if err != nil {
		return networkIds, fmt.Errorf("failed to resolve network identifiers: %w", err)
	}

	// Map to NEF
	nefSub.Dnn = networkIds.DNN
	nefSub.Snssai = networkIds.SNSSAI

	// Resolve DNAI if zone ID was provided
	if request.EdgeCloudZoneId != "" {
		dnai, err := tim.topologyEngine.ResolveDNAI(request.EdgeCloudZoneId)
		if err != nil {
			log.Printf("Warning: Could not resolve DNAI for zone %s: %v", request.EdgeCloudZoneId, err)
		} else {
			nefSub.TrafficRoutes = []nef.TrafficRoute{
				{Dnai: dnai},
			}
		}
	}

	log.Printf("Logical mapping completed: DNN=%s, SNSSAI=%+v", nefSub.Dnn, nefSub.Snssai)
	return networkIds, nil
}

// performStructuralMapping converts simple CAMARA structures to complex NEF structures
func (tim *TrafficInfluenceMapper) performStructuralMapping(request *camara.TrafficInfluenceRequest, nefSub *nef.TrafficInfluSub, networkIds topology.NetworkIdentifiers) error {
	log.Printf("Performing structural mapping")

	// Map device information to NEF indicators
	tim.mapDeviceToNEF(request, nefSub)

	// Convert CAMARA traffic filters to NEF FlowInfo
	flowInfos, err := tim.mapTrafficFiltersToFlowInfo(request, networkIds)
	if err != nil {
		return fmt.Errorf("failed to map traffic filters: %w", err)
	}
	nefSub.TrafficFilters = flowInfos

	log.Printf("Structural mapping completed: %d traffic filters created", len(nefSub.TrafficFilters))
	return nil
}

// mapDeviceToNEF maps device information
func (tim *TrafficInfluenceMapper) mapDeviceToNEF(request *camara.TrafficInfluenceRequest, nefSub *nef.TrafficInfluSub) {
	if request.Device.IPv4Address == nil && request.Device.PhoneNumber == "" {
		// No specific device - apply to any UE
		anyUE := true
		nefSub.AnyUeInd = &anyUE
		log.Printf("Device mapping: AnyUE indicator set to true")
	} else {
		// Specific device
		anyUE := true
		nefSub.AnyUeInd = &anyUE

		if request.Device.PhoneNumber != "" {
			nefSub.Gpsi = request.Device.PhoneNumber
			log.Printf("Device mapping: GPSI set to %s", nefSub.Gpsi)
		}
	}
}

// mapTrafficFiltersToFlowInfo converts CAMARA filters to NEF FlowInfo
func (tim *TrafficInfluenceMapper) mapTrafficFiltersToFlowInfo(request *camara.TrafficInfluenceRequest, networkIds topology.NetworkIdentifiers) ([]nef.FlowInfo, error) {
	// Use AppServer from topology or default value
	appServerIP := networkIds.AppServer
	if appServerIP == "" {
		appServerIP = "8.8.8.8" // Default fallback
	}

	// Use masks from topology or default values
	appServerMask := networkIds.AppServerMask
	if appServerMask == "" {
		appServerMask = "16" // Default fallback
	}

	ueNetworkMask := networkIds.UeNetworkMask
	if ueNetworkMask == "" {
		ueNetworkMask = "16" // Default fallback
	}

	// Determine UE IP
	ueIP := "any"
	if request.Device.IPv4Address != nil && request.Device.IPv4Address.PublicAddress != "" {
		ueIP = request.Device.IPv4Address.PublicAddress
	}

	// Build flow descriptions based on filters
	var flowDescriptions []string

	// Determine protocol
	protocol := "ip"
	if request.DestinationTrafficFilters != nil && request.DestinationTrafficFilters.DestinationProtocol != "" {
		protocol = tim.mapProtocolToNumber(request.DestinationTrafficFilters.DestinationProtocol)
	}

	// Build uplink rule (UE -> Server)
	uplinkRule := tim.buildFlowRule("permit", "out", protocol, appServerIP, appServerMask, ueIP, ueNetworkMask, request)
	flowDescriptions = append(flowDescriptions, uplinkRule)

	// Optionally, build downlink rule (Server -> UE)
	// downlinkRule := tim.buildFlowRule("permit", "in", protocol, appServerIP, appServerMask, ueIP, ueNetworkMask, request)
	// flowDescriptions = append(flowDescriptions, downlinkRule)

	flowId := 1
	flowInfo := nef.FlowInfo{
		FlowId:           &flowId,
		FlowDescriptions: flowDescriptions,
	}

	log.Printf("Created FlowInfo with %d flow descriptions using AppServer=%s/%s, UeMask=%s",
		len(flowDescriptions), appServerIP, appServerMask, ueNetworkMask)
	return []nef.FlowInfo{flowInfo}, nil
}

// buildFlowRule builds a flow description rule
func (tim *TrafficInfluenceMapper) buildFlowRule(action, direction, protocol, srcIP, srcMask, dstIP, dstMask string, request *camara.TrafficInfluenceRequest) string {
	// Build port strings
	srcPort := ""
	dstPort := ""

	if request.SourceTrafficFilters != nil && request.SourceTrafficFilters.SourcePort > 0 {
		srcPort = fmt.Sprintf(" %d", request.SourceTrafficFilters.SourcePort)
	}

	if request.DestinationTrafficFilters != nil && request.DestinationTrafficFilters.DestinationPort > 0 {
		dstPort = fmt.Sprintf(" %d", request.DestinationTrafficFilters.DestinationPort)
	}

	// Build the rule with topology masks
	rule := fmt.Sprintf("%s %s %s from %s%s/%s to %s%s/%s",
		action, direction, protocol,
		srcIP, srcPort, srcMask,
		dstIP, dstPort, dstMask)

	log.Printf("Built flow rule: %s", rule)
	return rule
}

// mapProtocolToNumber converts protocol name to IANA number
func (tim *TrafficInfluenceMapper) mapProtocolToNumber(protocol string) string {
	switch strings.ToUpper(protocol) {
	case "TCP":
		return "6"
	case "UDP":
		return "17"
	case "ICMP":
		return "1"
	case "ICMPV6":
		return "58"
	case "SCTP":
		return "132"
	default:
		log.Printf("Unknown protocol '%s', using 'ip' as default", protocol)
		return "ip"
	}
}

// MapFromNEF converts a NEF response to a CAMARA response
func (tim *TrafficInfluenceMapper) MapFromNEF(nefSub *nef.TrafficInfluSub, originalRequest *camara.TrafficInfluenceRequest) (*camara.TrafficInfluenceResponse, error) {
	if nefSub == nil {
		return nil, fmt.Errorf("NEF subscription is nil")
	}

	log.Printf("Mapping NEF response to CAMARA for subscription: %s", nefSub.SubscriptionId)

	response := &camara.TrafficInfluenceResponse{
		TrafficInfluenceID: nefSub.SubscriptionId,
		AppId:              nefSub.AfServiceId, // Use AfServiceId instead of AfAppId
		State:              "ordered",          // Initial state per CAMARA spec
	}

	// Copy fields from original request, if available
	if originalRequest != nil {
		response.EdgeCloudZoneId = originalRequest.EdgeCloudZoneId
		response.EdgeCloudRegion = originalRequest.EdgeCloudRegion
		response.AppInstanceId = originalRequest.AppInstanceId
		response.NotificationUri = originalRequest.NotificationUri

		// Copy traffic filters
		response.SourceTrafficFilters = originalRequest.SourceTrafficFilters
		response.DestinationTrafficFilters = originalRequest.DestinationTrafficFilters
	}

	log.Printf("CAMARA response mapping completed for TrafficInfluenceID: %s", response.TrafficInfluenceID)
	return response, nil
}

// ValidateMapping validates if a mapping can be performed
func (tim *TrafficInfluenceMapper) ValidateMapping(request *camara.TrafficInfluenceRequest) error {
	// Validate if required parameters are present
	if request.AppId == "" {
		return fmt.Errorf("AppId is required for mapping")
	}

	// Validate if at least one location identifier is present
	if request.EdgeCloudZoneId == "" && request.EdgeCloudRegion == "" && request.AppInstanceId == "" {
		return fmt.Errorf("at least one location identifier is required")
	}

	// Validate if the topology engine can resolve the identifiers
	_, err := tim.topologyEngine.ResolveNetworkIdentifiers(
		request.EdgeCloudZoneId,
		request.EdgeCloudRegion,
		request.AppInstanceId,
	)
	if err != nil {
		return fmt.Errorf("cannot resolve network identifiers: %w", err)
	}

	return nil
}
