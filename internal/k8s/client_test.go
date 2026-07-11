package k8s

import (
	"context"
	"encoding/json"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// newTestClient returns a Client backed by an in-memory fake clientset — the
// client-go analogue of the mock NEF used by the Traffic Influence tests.
func newTestClient() Client {
	return NewClientFromInterface(fake.NewSimpleClientset())
}

func deployment(name string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: name, Image: "nginx:latest"}},
				},
			},
		},
	}
}

func TestEnsureNamespaceIsIdempotent(t *testing.T) {
	c := newTestClient()
	ctx := context.Background()

	if err := c.EnsureNamespace(ctx, "ctrail-zone1", map[string]string{"ctrail.io/edge-zone": "zone1"}); err != nil {
		t.Fatalf("first EnsureNamespace: %v", err)
	}
	// A second call must not error (AlreadyExists is swallowed).
	if err := c.EnsureNamespace(ctx, "ctrail-zone1", nil); err != nil {
		t.Fatalf("second EnsureNamespace should be idempotent, got: %v", err)
	}

	nss, err := c.ListNamespaces(ctx, "ctrail.io/edge-zone=zone1")
	if err != nil {
		t.Fatalf("ListNamespaces: %v", err)
	}
	if len(nss) != 1 || nss[0].Name != "ctrail-zone1" {
		t.Fatalf("expected exactly the labelled namespace, got %+v", nss)
	}
}

func TestApplyDeploymentCreatesThenReplaces(t *testing.T) {
	c := newTestClient()
	ctx := context.Background()

	if _, err := c.ApplyDeployment(ctx, "ctrail-zone1", deployment("web")); err != nil {
		t.Fatalf("create ApplyDeployment: %v", err)
	}

	// Applying again with a changed image must replace, not fail on AlreadyExists.
	updated := deployment("web")
	updated.Spec.Template.Spec.Containers[0].Image = "nginx:1.27"
	got, err := c.ApplyDeployment(ctx, "ctrail-zone1", updated)
	if err != nil {
		t.Fatalf("replace ApplyDeployment: %v", err)
	}
	if img := got.Spec.Template.Spec.Containers[0].Image; img != "nginx:1.27" {
		t.Fatalf("expected replaced image nginx:1.27, got %q", img)
	}
}

func TestGetDeploymentNotFound(t *testing.T) {
	c := newTestClient()
	_, err := c.GetDeployment(context.Background(), "ctrail-zone1", "missing")
	if err == nil || !IsNotFound(err) {
		t.Fatalf("expected IsNotFound error, got: %v", err)
	}
}

func TestPatchDeploymentMergePatch(t *testing.T) {
	c := newTestClient()
	ctx := context.Background()
	if _, err := c.ApplyDeployment(ctx, "ctrail-zone1", deployment("web")); err != nil {
		t.Fatalf("seed ApplyDeployment: %v", err)
	}

	patch, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{"labels": map[string]string{"tier": "edge"}},
	})
	got, err := c.PatchDeployment(ctx, "ctrail-zone1", "web", patch)
	if err != nil {
		t.Fatalf("PatchDeployment: %v", err)
	}
	if got.Labels["tier"] != "edge" {
		t.Fatalf("expected merged label tier=edge, got %+v", got.Labels)
	}
}

func TestDeleteDeploymentNotFoundIsNoError(t *testing.T) {
	c := newTestClient()
	if err := c.DeleteDeployment(context.Background(), "ctrail-zone1", "missing"); err != nil {
		t.Fatalf("deleting a missing deployment should be a no-op, got: %v", err)
	}
}

func TestApplyServiceCreatesThenReplaces(t *testing.T) {
	c := newTestClient()
	ctx := context.Background()
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web"},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "web"},
			Ports:    []corev1.ServicePort{{Port: 80}},
		},
	}
	if _, err := c.ApplyService(ctx, "ctrail-zone1", svc); err != nil {
		t.Fatalf("create ApplyService: %v", err)
	}
	svc2 := svc.DeepCopy()
	svc2.Spec.Ports = []corev1.ServicePort{{Port: 8080}}
	got, err := c.ApplyService(ctx, "ctrail-zone1", svc2)
	if err != nil {
		t.Fatalf("replace ApplyService: %v", err)
	}
	if got.Spec.Ports[0].Port != 8080 {
		t.Fatalf("expected replaced port 8080, got %d", got.Spec.Ports[0].Port)
	}
}
