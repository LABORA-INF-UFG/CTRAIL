package nef

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{},
		token:      token,
	}
}

// CreateSubscription sends TrafficInfluSub to NEF
func (c *Client) CreateSubscription(ctx context.Context, sub TrafficInfluSub) (*TrafficInfluSub, error) {
	url := fmt.Sprintf("%s/3gpp-traffic-influence/v1/%s/subscriptions", c.baseURL, sub.AfAppId)

	body, err := json.Marshal(sub)
	if err != nil {
		return nil, fmt.Errorf("marshal failed: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("request creation failed: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	//print("\n-- REQ to NEF --\n")
	//print(req)
	//print("\n")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("nef error: %s", resp.Status)
	}

	var result TrafficInfluSub
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode failed: %v", err)
	}

	return &result, nil
}
