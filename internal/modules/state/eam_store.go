package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
)

// This file holds the Edge Application Management state, kept in-memory and
// thread-safe like InMemoryStateManager (Traffic Influence). Two stores are
// provided: an application catalog (onboarded AppManifests — the ETSI MEC 010-2
// "package" resource) and a deployment store (running deployments — the
// "instance" resource, with an ETSI-style lifecycle operation occurrence).
// Keeping package and instance separate is the MEC compliance invariant that
// onboarding an app is distinct from deploying it.

// OpState is the state of a lifecycle operation occurrence (ETSI MEC 010-2
// app_lcm_op_occs pattern), backing the CAMARA 202-Accepted deployment tasks.
type OpState string

const (
	OpProcessing OpState = "PROCESSING"
	OpCompleted  OpState = "COMPLETED"
	OpFailed     OpState = "FAILED"
)

// DeploymentState is the CTRAIL-observed lifecycle state of a deployment,
// aligned with CAMARA CloudEventStatus (DEPLOYING/RUNNING/FAILED/TERMINATING).
type DeploymentState string

const (
	DeploymentDeploying   DeploymentState = "DEPLOYING"
	DeploymentRunning     DeploymentState = "RUNNING"
	DeploymentFailed      DeploymentState = "FAILED"
	DeploymentTerminating DeploymentState = "TERMINATING"
	DeploymentTerminated  DeploymentState = "TERMINATED"
)

// ZonePlacement records where one instance of a deployment was scheduled: the
// edge cloud zone and the concrete Kubernetes objects that realise it. It is the
// CAMARA↔Kubernetes correlation the state module owns (as it does CAMARA↔NEF IDs
// for Traffic Influence).
type ZonePlacement struct {
	ZoneID        string `json:"zoneId"`
	AppInstanceID string `json:"appInstanceId"`
	Namespace     string `json:"namespace"`
	WorkloadName  string `json:"workloadName"`
	// MEPServiceID is the OAI MEP registry id when the placement's external
	// service was registered on the mp1 interface (empty when MEP is disabled).
	MEPServiceID string `json:"mepServiceId,omitempty"`
}

// DeploymentEntry is the internal record for a single deployment.
type DeploymentEntry struct {
	AppDeploymentID   string                       `json:"appDeploymentId"`
	AppDeploymentName string                       `json:"appDeploymentName"`
	AppID             string                       `json:"appId"`
	Request           *camara.AppDeploymentRequest `json:"request"`
	Placements        []ZonePlacement              `json:"placements"`
	State             DeploymentState              `json:"state"`
	OpState           OpState                      `json:"opState"`
	CreatedAt         time.Time                    `json:"createdAt"`
	UpdatedAt         time.Time                    `json:"updatedAt"`
	ErrorMessage      string                       `json:"errorMessage,omitempty"`
}

// AppCatalog stores onboarded AppManifests keyed by appId.
type AppCatalog struct {
	apps  map[string]*camara.AppManifest
	mutex sync.RWMutex
}

// NewAppCatalog creates an empty application catalog.
func NewAppCatalog() *AppCatalog {
	return &AppCatalog{apps: make(map[string]*camara.AppManifest)}
}

// Store persists an onboarded manifest. The appId must be set by the caller.
func (c *AppCatalog) Store(manifest *camara.AppManifest) error {
	if manifest == nil {
		return errors.New("manifest cannot be nil")
	}
	if manifest.AppId == "" {
		return errors.New("appId is required")
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if _, exists := c.apps[manifest.AppId]; exists {
		return fmt.Errorf("app with ID %s already exists", manifest.AppId)
	}
	c.apps[manifest.AppId] = manifest
	return nil
}

// Get returns a copy of the manifest for appId.
func (c *AppCatalog) Get(appID string) (*camara.AppManifest, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	manifest, exists := c.apps[appID]
	if !exists {
		return nil, fmt.Errorf("app with ID %s not found", appID)
	}
	return copyManifest(manifest), nil
}

// List returns copies of all onboarded manifests.
func (c *AppCatalog) List() []*camara.AppManifest {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	out := make([]*camara.AppManifest, 0, len(c.apps))
	for _, m := range c.apps {
		out = append(out, copyManifest(m))
	}
	return out
}

// Delete removes an app from the catalog.
func (c *AppCatalog) Delete(appID string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if _, exists := c.apps[appID]; !exists {
		return fmt.Errorf("app with ID %s not found", appID)
	}
	delete(c.apps, appID)
	return nil
}

// Exists reports whether appID is in the catalog.
func (c *AppCatalog) Exists(appID string) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	_, exists := c.apps[appID]
	return exists
}

