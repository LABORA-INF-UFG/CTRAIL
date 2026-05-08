package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/LABORA-INF-UFG/ctrail/internal/nef"
)

// RequestBuilder defines the methods for constructing and executing NEF requests.
type RequestBuilder interface {
	BuildCreateRequest(subscription *nef.TrafficInfluSub) (*http.Request, error)
	BuildUpdateRequest(subscriptionId string, subscription *nef.TrafficInfluSub) (*http.Request, error)
	BuildDeleteRequest(subscriptionId string, afId string) (*http.Request, error)
	BuildGetRequest(subscriptionId string, afId string) (*http.Request, error)
	ExecuteRequest(ctx context.Context, req *http.Request) (*http.Response, error)
}

// NEFRequestBuilder implements RequestBuilder for the 3GPP NEF Traffic Influence API.
type NEFRequestBuilder struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
	headers    map[string]string
}

// NewNEFRequestBuilder creates a request builder targeting the given NEF base URL
// and authenticating with apiToken.
func NewNEFRequestBuilder(baseURL, apiToken string) *NEFRequestBuilder {
	return &NEFRequestBuilder{
		baseURL:  baseURL,
		apiToken: apiToken,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		headers: map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json",
		},
	}
}

// BuildCreateRequest builds a POST request to create a NEF subscription.
func (nrb *NEFRequestBuilder) BuildCreateRequest(subscription *nef.TrafficInfluSub) (*http.Request, error) {
	log.Printf("Building CREATE request for NEF subscription")

	if err := nrb.validateSubscription(subscription); err != nil {
		return nil, fmt.Errorf("subscription validation failed: %w", err)
	}

	body, err := nrb.serializeSubscription(subscription)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize subscription: %w", err)
	}

	afId := subscription.AfServiceId
	if afId == "" {
		afId = "default-af"
	}
	url := fmt.Sprintf("%s/3gpp-traffic-influence/v1/%s/subscriptions", nrb.baseURL, afId)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	nrb.addHeaders(req)

	log.Printf("CREATE request built successfully for URL: %s", url)
	return req, nil
}

// BuildUpdateRequest builds a PUT request to replace a NEF subscription.
func (nrb *NEFRequestBuilder) BuildUpdateRequest(subscriptionId string, subscription *nef.TrafficInfluSub) (*http.Request, error) {
	log.Printf("Building UPDATE request for subscription: %s", subscriptionId)

	if subscriptionId == "" {
		return nil, fmt.Errorf("subscription ID is required for update")
	}

	if err := nrb.validateSubscription(subscription); err != nil {
		return nil, fmt.Errorf("subscription validation failed: %w", err)
	}

	body, err := nrb.serializeSubscription(subscription)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize subscription: %w", err)
	}

	afId := subscription.AfServiceId
	if afId == "" {
		afId = "default-af"
	}
	url := fmt.Sprintf("%s/3gpp-traffic-influence/v1/%s/subscriptions/%s", nrb.baseURL, afId, subscriptionId)

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	nrb.addHeaders(req)

	log.Printf("UPDATE request built successfully for URL: %s", url)
	return req, nil
}

// BuildDeleteRequest builds a DELETE request to remove a NEF subscription.
func (nrb *NEFRequestBuilder) BuildDeleteRequest(subscriptionId string, afId string) (*http.Request, error) {
	log.Printf("Building DELETE request for subscription: %s (afId: %s)", subscriptionId, afId)

	if subscriptionId == "" {
		return nil, fmt.Errorf("subscription ID is required for delete")
	}

	if afId == "" {
		afId = "default-af"
	}

	url := fmt.Sprintf("%s/3gpp-traffic-influence/v1/%s/subscriptions/%s", nrb.baseURL, afId, subscriptionId)

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if nrb.apiToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", nrb.apiToken))
	}

	log.Printf("DELETE request built successfully for URL: %s", url)
	return req, nil
}

