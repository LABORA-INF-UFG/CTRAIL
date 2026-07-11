package mep

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockMEP is an in-process stand-in for the OAI MEP registry, mirroring its
// /register and /register/{id} surface.
func mockMEP(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var deleted []string
	mux := http.NewServeMux()
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var svc Service
		_ = json.NewDecoder(r.Body).Decode(&svc)
		svc.Sid = "sid-123"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode([]Service{svc})
	})
	mux.HandleFunc("/register/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		deleted = append(deleted, r.URL.Path[len("/register/"):])
		w.WriteHeader(http.StatusOK)
	})
	return httptest.NewServer(mux), &deleted
}

func TestRegisterReturnsAssignedSid(t *testing.T) {
	srv, _ := mockMEP(t)
	defer srv.Close()

	c := NewClient(srv.URL)
	got, err := c.Register(context.Background(), Service{Name: "web", Type: "Traffic", Host: "web.ns", Port: 80, Path: "/"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got.Sid != "sid-123" {
		t.Fatalf("expected assigned sid, got %q", got.Sid)
	}
}

func TestDeregisterHitsRegistryPath(t *testing.T) {
	srv, deleted := mockMEP(t)
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.Deregister(context.Background(), "sid-123"); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	if len(*deleted) != 1 || (*deleted)[0] != "sid-123" {
		t.Fatalf("expected deregister of sid-123, got %v", *deleted)
	}
}

func TestRegisterErrorsOnServerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if _, err := c.Register(context.Background(), Service{Name: "web", Type: "Traffic"}); err == nil {
		t.Fatal("expected error on 500 from MEP")
	}
}
