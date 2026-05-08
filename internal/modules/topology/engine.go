package topology

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/LABORA-INF-UFG/ctrail/internal/nef"
)

// NetworkIdentifiers holds the 3GPP network parameters resolved for a given zone.
type NetworkIdentifiers struct {
	DNN           string      `json:"dnn"`
	SNSSAI        *nef.Snssai `json:"snssai"`
	AppServer     string      `json:"appServer,omitempty"`
	AppServerMask string      `json:"appServerMask,omitempty"`
	UeNetworkMask string      `json:"ueNetworkMask,omitempty"`
}

// ZoneMapping maps a CAMARA edge cloud zone identifier to the underlying 3GPP
// network parameters required by the NEF.
type ZoneMapping struct {
	ZoneID        string             `json:"zoneId"`
	Name          string             `json:"name,omitempty"`
	RegionID      string             `json:"regionId,omitempty"`
	Provider      string             `json:"provider,omitempty"`
	Status        string             `json:"status,omitempty"`
	AppServer     string             `json:"appServer"`
	AppServerMask string             `json:"appServerMask"`
	UeNetworkMask string             `json:"ueNetworkMask"`
	DNAI          string             `json:"dnai"`
	Network       NetworkIdentifiers `json:"network"`
	Description   string             `json:"description,omitempty"`
}

// PolicyRule applies a NetworkIdentifiers set when a condition matches, used
// as a fallback when no direct zone mapping is found.
type PolicyRule struct {
	Condition string             `json:"condition"`
	Network   NetworkIdentifiers `json:"network"`
	Priority  int                `json:"priority"`
}

// TopologyEngine manages zone-to-network mappings and region-based policies.
type TopologyEngine struct {
	zoneMappings  map[string]ZoneMapping
	policyRules   []PolicyRule
	defaultDNN    string
	defaultSNSSAI *nef.Snssai
}

// NewTopologyEngine creates a TopologyEngine initialised with built-in defaults
// and then attempts to overlay them from TOPOLOGY_CONFIG_FILE (default:
// ./config/topology.json).
func NewTopologyEngine() *TopologyEngine {
	engine := &TopologyEngine{
		zoneMappings: make(map[string]ZoneMapping),
		policyRules:  []PolicyRule{},
		defaultDNN:   "internet",
		defaultSNSSAI: &nef.Snssai{
			Sst: 1,
			Sd:  "010203",
		},
	}

	engine.loadDefaultMappings()
	engine.loadFromConfigFile()

	return engine
}

func (te *TopologyEngine) loadDefaultMappings() {
	te.zoneMappings["camara-mec-zone-1"] = ZoneMapping{
		ZoneID:   "camara-mec-zone-1",
		Name:     "MEC Zone North",
		RegionID: "north",
		Provider: "operator",
		Status:   "active",
		DNAI:     "mec",
		Network: NetworkIdentifiers{
			DNN: "mec-internet",
			SNSSAI: &nef.Snssai{
				Sst: 1,
				Sd:  "010203",
			},
		},
		Description: "Default MEC zone mapping",
	}

	te.zoneMappings["camara-edge-zone-central"] = ZoneMapping{
		ZoneID:   "camara-edge-zone-central",
		Name:     "Edge Zone Central",
		RegionID: "central",
		Provider: "operator",
		Status:   "active",
		DNAI:     "central-edge",
		Network: NetworkIdentifiers{
			DNN: "edge-internet",
			SNSSAI: &nef.Snssai{
				Sst: 1,
				Sd:  "020304",
			},
		},
		Description: "Central edge zone mapping",
	}

	te.policyRules = []PolicyRule{
		{
			Condition: "region=north",
			Network: NetworkIdentifiers{
				DNN: "north-internet",
				SNSSAI: &nef.Snssai{Sst: 1, Sd: "030405"},
			},
			Priority: 10,
		},
		{
			Condition: "region=south",
			Network: NetworkIdentifiers{
				DNN: "south-internet",
				SNSSAI: &nef.Snssai{Sst: 1, Sd: "040506"},
			},
			Priority: 10,
		},
	}
}

