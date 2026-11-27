package mcpserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBasicAuthenticationAndNonJSON(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "operator" || password != "test-password" {
			t.Error("Basic credentials were not forwarded")
		}
		w.WriteHeader(404)
		_, _ = io.WriteString(w, "404 page not found")
	}))
	defer gateway.Close()
	cfg := testConfig(gateway.URL)
	cfg.BearerToken, cfg.Username, cfg.Password = "", "operator", "test-password"
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.do(context.Background(), "GET", apiRequest{path: "/apps"})
	if err != nil || response.Status != 404 || response.Body != "404 page not found" {
		t.Fatalf("response=%+v error=%v", response, err)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer target.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer gateway.Close()
	client, _ := NewClient(testConfig(gateway.URL))
	response, err := client.do(context.Background(), "POST", apiRequest{path: "/apps", body: map[string]string{"name": "demo"}})
	if err != nil || response.Status != 307 || calls.Load() != 0 {
		t.Fatalf("redirect followed: response=%+v calls=%d error=%v", response, calls.Load(), err)
	}
}

func TestTimeoutCancellationAndResponseLimit(t *testing.T) {
	started := make(chan struct{})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/large" {
			_, _ = io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
			return
		}
		if r.URL.Path == "/cancel" {
			close(started)
		}
		<-r.Context().Done()
	}))
	defer gateway.Close()
	cfg := testConfig(gateway.URL)
	cfg.Timeout = 50 * time.Millisecond
	client, _ := NewClient(cfg)
	if _, err := client.do(context.Background(), "GET", apiRequest{path: "/timeout"}); err == nil {
		t.Fatal("timeout was not enforced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	if _, err := client.do(ctx, "GET", apiRequest{path: "/cancel"}); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancellation not propagated: %v", err)
	}
	cfg.Timeout = 2 * time.Second
	client, _ = NewClient(cfg)
	if _, err := client.do(context.Background(), "GET", apiRequest{path: "/large"}); err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("response size limit not enforced: %v", err)
	}
}
