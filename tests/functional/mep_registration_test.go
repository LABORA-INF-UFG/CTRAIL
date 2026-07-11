package functional

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/LABORA-INF-UFG/ctrail/internal/k8s"
	"github.com/LABORA-INF-UFG/ctrail/internal/mep"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules"
	"github.com/gin-gonic/gin"
	"k8s.io/client-go/kubernetes/fake"
)

// fakeMEP records registrations and deregistrations in memory, standing in for
// the OAI MEP registry so the mp1 wiring can be tested without a live MEP.
type fakeMEP struct {
	mu         sync.Mutex
	registered []mep.Service
	deleted    []string
}

func (f *fakeMEP) Register(_ context.Context, svc mep.Service) (*mep.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	svc.Sid = "mep-sid-1"
	f.registered = append(f.registered, svc)
	return &svc, nil
}

func (f *fakeMEP) Deregister(_ context.Context, serviceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, serviceID)
	return nil
}

// setupEAMWithMEP builds a gateway with EAM enabled against a fake clientset and
// the given MEP client.
func setupEAMWithMEP(t *testing.T, mepClient mep.Client) *EAMTestContext {
	t.Helper()
	gin.SetMode(gin.TestMode)
	clientset := fake.NewSimpleClientset()
	gateway := modules.NewGatewayOrchestrator("http://mock-nef.local", "test-token")
	gateway.EnableEAM(k8s.NewClientFromInterface(clientset), "ctrail-", mepClient)
	router := gin.New()
	gateway.SetupRoutes(router)
	return &EAMTestContext{Gateway: gateway, Router: router, Fake: clientset}
}

func TestDeployment_RegistersExternalServiceWithMEP(t *testing.T) {
	f := &fakeMEP{}
	tc := setupEAMWithMEP(t, f)

	depID, _ := tc.createDeployment(t, []string{"camara-mec-zone-1"})

	f.mu.Lock()
	regCount := len(f.registered)
	f.mu.Unlock()
	if regCount != 1 {
		t.Fatalf("expected one MEP registration for the external service, got %d", regCount)
	}

	// Terminating the deployment must deregister the service.
	if w := tc.do(http.MethodDelete, "/deployments/"+depID, nil); w.Code != http.StatusAccepted {
		t.Fatalf("DELETE expected 202, got %d", w.Code)
	}
	f.mu.Lock()
	delCount := len(f.deleted)
	f.mu.Unlock()
	if delCount != 1 || f.deleted[0] != "mep-sid-1" {
		t.Fatalf("expected deregistration of mep-sid-1, got %v", f.deleted)
	}
}

func TestDeployment_NoMEPClientSkipsRegistration(t *testing.T) {
	// The default setup passes a nil MEP client; deployments must still succeed.
	tc := SetupEAMTest(t)
	if _, _ = tc.createDeployment(t, []string{"camara-mec-zone-1"}); tc == nil {
		t.Fatal("deployment without MEP should still work")
	}
}
