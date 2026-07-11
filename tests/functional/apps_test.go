package functional

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/LABORA-INF-UFG/ctrail/internal/k8s"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules"
	"github.com/gin-gonic/gin"
	"k8s.io/client-go/kubernetes/fake"
)

// EAMTestContext hosts the Edge Application Management instance backed by a fake
// Kubernetes clientset — the client-go analogue of the mock NEF used by the
// Traffic Influence tests.
type EAMTestContext struct {
	Gateway *modules.GatewayOrchestrator
	Router  *gin.Engine
	Fake    *fake.Clientset
}

// SetupEAMTest builds a gateway with EAM enabled against a fake clientset.
func SetupEAMTest(t *testing.T) *EAMTestContext {
	t.Helper()
	gin.SetMode(gin.TestMode)

	clientset := fake.NewSimpleClientset()
	gateway := modules.NewGatewayOrchestrator("http://mock-nef.local", "test-token")
	gateway.EnableEAM(k8s.NewClientFromInterface(clientset), "ctrail-", nil)

	router := gin.New()
	gateway.SetupRoutes(router)

	return &EAMTestContext{Gateway: gateway, Router: router, Fake: clientset}
}

func (tc *EAMTestContext) do(method, path string, body any) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "adminpassword123")
	w := httptest.NewRecorder()
	tc.Router.ServeHTTP(w, req)
	return w
}

func sampleAppManifest() camara.AppManifest {
	return camara.AppManifest{
		Name:        "WebApp",
		AppProvider: "acmeprovider",
		Version:     "1.0.0",
		PackageType: camara.PackageTypeContainer,
		AppRepo: camara.AppRepo{
			Type:      camara.AppRepoPublic,
			ImagePath: "docker.io/library/nginx:1.27",
		},
		RequiredResources: camara.RequiredResources{
			InfraKind: "container",
			NumCPU:    "500m",
			Memory:    512,
		},
		ComponentSpec: []camara.ComponentSpec{{
			ComponentName: "web",
			NetworkInterfaces: []camara.NetworkInterface{{
				InterfaceId:    "http",
				Protocol:       camara.ProtocolTCP,
				Port:           8080,
				VisibilityType: camara.VisibilityExternal,
			}},
		}},
	}
}

// submitApp onboards the sample manifest and returns the assigned appId.
func (tc *EAMTestContext) submitApp(t *testing.T) string {
	t.Helper()
	w := tc.do(http.MethodPost, "/apps", sampleAppManifest())
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /apps expected 201, got %d. Body: %s", w.Code, w.Body.String())
	}
	var submitted camara.SubmittedApp
	if err := json.Unmarshal(w.Body.Bytes(), &submitted); err != nil {
		t.Fatalf("parse SubmittedApp: %v", err)
	}
	if submitted.AppId == "" {
		t.Fatal("expected server-assigned appId")
	}
	return submitted.AppId
}

func TestSubmitApp_AssignsAppId(t *testing.T) {
	tc := SetupEAMTest(t)
	appID := tc.submitApp(t)
	if len(appID) != 36 {
		t.Fatalf("expected a UUID appId, got %q", appID)
	}
}

func TestSubmitApp_MissingImageRejected(t *testing.T) {
	tc := SetupEAMTest(t)
	manifest := sampleAppManifest()
	manifest.AppRepo.ImagePath = ""

	w := tc.do(http.MethodPost, "/apps", manifest)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing image, got %d", w.Code)
	}
}

func TestGetApp_ReturnsManifestEnvelope(t *testing.T) {
	tc := SetupEAMTest(t)
	appID := tc.submitApp(t)

	w := tc.do(http.MethodGet, "/apps/"+appID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /apps/{id} expected 200, got %d", w.Code)
	}
	var wrapper camara.AppManifestWrapper
	if err := json.Unmarshal(w.Body.Bytes(), &wrapper); err != nil {
		t.Fatalf("parse AppManifestWrapper: %v", err)
	}
	if wrapper.AppManifest.AppId != appID || wrapper.AppManifest.Name != "WebApp" {
		t.Fatalf("unexpected manifest: %+v", wrapper.AppManifest)
	}
}

func TestGetApp_NotFound(t *testing.T) {
	tc := SetupEAMTest(t)
	w := tc.do(http.MethodGet, "/apps/11111111-1111-1111-1111-111111111111", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetApps_ListsOnboarded(t *testing.T) {
	tc := SetupEAMTest(t)
	tc.submitApp(t)
	tc.submitApp(t)

	w := tc.do(http.MethodGet, "/apps", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /apps expected 200, got %d", w.Code)
	}
	var apps []camara.AppManifest
	if err := json.Unmarshal(w.Body.Bytes(), &apps); err != nil {
		t.Fatalf("parse apps: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}
}

func TestDeleteApp_Success(t *testing.T) {
	tc := SetupEAMTest(t)
	appID := tc.submitApp(t)

	w := tc.do(http.MethodDelete, "/apps/"+appID, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE /apps/{id} expected 204, got %d", w.Code)
	}
	// Confirm it is gone.
	if w2 := tc.do(http.MethodGet, "/apps/"+appID, nil); w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w2.Code)
	}
}

func TestDeleteApp_NotFound(t *testing.T) {
	tc := SetupEAMTest(t)
	w := tc.do(http.MethodDelete, "/apps/11111111-1111-1111-1111-111111111111", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
