package mapper

import (
	"fmt"
	"strings"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/topology"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Kubernetes object labels CTRAIL stamps on everything it creates, so the
// Deployments/Services it owns can be found and correlated back to CAMARA IDs.
const (
	LabelManagedBy    = "app.kubernetes.io/managed-by"
	LabelAppId        = "ctrail.io/app-id"
	LabelDeploymentId = "ctrail.io/deployment-id"
	LabelZoneId       = "ctrail.io/edge-zone"
	ManagedByValue    = "ctrail"
)

// AppMapper translates a CAMARA AppManifest into the Kubernetes objects that
// realise it on an edge cloud zone. It is the EAM-specific counterpart to the
// TrafficInfluenceMapper: the three-phase mapping (direct / logical+topology /
// structural) is preserved, but the target is Kubernetes rather than the NEF.
type AppMapper interface {
	// MapToDeployment builds the Deployment for one target zone.
	MapToDeployment(manifest *camara.AppManifest, deploymentID, zoneID string) (*appsv1.Deployment, error)
	// MapToService builds the Service exposing the manifest's external ports,
	// or nil when the manifest exposes no external interface.
	MapToService(manifest *camara.AppManifest, deploymentID, zoneID string) (*corev1.Service, error)
}

// KubernetesAppMapper is the default AppMapper. It holds the topology engine so
// it can resolve zone-specific placement (the logical mapping phase).
type KubernetesAppMapper struct {
	topologyEngine *topology.TopologyEngine
}

// NewKubernetesAppMapper creates an AppMapper backed by the given topology engine.
func NewKubernetesAppMapper(topologyEngine *topology.TopologyEngine) *KubernetesAppMapper {
	return &KubernetesAppMapper{topologyEngine: topologyEngine}
}

// MapToDeployment produces the appsv1.Deployment for a manifest in a zone.
//
//   - Direct: name/appId/image → Deployment metadata + container image.
//   - Logical: requiredResources (Vcpu/memory) → resource requests; zone → labels.
//   - Structural: componentSpec network interfaces → container ports.
func (m *KubernetesAppMapper) MapToDeployment(manifest *camara.AppManifest, deploymentID, zoneID string) (*appsv1.Deployment, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest cannot be nil")
	}
	if manifest.AppRepo.ImagePath == "" {
		return nil, fmt.Errorf("manifest appRepo.imagePath is required to build a container")
	}

	name := workloadName(manifest.Name, deploymentID)
	labels := map[string]string{
		LabelManagedBy:    ManagedByValue,
		LabelAppId:        manifest.AppId,
		LabelDeploymentId: deploymentID,
		LabelZoneId:       zoneID,
		"app":             name,
	}

	container := corev1.Container{
		Name:  containerName(manifest),
		Image: manifest.AppRepo.ImagePath,
		Ports: containerPorts(manifest),
	}
	if reqs, err := resourceRequirements(manifest.RequiredResources); err != nil {
		return nil, err
	} else if reqs != nil {
		container.Resources = *reqs
	}

	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
				},
			},
		},
	}

	// Structural: private-repo credentials → imagePullSecrets reference.
	if manifest.AppRepo.Type == camara.AppRepoPrivate && manifest.AppRepo.UserName != "" {
		deployment.Spec.Template.Spec.ImagePullSecrets = []corev1.LocalObjectReference{
			{Name: name + "-pull"},
		}
	}

	return deployment, nil
}

// MapToService builds a Service for the manifest's external interfaces. Returns
// (nil, nil) when nothing is exposed externally, so the caller skips creation.
func (m *KubernetesAppMapper) MapToService(manifest *camara.AppManifest, deploymentID, zoneID string) (*corev1.Service, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest cannot be nil")
	}

	name := workloadName(manifest.Name, deploymentID)
	var ports []corev1.ServicePort
	hasExternal := false

	for _, comp := range manifest.ComponentSpec {
		for _, ni := range comp.NetworkInterfaces {
			ports = append(ports, corev1.ServicePort{
				Name:     strings.ToLower(ni.InterfaceId),
				Port:     ni.Port,
				Protocol: k8sProtocol(ni.Protocol),
			})
			if ni.VisibilityType == camara.VisibilityExternal {
				hasExternal = true
			}
		}
	}

	if len(ports) == 0 {
		return nil, nil
	}

	svcType := corev1.ServiceTypeClusterIP
	if hasExternal {
		// External visibility → assign an externally reachable port.
		svcType = corev1.ServiceTypeLoadBalancer
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				LabelManagedBy:    ManagedByValue,
				LabelAppId:        manifest.AppId,
				LabelDeploymentId: deploymentID,
				LabelZoneId:       zoneID,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports:    ports,
			Type:     svcType,
		},
	}, nil
}

// containerPorts flattens every component interface into container ports.
func containerPorts(manifest *camara.AppManifest) []corev1.ContainerPort {
	var ports []corev1.ContainerPort
	for _, comp := range manifest.ComponentSpec {
		for _, ni := range comp.NetworkInterfaces {
			ports = append(ports, corev1.ContainerPort{
				Name:          strings.ToLower(ni.InterfaceId),
				ContainerPort: ni.Port,
				Protocol:      k8sProtocol(ni.Protocol),
			})
		}
	}
	return ports
}

// resourceRequirements maps CAMARA required resources to Kubernetes requests.
// numCPU follows the Vcpu format ("1", "0.500", "500m") which is already a valid
// Kubernetes CPU quantity; memory is in mega bytes → Mi.
func resourceRequirements(rr camara.RequiredResources) (*corev1.ResourceRequirements, error) {
	requests := corev1.ResourceList{}

	if rr.NumCPU != "" {
		cpu, err := resource.ParseQuantity(rr.NumCPU)
		if err != nil {
			return nil, fmt.Errorf("invalid numCPU %q: %w", rr.NumCPU, err)
		}
		requests[corev1.ResourceCPU] = cpu
	}
	if rr.Memory > 0 {
		mem, err := resource.ParseQuantity(fmt.Sprintf("%dMi", rr.Memory))
		if err != nil {
			return nil, fmt.Errorf("invalid memory %d: %w", rr.Memory, err)
		}
		requests[corev1.ResourceMemory] = mem
	}

	if len(requests) == 0 {
		return nil, nil
	}
	return &corev1.ResourceRequirements{Requests: requests}, nil
}

// k8sProtocol maps a CAMARA transport protocol to a Kubernetes protocol,
// defaulting ANY/unknown to TCP.
func k8sProtocol(p camara.Protocol) corev1.Protocol {
	switch p {
	case camara.ProtocolUDP:
		return corev1.ProtocolUDP
	default:
		return corev1.ProtocolTCP
	}
}

// containerName returns a stable container name from the first component, or the
// app name when no component is defined.
func containerName(manifest *camara.AppManifest) string {
	if len(manifest.ComponentSpec) > 0 && manifest.ComponentSpec[0].ComponentName != "" {
		return sanitize(manifest.ComponentSpec[0].ComponentName)
	}
	return sanitize(manifest.Name)
}

// workloadName derives a DNS-1123-safe workload name unique to a deployment.
// The deployment ID's leading segment keeps names distinct when the same app is
// deployed more than once.
func workloadName(appName, deploymentID string) string {
	base := sanitize(appName)
	suffix := deploymentID
	if idx := strings.Index(deploymentID, "-"); idx > 0 {
		suffix = deploymentID[:idx]
	}
	if suffix == "" {
		return base
	}
	return base + "-" + suffix
}

// sanitize lower-cases and strips characters not allowed in DNS-1123 labels.
func sanitize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "app"
	}
	return out
}
