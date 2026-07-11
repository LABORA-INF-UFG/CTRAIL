// Package mep is CTRAIL's client for the OAI MEP Mp1 service registry (ETSI MEC
// 011 service registration/discovery, as implemented by EURECOM's OAI-MEP). It
// is an optional value-add for the Edge Application Management instance: once a
// deployment exposes an externally-visible service, CTRAIL can register it here
// so other MEC applications can discover it via the mp1 interface.
//
// Divergence note: OAI MEP exposes a simplified, non-standard surface
// (/register, /discover) rather than the canonical MEC 011
// /applications/{appInstanceId}/services path, and its Service.type is a fixed
// enum of MEC service categories. CTRAIL's internal model should be the MEC 011
// ServiceInfo; this client is the down-converting adapter to OAI MEP. See
// docs/reference/MEC-COMPLIANCE.md §4.
package mep

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Endpoint is one consumable endpoint of a registered MEC service.
type Endpoint struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Method      string `json:"method,omitempty"`
	Description string `json:"description,omitempty"`
}

// Service is the OAI MEP registry object (mp1). Type must be one of the MEC
// service categories accepted by OAI MEP (e.g. "Traffic").
type Service struct {
	Sid         string     `json:"sid,omitempty"`
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Host        string     `json:"host"`
	Port        int        `json:"port"`
	Path        string     `json:"path"`
	Endpoints   []Endpoint `json:"endpoints"`
	Description string     `json:"description,omitempty"`
}

// Client registers and deregisters services in the OAI MEP registry.
type Client interface {
	Register(ctx context.Context, svc Service) (*Service, error)
	Deregister(ctx context.Context, serviceID string) error
}

// httpClient talks to the OAI MEP gateway over HTTP.
type httpClient struct {
	baseURL string
	http    *http.Client
}

// NewClient creates an OAI MEP client targeting the given base URL, e.g.
// http://oai-mep.org/service_registry/v1.
func NewClient(baseURL string) Client {
	return &httpClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Register POSTs a service to /register. OAI MEP returns the created service(s);
// the first element (with its assigned sid) is returned.
func (c *httpClient) Register(ctx context.Context, svc Service) (*Service, error) {
	body, err := json.Marshal(svc)
	if err != nil {
		return nil, fmt.Errorf("marshal service: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/register", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build register request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MEP register returned %s", resp.Status)
	}

	// OAI MEP returns an array of the registered services.
	var services []Service
	if err := json.NewDecoder(resp.Body).Decode(&services); err != nil {
		return nil, fmt.Errorf("decode register response: %w", err)
	}
	if len(services) == 0 {
		return &svc, nil
	}
	return &services[0], nil
}

// Deregister removes a service by its registry id via DELETE /register/{id}.
func (c *httpClient) Deregister(ctx context.Context, serviceID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/register/"+serviceID, nil)
	if err != nil {
		return fmt.Errorf("build deregister request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("deregister request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("MEP deregister returned %s", resp.Status)
	}
	return nil
}
