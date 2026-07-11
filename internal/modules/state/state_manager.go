package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/LABORA-INF-UFG/ctrail/internal/nef"
)

// SubscriptionState represents the lifecycle state of a Traffic Influence subscription.
type SubscriptionState string

const (
	StateOrdered  SubscriptionState = "ordered"
	StateCreated  SubscriptionState = "created"
	StateActive   SubscriptionState = "active"
	StateInactive SubscriptionState = "inactive"
	StateError    SubscriptionState = "error"
	StateDeleted  SubscriptionState = "deleted"
)

// SubscriptionEntry is the internal state record for a single subscription.
// It stores both the gateway-assigned ID and the NEF subscription ID so that
// lookups can be performed in either direction.
type SubscriptionEntry struct {
	TrafficInfluenceID string                          `json:"trafficInfluenceId"`
	NEFSubscriptionID  string                          `json:"nefSubscriptionId"`
	State              SubscriptionState               `json:"state"`
	OriginalRequest    *camara.TrafficInfluenceRequest `json:"originalRequest"`
	NEFSubscription    *nef.TrafficInfluSub            `json:"nefSubscription"`
	CreatedAt          time.Time                       `json:"createdAt"`
	UpdatedAt          time.Time                       `json:"updatedAt"`
	LastNotificationAt *time.Time                      `json:"lastNotificationAt,omitempty"`
	ErrorMessage       string                          `json:"errorMessage,omitempty"`
	Metadata           map[string]any                  `json:"metadata,omitempty"`
}

// StateManager defines the subscription lifecycle management contract.
type StateManager interface {
	StoreSubscription(entry *SubscriptionEntry) error
	GetSubscription(trafficInfluenceID string) (*SubscriptionEntry, error)
	GetSubscriptionByNEFID(nefSubscriptionID string) (*SubscriptionEntry, error)
	UpdateSubscriptionState(trafficInfluenceID string, state SubscriptionState, errorMessage string) error
	UpdateLastNotification(trafficInfluenceID string) error
	UpdateNEFSubscriptionID(trafficInfluenceID, nefSubscriptionID string) error
	DeleteSubscription(trafficInfluenceID string) error
	ListSubscriptions() ([]*SubscriptionEntry, error)
	ListSubscriptionsByState(state SubscriptionState) ([]*SubscriptionEntry, error)
	CleanupExpiredSubscriptions(maxAge time.Duration) (int, error)
	GetStatistics() map[string]any
}

// InMemoryStateManager is a thread-safe in-memory implementation of StateManager.
// It maintains a secondary index (nefIndex) for O(1) lookups by NEF subscription ID.
type InMemoryStateManager struct {
	subscriptions map[string]*SubscriptionEntry
	nefIndex      map[string]string // nefSubscriptionID -> trafficInfluenceID
	mutex         sync.RWMutex
}

// NewInMemoryStateManager creates an empty in-memory state manager.
func NewInMemoryStateManager() *InMemoryStateManager {
	return &InMemoryStateManager{
		subscriptions: make(map[string]*SubscriptionEntry),
		nefIndex:      make(map[string]string),
	}
}

// StoreSubscription persists a new subscription entry.
func (ism *InMemoryStateManager) StoreSubscription(entry *SubscriptionEntry) error {
	if entry == nil {
		return errors.New("subscription entry cannot be nil")
	}

	if entry.TrafficInfluenceID == "" {
		return errors.New("trafficInfluenceId is required")
	}

	ism.mutex.Lock()
	defer ism.mutex.Unlock()

	if _, exists := ism.subscriptions[entry.TrafficInfluenceID]; exists {
		return fmt.Errorf("subscription with ID %s already exists", entry.TrafficInfluenceID)
	}

	now := time.Now()
	entry.CreatedAt = now
	entry.UpdatedAt = now

	if entry.State == "" {
		entry.State = StateOrdered
	}

	ism.subscriptions[entry.TrafficInfluenceID] = entry

	if entry.NEFSubscriptionID != "" {
		ism.nefIndex[entry.NEFSubscriptionID] = entry.TrafficInfluenceID
	}

	log.Printf("Stored subscription: %s (NEF: %s, State: %s)",
		entry.TrafficInfluenceID, entry.NEFSubscriptionID, entry.State)

	return nil
}