func (te *TopologyEngine) loadFromConfigFile() {
	configFile := os.Getenv("TOPOLOGY_CONFIG_FILE")
	if configFile == "" {
		configFile = "./config/topology.json"
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Printf("Could not load topology config from %s: %v", configFile, err)
		return
	}

	var config struct {
		ZoneMappings []ZoneMapping `json:"zoneMappings"`
		PolicyRules  []PolicyRule  `json:"policyRules"`
		Default      struct {
			DNN    string      `json:"dnn"`
			SNSSAI *nef.Snssai `json:"snssai"`
		} `json:"default"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("Error parsing topology config: %v", err)
		return
	}

	for _, mapping := range config.ZoneMappings {
		te.zoneMappings[mapping.ZoneID] = mapping
	}

	if len(config.PolicyRules) > 0 {
		te.policyRules = config.PolicyRules
	}

	if config.Default.DNN != "" {
		te.defaultDNN = config.Default.DNN
	}
	if config.Default.SNSSAI != nil {
		te.defaultSNSSAI = config.Default.SNSSAI
	}

	log.Printf("Loaded topology configuration from %s", configFile)
}

// ResolveNetworkIdentifiers returns the 3GPP network parameters for a given
// zone/region/appInstance combination. Resolution order:
//  1. Direct zone ID lookup
//  2. Region-based policy rules
//  3. Built-in defaults
func (te *TopologyEngine) ResolveNetworkIdentifiers(zoneID, regionID, appInstanceID string) (NetworkIdentifiers, error) {
	if zoneID != "" {
		if mapping, exists := te.zoneMappings[zoneID]; exists {
			networkIds := mapping.Network
			networkIds.AppServer = mapping.AppServer
			networkIds.AppServerMask = mapping.AppServerMask
			networkIds.UeNetworkMask = mapping.UeNetworkMask

			log.Printf("Resolved network identifiers for zone %s: DNN=%s, SNSSAI=%+v, AppServer=%s/%s, UeMask=%s",
				zoneID, networkIds.DNN, networkIds.SNSSAI, networkIds.AppServer, networkIds.AppServerMask, networkIds.UeNetworkMask)
			return networkIds, nil
		}
	}

	if regionID != "" {
		for _, rule := range te.policyRules {
			if te.matchesCondition(rule.Condition, regionID, appInstanceID) {
				log.Printf("Applied policy rule for region %s: DNN=%s, SNSSAI=%+v",
					regionID, rule.Network.DNN, rule.Network.SNSSAI)
				return rule.Network, nil
			}
		}
	}

	defaultNetwork := NetworkIdentifiers{
		DNN:           te.defaultDNN,
		SNSSAI:        te.defaultSNSSAI,
		AppServerMask: "16",
		UeNetworkMask: "16",
	}

	log.Printf("Using default network identifiers: DNN=%s, SNSSAI=%+v",
		defaultNetwork.DNN, defaultNetwork.SNSSAI)

	return defaultNetwork, nil
}

// ResolveDNAI returns the DNAI for a given zone ID. Returns an error if the
// zone is not registered.
func (te *TopologyEngine) ResolveDNAI(zoneID string) (string, error) {
	if zoneID == "" {
		return "", errors.New("zone ID is required for DNAI resolution")
	}

	if mapping, exists := te.zoneMappings[zoneID]; exists {
		log.Printf("Resolved DNAI for zone %s: %s", zoneID, mapping.DNAI)
		return mapping.DNAI, nil
	}

	return "", fmt.Errorf("no DNAI mapping found for zone: %s", zoneID)
}

func (te *TopologyEngine) matchesCondition(condition, regionID, appInstanceID string) bool {
	switch condition {
	case fmt.Sprintf("region=%s", regionID):
		return true
	case fmt.Sprintf("appInstance=%s", appInstanceID):
		return true
	default:
		return false
	}
}

// AddZoneMapping registers a new zone mapping at runtime.
func (te *TopologyEngine) AddZoneMapping(mapping ZoneMapping) {
	te.zoneMappings[mapping.ZoneID] = mapping
	log.Printf("Added zone mapping: %s -> DNAI:%s, DNN:%s",
		mapping.ZoneID, mapping.DNAI, mapping.Network.DNN)
}

// RemoveZoneMapping removes a zone mapping by zone ID.
func (te *TopologyEngine) RemoveZoneMapping(zoneID string) {
	delete(te.zoneMappings, zoneID)
	log.Printf("Removed zone mapping for: %s", zoneID)
}

// GetAllZoneMappings returns a snapshot of all registered zone mappings.
func (te *TopologyEngine) GetAllZoneMappings() map[string]ZoneMapping {
	return te.zoneMappings
}

// AddPolicyRule appends a new region-based policy rule.
func (te *TopologyEngine) AddPolicyRule(rule PolicyRule) {
	te.policyRules = append(te.policyRules, rule)
	log.Printf("Added policy rule: %s -> DNN:%s", rule.Condition, rule.Network.DNN)
}

// ValidateConfiguration checks all zone mappings and policy rules for missing
// required fields and returns any errors found.
func (te *TopologyEngine) ValidateConfiguration() []error {
	var errors []error

	for zoneID, mapping := range te.zoneMappings {
		if mapping.DNAI == "" {
			errors = append(errors, fmt.Errorf("zone %s has empty DNAI", zoneID))
		}
		if mapping.Network.DNN == "" {
			errors = append(errors, fmt.Errorf("zone %s has empty DNN", zoneID))
		}
		if mapping.Network.SNSSAI == nil {
			errors = append(errors, fmt.Errorf("zone %s has nil SNSSAI", zoneID))
		}
	}

	for i, rule := range te.policyRules {
		if rule.Condition == "" {
			errors = append(errors, fmt.Errorf("policy rule %d has empty condition", i))
		}
		if rule.Network.DNN == "" {
			errors = append(errors, fmt.Errorf("policy rule %d has empty DNN", i))
		}
	}

	return errors
}
