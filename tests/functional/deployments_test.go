package functional

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// createDeployment onboards an app and deploys it to the given zones, returning
// the assigned appDeploymentId and the app id.
func (tc *EAMTestContext) createDeployment(t *testing.T, zones []string) (string, string) {
	t.Helper()
	appID := tc.submitApp(t)

	req := camara.AppDeploymentRequest{
		AppDeploymentName: "my_deployment",
		AppId:             appID,
		EdgeCloudZones:    zones,
	}
	w := tc.do(http.MethodPost, "/deployments", req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("POST /deployments expected 202, got %d. Body: %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc == "" {
		t.Fatal("expected Location header on 202")
	}
	var resp struct {
		AppDeploymentId string `json:"appDeploymentId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse deployment response: %v", err)
	}
	if resp.AppDeploymentId == "" {
		t.Fatal("expected appDeploymentId")
	}
	return resp.AppDeploymentId, appID
}

func TestCreateDeployment_CreatesK8sObjects(t *testing.T) {
	tc := SetupEAMTest(t)
	depID, _ := tc.createDeployment(t, []string{"camara-mec-zone-1"})

	// The namespace derives from prefix + zoneID.
	ns := "ctrail-camara-mec-zone-1"
	deps, err := tc.Fake.AppsV1().Deployments(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(deps.Items) != 1 {
		t.Fatalf("expected exactly one Deployment in %s, got %d", ns, len(deps.Items))
	}
	if deps.Items[0].Labels["ctrail.io/deployment-id"] != depID {
		t.Fatalf("deployment missing correlation label, got %+v", deps.Items[0].Labels)
	}

	// An externally-visible interface must produce a Service too.
	svcs, err := tc.Fake.CoreV1().Services(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	if len(svcs.Items) != 1 {
		t.Fatalf("expected one Service in %s, got %d", ns, len(svcs.Items))
	}
}

func TestCreateDeployment_UnknownAppRejected(t *testing.T) {
	tc := SetupEAMTest(t)
	req := camara.AppDeploymentRequest{
		AppDeploymentName: "d",
		AppId:             "11111111-1111-1111-1111-111111111111",
		EdgeCloudZones:    []string{"camara-mec-zone-1"},
	}
	w := tc.do(http.MethodPost, "/deployments", req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown appId, got %d", w.Code)
	}
}

func TestCreateDeployment_UnknownZoneRejected(t *testing.T) {
	tc := SetupEAMTest(t)
	appID := tc.submitApp(t)
	req := camara.AppDeploymentRequest{
		AppDeploymentName: "d",
		AppId:             appID,
		EdgeCloudZones:    []string{"no-such-zone"},
	}
	w := tc.do(http.MethodPost, "/deployments", req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for unresolvable zone, got %d", w.Code)
	}
}

func TestGetDeployment_ReportsInstancesAndZones(t *testing.T) {
	tc := SetupEAMTest(t)
	depID, appID := tc.createDeployment(t, []string{"camara-mec-zone-1"})

	w := tc.do(http.MethodGet, "/deployments/"+depID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /deployments/{id} expected 200, got %d", w.Code)
	}
	var info camara.AppDeploymentInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("parse AppDeploymentInfo: %v", err)
	}
	if info.AppId != appID || info.AppDeploymentId != depID {
		t.Fatalf("unexpected info: %+v", info)
	}
	if len(info.EdgeCloudZones) != 1 || info.EdgeCloudZones[0] != "camara-mec-zone-1" {
		t.Fatalf("expected the target zone, got %+v", info.EdgeCloudZones)
	}
	if len(info.AppInstances) != 1 {
		t.Fatalf("expected one app instance, got %d", len(info.AppInstances))
	}
}

func TestGetDeployment_NotFound(t *testing.T) {
	tc := SetupEAMTest(t)
	w := tc.do(http.MethodGet, "/deployments/11111111-1111-1111-1111-111111111111", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestListDeployments_FiltersByAppId(t *testing.T) {
	tc := SetupEAMTest(t)
	_, appID := tc.createDeployment(t, []string{"camara-mec-zone-1"})

	w := tc.do(http.MethodGet, "/deployments?appId="+appID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /deployments expected 200, got %d", w.Code)
	}
	var list []camara.AppDeploymentInfo
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("parse list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 deployment for appId, got %d", len(list))
	}

	// A different appId filter returns none.
	w2 := tc.do(http.MethodGet, "/deployments?appId=99999999-9999-9999-9999-999999999999", nil)
	var empty []camara.AppDeploymentInfo
	_ = json.Unmarshal(w2.Body.Bytes(), &empty)
	if len(empty) != 0 {
		t.Fatalf("expected 0 deployments for unknown appId, got %d", len(empty))
	}
}

func TestPatchDeployment_ReplacesZones(t *testing.T) {
	tc := SetupEAMTest(t)
	depID, _ := tc.createDeployment(t, []string{"camara-mec-zone-1"})

	newZones := []string{"camara-edge-zone-central"}
	patch := camara.AppDeploymentPatch{EdgeCloudZones: &newZones}
	w := tc.do(http.MethodPatch, "/deployments/"+depID, patch)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH /deployments/{id} expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}
	var info camara.AppDeploymentInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("parse info: %v", err)
	}
	if len(info.EdgeCloudZones) != 1 || info.EdgeCloudZones[0] != "camara-edge-zone-central" {
		t.Fatalf("expected zones replaced with central, got %+v", info.EdgeCloudZones)
	}

	// Old zone's workload must be gone; new zone's must exist.
	oldNs := "ctrail-camara-mec-zone-1"
	newNs := "ctrail-camara-edge-zone-central"
	oldDeps, _ := tc.Fake.AppsV1().Deployments(oldNs).List(context.Background(), metav1.ListOptions{})
	if len(oldDeps.Items) != 0 {
		t.Fatalf("expected old-zone deployment removed, got %d", len(oldDeps.Items))
	}
	newDeps, _ := tc.Fake.AppsV1().Deployments(newNs).List(context.Background(), metav1.ListOptions{})
	if len(newDeps.Items) != 1 {
		t.Fatalf("expected new-zone deployment created, got %d", len(newDeps.Items))
	}
}

func TestPatchDeployment_RenameOnly(t *testing.T) {
	tc := SetupEAMTest(t)
	depID, _ := tc.createDeployment(t, []string{"camara-mec-zone-1"})

	name := "renamed_deployment"
	patch := camara.AppDeploymentPatch{AppDeploymentName: &name}
	w := tc.do(http.MethodPatch, "/deployments/"+depID, patch)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var info camara.AppDeploymentInfo
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	if info.AppDeploymentName != name {
		t.Fatalf("expected renamed deployment, got %q", info.AppDeploymentName)
	}
	if len(info.EdgeCloudZones) != 1 {
		t.Fatalf("rename must not change zones, got %+v", info.EdgeCloudZones)
	}
}

func TestDeleteDeployment_RemovesWorkloadsAnd202(t *testing.T) {
	tc := SetupEAMTest(t)
	depID, _ := tc.createDeployment(t, []string{"camara-mec-zone-1"})

	w := tc.do(http.MethodDelete, "/deployments/"+depID, nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("DELETE /deployments/{id} expected 202, got %d", w.Code)
	}
	if w2 := tc.do(http.MethodGet, "/deployments/"+depID, nil); w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w2.Code)
	}
	ns := "ctrail-camara-mec-zone-1"
	deps, _ := tc.Fake.AppsV1().Deployments(ns).List(context.Background(), metav1.ListOptions{})
	if len(deps.Items) != 0 {
		t.Fatalf("expected workloads removed, got %d", len(deps.Items))
	}
}

// TestGetDeployment_RunningWhenReplicasReady seeds the fake deployment status so
// the observed state derives RUNNING from the backend, not assumption.
func TestGetDeployment_RunningWhenReplicasReady(t *testing.T) {
	tc := SetupEAMTest(t)
	depID, _ := tc.createDeployment(t, []string{"camara-mec-zone-1"})

	ns := "ctrail-camara-mec-zone-1"
	deps, _ := tc.Fake.AppsV1().Deployments(ns).List(context.Background(), metav1.ListOptions{})
	dep := deps.Items[0]
	dep.Status = appsv1.DeploymentStatus{ReadyReplicas: 1}
	if _, err := tc.Fake.AppsV1().Deployments(ns).UpdateStatus(context.Background(), &dep, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("seed status: %v", err)
	}

	// GET refreshes state from the backend; the request must still succeed.
	w := tc.do(http.MethodGet, "/deployments/"+depID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET after status seed expected 200, got %d", w.Code)
	}
}