// GetSubscription retrieves a subscription by its gateway-assigned ID.
// Returns a deep copy to prevent accidental mutation of internal state.
func (ism *InMemoryStateManager) GetSubscription(trafficInfluenceID string) (*SubscriptionEntry, error) {
	if trafficInfluenceID == "" {
		return nil, errors.New("trafficInfluenceId is required")
	}

	ism.mutex.RLock()
	defer ism.mutex.RUnlock()

	entry, exists := ism.subscriptions[trafficInfluenceID]
	if !exists {
		return nil, fmt.Errorf("subscription with ID %s not found", trafficInfluenceID)
	}

	return ism.copyEntry(entry), nil
}

// GetSubscriptionByNEFID retrieves a subscription by its NEF-assigned ID.
func (ism *InMemoryStateManager) GetSubscriptionByNEFID(nefSubscriptionID string) (*SubscriptionEntry, error) {
	if nefSubscriptionID == "" {
		return nil, errors.New("nefSubscriptionId is required")
	}

	ism.mutex.RLock()
	defer ism.mutex.RUnlock()

	trafficInfluenceID, exists := ism.nefIndex[nefSubscriptionID]
	if !exists {
		return nil, fmt.Errorf("subscription with NEF ID %s not found", nefSubscriptionID)
	}

	entry, exists := ism.subscriptions[trafficInfluenceID]
	if !exists {
		// Index is inconsistent — clean it up and surface the error.
		delete(ism.nefIndex, nefSubscriptionID)
		return nil, fmt.Errorf("subscription with NEF ID %s not found in main store", nefSubscriptionID)
	}

	return ism.copyEntry(entry), nil
}

// UpdateSubscriptionState transitions a subscription to a new state.
func (ism *InMemoryStateManager) UpdateSubscriptionState(trafficInfluenceID string, state SubscriptionState, errorMessage string) error {
	if trafficInfluenceID == "" {
		return errors.New("trafficInfluenceId is required")
	}

	ism.mutex.Lock()
	defer ism.mutex.Unlock()

	entry, exists := ism.subscriptions[trafficInfluenceID]
	if !exists {
		return fmt.Errorf("subscription with ID %s not found", trafficInfluenceID)
	}

	oldState := entry.State
	entry.State = state
	entry.UpdatedAt = time.Now()
	entry.ErrorMessage = errorMessage

	log.Printf("Updated subscription %s state: %s -> %s", trafficInfluenceID, oldState, state)

	if errorMessage != "" {
		log.Printf("Subscription %s error: %s", trafficInfluenceID, errorMessage)
	}

	return nil
}

// UpdateLastNotification records the current time as the last notification timestamp.
func (ism *InMemoryStateManager) UpdateLastNotification(trafficInfluenceID string) error {
	if trafficInfluenceID == "" {
		return errors.New("trafficInfluenceId is required")
	}

	ism.mutex.Lock()
	defer ism.mutex.Unlock()

	entry, exists := ism.subscriptions[trafficInfluenceID]
	if !exists {
		return fmt.Errorf("subscription with ID %s not found", trafficInfluenceID)
	}

	now := time.Now()
	entry.LastNotificationAt = &now
	entry.UpdatedAt = now

	log.Printf("Updated last notification time for subscription: %s", trafficInfluenceID)
	return nil
}

// DeleteSubscription removes a subscription and its NEF index entry.
func (ism *InMemoryStateManager) DeleteSubscription(trafficInfluenceID string) error {
	if trafficInfluenceID == "" {
		return errors.New("trafficInfluenceId is required")
	}

	ism.mutex.Lock()
	defer ism.mutex.Unlock()

	entry, exists := ism.subscriptions[trafficInfluenceID]
	if !exists {
		return fmt.Errorf("subscription with ID %s not found", trafficInfluenceID)
	}

	if entry.NEFSubscriptionID != "" {
		delete(ism.nefIndex, entry.NEFSubscriptionID)
	}

	delete(ism.subscriptions, trafficInfluenceID)

	log.Printf("Deleted subscription: %s", trafficInfluenceID)
	return nil
}

