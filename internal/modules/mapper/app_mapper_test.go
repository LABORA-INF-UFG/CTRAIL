package mapper

import (
	"testing"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/topology"
	corev1 "k8s.io/api/core/v1"
)

func sampleManifest() *camara.AppManifest {
	return &camara.AppManifest{
		AppId:       "11111111-1111-1111-1111-111111111111",
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

func newMapper() *KubernetesAppMapper {
	return NewKubernetesAppMapper(topology.NewTopologyEngine())
}

func TestMapToDeployment(t *testing.T) {
	m := newMapper()
	depID := "22222222-2222-2222-2222-222222222222"

	dep, err := m.MapToDeployment(sampleManifest(), depID, "zone-a")
	if err != nil {
		t.Fatalf("MapToDeployment: %v", err)
	}

	if dep.Labels[LabelDeploymentId] != depID || dep.Labels[LabelZoneId] != "zone-a" {
		t.Fatalf("expected correlation labels, got %+v", dep.Labels)
	}

	c := dep.Spec.Template.Spec.Containers[0]
	if c.Image != "docker.io/library/nginx:1.27" {
		t.Fatalf("expected image mapped from appRepo, got %q", c.Image)
	}
	if len(c.Ports) != 1 || c.Ports[0].ContainerPort != 8080 || c.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Fatalf("expected container port 8080/TCP, got %+v", c.Ports)
	}

	cpu := c.Resources.Requests[corev1.ResourceCPU]
	if cpu.String() != "500m" {
		t.Fatalf("expected CPU request 500m, got %s", cpu.String())
	}
	mem := c.Resources.Requests[corev1.ResourceMemory]
	if mem.String() != "512Mi" {
		t.Fatalf("expected memory request 512Mi, got %s", mem.String())
	}
}

func TestMapToServiceExternalIsLoadBalancer(t *testing.T) {
	m := newMapper()
	svc, err := m.MapToService(sampleManifest(), "22222222-2222-2222-2222-222222222222", "zone-a")
	if err != nil {
		t.Fatalf("MapToService: %v", err)
	}
	if svc == nil {
		t.Fatal("expected a Service for an externally-visible interface")
	}
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Fatalf("external visibility should map to LoadBalancer, got %s", svc.Spec.Type)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 8080 {
		t.Fatalf("expected service port 8080, got %+v", svc.Spec.Ports)
	}
}

func TestMapToServiceInternalOnlyIsClusterIP(t *testing.T) {
	m := newMapper()
	manifest := sampleManifest()
	manifest.ComponentSpec[0].NetworkInterfaces[0].VisibilityType = camara.VisibilityInternal

	svc, err := m.MapToService(manifest, "dep", "zone-a")
	if err != nil {
		t.Fatalf("MapToService: %v", err)
	}
	if svc == nil || svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Fatalf("internal-only visibility should map to ClusterIP, got %+v", svc)
	}
}

func TestMapToServiceNoPortsReturnsNil(t *testing.T) {
	m := newMapper()
	manifest := sampleManifest()
	manifest.ComponentSpec = nil

	svc, err := m.MapToService(manifest, "dep", "zone-a")
	if err != nil {
		t.Fatalf("MapToService: %v", err)
	}
	if svc != nil {
		t.Fatalf("expected nil Service when nothing is exposed, got %+v", svc)
	}
}

func TestMapToDeploymentRequiresImage(t *testing.T) {
	m := newMapper()
	manifest := sampleManifest()
	manifest.AppRepo.ImagePath = ""

	if _, err := m.MapToDeployment(manifest, "dep", "zone-a"); err == nil {
		t.Fatal("expected error when appRepo.imagePath is empty")
	}
}