// DeploymentStore holds deployments and a reverse index from namespace/workload
// back to the owning appDeploymentId.
type DeploymentStore struct {
	deployments map[string]*DeploymentEntry
	byWorkload  map[string]string // "namespace/workloadName" -> appDeploymentId
	mutex       sync.RWMutex
}

// NewDeploymentStore creates an empty deployment store.
func NewDeploymentStore() *DeploymentStore {
	return &DeploymentStore{
		deployments: make(map[string]*DeploymentEntry),
		byWorkload:  make(map[string]string),
	}
}

// Store persists a new deployment entry, indexing its placements.
func (s *DeploymentStore) Store(entry *DeploymentEntry) error {
	if entry == nil {
		return errors.New("deployment entry cannot be nil")
	}
	if entry.AppDeploymentID == "" {
		return errors.New("appDeploymentId is required")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, exists := s.deployments[entry.AppDeploymentID]; exists {
		return fmt.Errorf("deployment with ID %s already exists", entry.AppDeploymentID)
	}
	now := time.Now()
	entry.CreatedAt = now
	entry.UpdatedAt = now
	s.deployments[entry.AppDeploymentID] = entry
	for _, p := range entry.Placements {
		s.byWorkload[p.Namespace+"/"+p.WorkloadName] = entry.AppDeploymentID
	}
	return nil
}

// Get returns a copy of a deployment entry.
func (s *DeploymentStore) Get(id string) (*DeploymentEntry, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	entry, exists := s.deployments[id]
	if !exists {
		return nil, fmt.Errorf("deployment with ID %s not found", id)
	}
	return copyDeployment(entry), nil
}

// List returns copies of all deployments, optionally filtered by appId ("" = all).
func (s *DeploymentStore) List(appIDFilter string) []*DeploymentEntry {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	out := make([]*DeploymentEntry, 0, len(s.deployments))
	for _, e := range s.deployments {
		if appIDFilter != "" && e.AppID != appIDFilter {
			continue
		}
		out = append(out, copyDeployment(e))
	}
	return out
}

// Update replaces the mutable fields of a stored deployment (placements, state,
// op-occurrence). The reverse index is rebuilt from the new placements.
func (s *DeploymentStore) Update(entry *DeploymentEntry) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	existing, exists := s.deployments[entry.AppDeploymentID]
	if !exists {
		return fmt.Errorf("deployment with ID %s not found", entry.AppDeploymentID)
	}
	for _, p := range existing.Placements {
		delete(s.byWorkload, p.Namespace+"/"+p.WorkloadName)
	}
	entry.CreatedAt = existing.CreatedAt
	entry.UpdatedAt = time.Now()
	s.deployments[entry.AppDeploymentID] = entry
	for _, p := range entry.Placements {
		s.byWorkload[p.Namespace+"/"+p.WorkloadName] = entry.AppDeploymentID
	}
	return nil
}

// SetState transitions a deployment's observed lifecycle state and op-occurrence.
func (s *DeploymentStore) SetState(id string, state DeploymentState, op OpState, errMsg string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	entry, exists := s.deployments[id]
	if !exists {
		return fmt.Errorf("deployment with ID %s not found", id)
	}
	entry.State = state
	entry.OpState = op
	entry.ErrorMessage = errMsg
	entry.UpdatedAt = time.Now()
	return nil
}

// Delete removes a deployment and its index entries.
func (s *DeploymentStore) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	entry, exists := s.deployments[id]
	if !exists {
		return fmt.Errorf("deployment with ID %s not found", id)
	}
	for _, p := range entry.Placements {
		delete(s.byWorkload, p.Namespace+"/"+p.WorkloadName)
	}
	delete(s.deployments, id)
	return nil
}

// CountByApp returns how many deployments reference appID (used to block
// deletion of an app that still has deployments — CAMARA 409 ABORTED).
func (s *DeploymentStore) CountByApp(appID string) int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	n := 0
	for _, e := range s.deployments {
		if e.AppID == appID {
			n++
		}
	}
	return n
}

func copyManifest(m *camara.AppManifest) *camara.AppManifest {
	if m == nil {
		return nil
	}
	data, _ := json.Marshal(m)
	var c camara.AppManifest
	_ = json.Unmarshal(data, &c)
	return &c
}

func copyDeployment(e *DeploymentEntry) *DeploymentEntry {
	if e == nil {
		return nil
	}
	data, _ := json.Marshal(e)
	var c DeploymentEntry
	_ = json.Unmarshal(data, &c)
	return &c
}
