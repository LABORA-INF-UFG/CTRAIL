package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

const maxResponseBytes = 4 << 20

// APIResponse preserves the gateway's status and body, including asynchronous
// 202 responses, empty 204 responses, and CAMARA error documents.
type APIResponse struct {
	Status     int    `json:"status" jsonschema:"HTTP status returned by CTRAIL; 202 means accepted, not ready"`
	Body       any    `json:"body" jsonschema:"CTRAIL JSON response, text for non-JSON responses, or null for an empty response"`
	Location   string `json:"location,omitempty" jsonschema:"Location header returned by CTRAIL"`
	RetryAfter string `json:"retryAfter,omitempty" jsonschema:"Retry-After header returned by CTRAIL"`
	Correlator string `json:"correlator,omitempty" jsonschema:"x-correlator header returned by CTRAIL"`
}

// Client calls a single operator-configured gateway. Tool arguments never choose
// a host, an arbitrary route, credentials, or request headers.
type Client struct {
	baseURL *url.URL
	http    *http.Client
	cfg     Config
}

func NewClient(cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	u, _ := url.Parse(cfg.BaseURL)
	return &Client{
		baseURL: u,
		cfg:     cfg,
		http: &http.Client{
			Timeout: cfg.Timeout,
			// Redirects could disclose credentials or change a mutating method.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

type apiRequest struct {
	path  string
	query url.Values
	body  any
}

func (c *Client) do(ctx context.Context, method string, input apiRequest) (*APIResponse, error) {
	var body io.Reader
	if input.body != nil {
		data, err := json.Marshal(input.body)
		if err != nil {
			return nil, fmt.Errorf("cannot encode the CTRAIL request")
		}
		body = bytes.NewReader(data)
	}
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + input.path
	u.RawPath = ""
	u.RawQuery = input.query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("cannot construct the CTRAIL request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ctrail-mcp/"+Version)
	if input.body != nil {
		// The gateway currently validates application/json for Traffic Influence;
		// its deployment PATCH handler also accepts JSON Merge Patch with this type.
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)
	} else if c.cfg.Username != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// Do not expose transport errors containing a configured URL or credentials.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("CTRAIL request cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("CTRAIL request failed; check gateway connectivity and CTRAIL_MCP_TIMEOUT; a write may already have been applied")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read the CTRAIL response; a write may already have been applied")
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("CTRAIL response exceeds the 4 MiB limit")
	}
	result := &APIResponse{
		Status: resp.StatusCode, Location: resp.Header.Get("Location"),
		RetryAfter: resp.Header.Get("Retry-After"), Correlator: resp.Header.Get("x-correlator"),
	}
	if len(bytes.TrimSpace(data)) != 0 {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&result.Body); err != nil || !json.Valid(data) {
			result.Body = string(data)
		}
	}
	return result, nil
}

func resourcePath(collection, id string) (string, error) {
	if id == "" || strings.TrimSpace(id) != id || id == "." || id == ".." ||
		strings.ContainsAny(id, "/\\%?#") || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("resource ID must be a nonempty single path segment without URL delimiters")
	}
	return collection + "/" + id, nil
}