// ListSubscriptions returns a copy of all subscriptions regardless of state.
func (ism *InMemoryStateManager) ListSubscriptions() ([]*SubscriptionEntry, error) {
	ism.mutex.RLock()
	defer ism.mutex.RUnlock()

	entries := make([]*SubscriptionEntry, 0, len(ism.subscriptions))
	for _, entry := range ism.subscriptions {
		entries = append(entries, ism.copyEntry(entry))
	}

	log.Printf("Listed %d subscriptions", len(entries))
	return entries, nil
}

// ListSubscriptionsByState returns all subscriptions in the given state.
func (ism *InMemoryStateManager) ListSubscriptionsByState(state SubscriptionState) ([]*SubscriptionEntry, error) {
	ism.mutex.RLock()
	defer ism.mutex.RUnlock()

	var entries []*SubscriptionEntry
	for _, entry := range ism.subscriptions {
		if entry.State == state {
			entries = append(entries, ism.copyEntry(entry))
		}
	}

	log.Printf("Listed %d subscriptions with state: %s", len(entries), state)
	return entries, nil
}

// CleanupExpiredSubscriptions removes deleted/errored subscriptions older than maxAge.
func (ism *InMemoryStateManager) CleanupExpiredSubscriptions(maxAge time.Duration) (int, error) {
	ism.mutex.Lock()
	defer ism.mutex.Unlock()

	cutoff := time.Now().Add(-maxAge)
	var toDelete []string

	for id, entry := range ism.subscriptions {
		if entry.UpdatedAt.Before(cutoff) &&
			(entry.State == StateDeleted || entry.State == StateError) {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		entry := ism.subscriptions[id]
		if entry.NEFSubscriptionID != "" {
			delete(ism.nefIndex, entry.NEFSubscriptionID)
		}
		delete(ism.subscriptions, id)
	}

	log.Printf("Cleaned up %d expired subscriptions", len(toDelete))
	return len(toDelete), nil
}

// UpdateNEFSubscriptionID links an existing gateway subscription to its NEF counterpart.
func (ism *InMemoryStateManager) UpdateNEFSubscriptionID(trafficInfluenceID, nefSubscriptionID string) error {
	if trafficInfluenceID == "" {
		return errors.New("trafficInfluenceId is required")
	}

	ism.mutex.Lock()
	defer ism.mutex.Unlock()

	entry, exists := ism.subscriptions[trafficInfluenceID]
	if !exists {
		return fmt.Errorf("subscription with ID %s not found", trafficInfluenceID)
	}

	if entry.NEFSubscriptionID != "" {
		delete(ism.nefIndex, entry.NEFSubscriptionID)
	}

	entry.NEFSubscriptionID = nefSubscriptionID
	entry.UpdatedAt = time.Now()

	if nefSubscriptionID != "" {
		ism.nefIndex[nefSubscriptionID] = trafficInfluenceID
	}

	log.Printf("Updated NEF subscription ID for %s: %s", trafficInfluenceID, nefSubscriptionID)
	return nil
}

// GetStatistics returns subscription counts broken down by state.
func (ism *InMemoryStateManager) GetStatistics() map[string]any {
	ism.mutex.RLock()
	defer ism.mutex.RUnlock()

	stats := make(map[string]any)
	stats["total_subscriptions"] = len(ism.subscriptions)
	stats["nef_index_entries"] = len(ism.nefIndex)

	stateCounts := make(map[SubscriptionState]int)
	for _, entry := range ism.subscriptions {
		stateCounts[entry.State]++
	}
	stats["by_state"] = stateCounts

	return stats
}

// copyEntry returns a deep copy of an entry via JSON round-trip to avoid
// callers mutating shared internal state.
func (ism *InMemoryStateManager) copyEntry(entry *SubscriptionEntry) *SubscriptionEntry {
	if entry == nil {
		return nil
	}

	data, _ := json.Marshal(entry)
	var copy SubscriptionEntry
	json.Unmarshal(data, &copy)

	return &copy
}