// BuildGetRequest builds a GET request to retrieve a NEF subscription.
func (nrb *NEFRequestBuilder) BuildGetRequest(subscriptionId string, afId string) (*http.Request, error) {
	log.Printf("Building GET request for subscription: %s (afId: %s)", subscriptionId, afId)

	if subscriptionId == "" {
		return nil, fmt.Errorf("subscription ID is required for get")
	}

	if afId == "" {
		afId = "default-af"
	}

	url := fmt.Sprintf("%s/3gpp-traffic-influence/v1/%s/subscriptions/%s", nrb.baseURL, afId, subscriptionId)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if nrb.apiToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", nrb.apiToken))
	}

	log.Printf("GET request built successfully for URL: %s", url)
	return req, nil
}

// ExecuteRequest executes a built HTTP request and returns the response.
func (nrb *NEFRequestBuilder) ExecuteRequest(ctx context.Context, req *http.Request) (*http.Response, error) {
	log.Printf("Executing %s request to: %s", req.Method, req.URL.String())

	req = req.WithContext(ctx)

	if req.Method == http.MethodPost || req.Method == http.MethodPut {
		if req.Body != nil {
			bodyBytes := make([]byte, 0)
			if req.GetBody != nil {
				if bodyReader, err := req.GetBody(); err == nil {
					buf := new(bytes.Buffer)
					buf.ReadFrom(bodyReader)
					bodyBytes = buf.Bytes()
				}
			}

			log.Printf("Request body: %s", string(bodyBytes))
		}
	}

	resp, err := nrb.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	log.Printf("Request completed with status: %d %s", resp.StatusCode, resp.Status)
	return resp, nil
}

func (nrb *NEFRequestBuilder) validateSubscription(subscription *nef.TrafficInfluSub) error {
	if subscription == nil {
		return fmt.Errorf("subscription cannot be nil")
	}

	if len(subscription.TrafficFilters) == 0 && len(subscription.EthTrafficFilters) == 0 {
		return fmt.Errorf("at least one traffic filter is required")
	}

	if subscription.Snssai != nil {
		if subscription.Snssai.Sst < 1 || subscription.Snssai.Sst > 255 {
			return fmt.Errorf("invalid SNSSAI SST value: %d", subscription.Snssai.Sst)
		}
	}

	for i, flowInfo := range subscription.TrafficFilters {
		if len(flowInfo.FlowDescriptions) == 0 {
			return fmt.Errorf("traffic filter %d has no flow descriptions", i)
		}
	}

	return nil
}

// serializeSubscription marshals a subscription for the NEF wire format,
// stripping the subscriptionId so it is not sent in create/update requests.
func (nrb *NEFRequestBuilder) serializeSubscription(subscription *nef.TrafficInfluSub) ([]byte, error) {
	requestSub := *subscription
	requestSub.SubscriptionId = ""

	body, err := json.MarshalIndent(&requestSub, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("JSON marshaling failed: %w", err)
	}

	return body, nil
}

func (nrb *NEFRequestBuilder) addHeaders(req *http.Request) {
	for key, value := range nrb.headers {
		req.Header.Set(key, value)
	}

	if nrb.apiToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", nrb.apiToken))
	}

	req.Header.Set("3gpp-Sbi-Target-apiRoot", nrb.baseURL)
	req.Header.Set("X-Request-Timestamp", time.Now().UTC().Format(time.RFC3339))
}

// SetCustomHeader sets a custom header on all outgoing requests.
func (nrb *NEFRequestBuilder) SetCustomHeader(key, value string) {
	if nrb.headers == nil {
		nrb.headers = make(map[string]string)
	}
	nrb.headers[key] = value
}

// RemoveCustomHeader removes a previously set custom header.
func (nrb *NEFRequestBuilder) RemoveCustomHeader(key string) {
	delete(nrb.headers, key)
}

// SetTimeout sets the HTTP client timeout.
func (nrb *NEFRequestBuilder) SetTimeout(timeout time.Duration) {
	nrb.httpClient.Timeout = timeout
}

// GetBaseURL returns the configured NEF base URL.
func (nrb *NEFRequestBuilder) GetBaseURL() string {
	return nrb.baseURL
}

// UpdateCredentials replaces the bearer token used for NEF authentication.
func (nrb *NEFRequestBuilder) UpdateCredentials(apiToken string) {
	nrb.apiToken = apiToken
}
