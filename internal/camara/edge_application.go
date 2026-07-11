package camara

// Data models for the CAMARA Edge Application Management API (Apps, Deployments,
// Clusters). Field names and JSON tags follow
// openapi/camara/edge_application_management.yaml. These are the northbound
// contract that CTRAIL translates into Kubernetes objects (the MEC orchestrator).

// PackageType is the format of the application image package.
type PackageType string

const (
	PackageTypeQCOW2     PackageType = "QCOW2"
	PackageTypeOVA       PackageType = "OVA"
	PackageTypeContainer PackageType = "CONTAINER"
	PackageTypeHelm      PackageType = "HELM"
	PackageTypeCSAR      PackageType = "CSAR"
)

// AppRepoType distinguishes public from private application repositories.
type AppRepoType string

const (
	AppRepoPrivate AppRepoType = "PRIVATEREPO"
	AppRepoPublic  AppRepoType = "PUBLICREPO"
)

// AppRepoAuthType is how repository credentials are formatted.
type AppRepoAuthType string

const (
	AppRepoAuthDocker AppRepoAuthType = "DOCKER"
	AppRepoAuthBasic  AppRepoAuthType = "HTTP_BASIC"
	AppRepoAuthBearer AppRepoAuthType = "HTTP_BEARER"
	AppRepoAuthNone   AppRepoAuthType = "NONE"
)

// Protocol is the IP transport protocol exposed by a component interface.
type Protocol string

const (
	ProtocolTCP Protocol = "TCP"
	ProtocolUDP Protocol = "UDP"
	ProtocolANY Protocol = "ANY"
)

// VisibilityType controls whether a component interface is reachable from
// outside the edge cloud (external) or only between components (internal).
type VisibilityType string

const (
	VisibilityExternal VisibilityType = "VISIBILITY_EXTERNAL"
	VisibilityInternal VisibilityType = "VISIBILITY_INTERNAL"
)

// AppRepo describes where the Application Provider stores the application image.
type AppRepo struct {
	Type        AppRepoType     `json:"type"`
	ImagePath   string          `json:"imagePath"`
	UserName    string          `json:"userName,omitempty"`
	Credentials string          `json:"credentials,omitempty"`
	AuthType    AppRepoAuthType `json:"authType,omitempty"`
	Checksum    string          `json:"checksum,omitempty"`
}

// RequiredResources captures the hardware requirements of an application. The
// CAMARA schema is a oneOf discriminated by infraKind (kubernetes / container /
// virtualMachine / dockerCompose); this struct models the fields CTRAIL uses
// when mapping to Kubernetes resource requests. numCPU follows the CAMARA Vcpu
// format ("1", "0.500", or "500m") and maps directly to a Kubernetes CPU
// quantity; memory is in mega bytes.
type RequiredResources struct {
	InfraKind string `json:"infraKind"`
	NumCPU    string `json:"numCPU,omitempty"`
	Memory    int32  `json:"memory,omitempty"`
}

// NetworkInterface is a single exposed port of an application component.
type NetworkInterface struct {
	InterfaceId    string         `json:"interfaceId"`
	Protocol       Protocol       `json:"protocol"`
	Port           int32          `json:"port"`
	VisibilityType VisibilityType `json:"visibilityType"`
}

// ComponentSpec exposes the network ports of one container of the application.
type ComponentSpec struct {
	ComponentName     string             `json:"componentName"`
	NetworkInterfaces []NetworkInterface `json:"networkInterfaces"`
}

// AppManifest is the application information and requirements provided by the
// Application Provider (POST /apps body). It is CTRAIL's equivalent of an ETSI
// MEC 010-2 application descriptor (AppD) — onboarding metadata, distinct from a
// running instance/deployment.
type AppManifest struct {
	AppId             string            `json:"appId,omitempty"`
	Name              string            `json:"name"`
	AppProvider       string            `json:"appProvider"`
	Version           string            `json:"version"`
	PackageType       PackageType       `json:"packageType"`
	OperatingSystem   any               `json:"operatingSystem,omitempty"`
	AppRepo           AppRepo           `json:"appRepo"`
	RequiredResources RequiredResources `json:"requiredResources"`
	ComponentSpec     []ComponentSpec   `json:"componentSpec"`
}

// SubmittedApp is returned by POST /apps once the app is onboarded.
type SubmittedApp struct {
	AppId string `json:"appId"`
}

// AppManifestWrapper is the GET /apps/{appId} response envelope.
type AppManifestWrapper struct {
	AppManifest AppManifest `json:"appManifest"`
}

// AppDeploymentRequest is the POST /deployments body: deploy an onboarded app to
// one or more Edge Cloud Zones.
type AppDeploymentRequest struct {
	AppDeploymentName     string              `json:"appDeploymentName"`
	AppId                 string              `json:"appId"`
	EdgeCloudZones        []string            `json:"edgeCloudZones"`
	KubernetesClusterRefs []string            `json:"kubernetesClusterRefs,omitempty"`
	SubscriptionRequest   *AppSubscriptionReq `json:"subscriptionRequest,omitempty"`
}

// AppDeploymentPatch is the PATCH /deployments/{id} body (RFC 7396 merge-patch).
// Pointers distinguish "field omitted" from "field set"; array fields are
// replaced wholesale per JSON Merge Patch semantics.
type AppDeploymentPatch struct {
	AppDeploymentName     *string   `json:"appDeploymentName,omitempty"`
	EdgeCloudZones        *[]string `json:"edgeCloudZones,omitempty"`
	KubernetesClusterRefs *[]string `json:"kubernetesClusterRefs,omitempty"`
}

// AppSubscriptionReq is the implicit status-change subscription attached to a
// deployment (CAMARA onAppDeploymentStatusChange callback target).
type AppSubscriptionReq struct {
	Sink  string   `json:"sink"`
	Types []string `json:"types,omitempty"`
}

// AppDeploymentInfo is the representation of a deployment returned on GET.
type AppDeploymentInfo struct {
	AppDeploymentName string   `json:"appDeploymentName"`
	AppDeploymentId   string   `json:"appDeploymentId"`
	AppId             string   `json:"appId"`
	EdgeCloudZones    []string `json:"edgeCloudZones"`
	AppInstances      []string `json:"appInstances"`
}

// ClusterInfo is Kubernetes cluster information returned by GET /clusters.
type ClusterInfo struct {
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	ClusterRef      string `json:"clusterRef"`
	EdgeCloudZoneId string `json:"edgeCloudZoneId"`
	EdgeCloudRegion string `json:"edgeCloudRegion,omitempty"`
	Version         string `json:"version,omitempty"`
}
