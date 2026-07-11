package modules

import (
	"log"
	"net/http"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/LABORA-INF-UFG/ctrail/internal/k8s"
	"github.com/LABORA-INF-UFG/ctrail/internal/mep"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/mapper"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/state"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/topology"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// mepServiceType is the OAI MEP service category CTRAIL registers edge apps
// under. OAI MEP constrains Service.type to a fixed MEC enum, so a generic edge
// app has no exact match; "Traffic" is used as a documented default (see the
// divergence note in internal/mep and docs/reference/MEC-COMPLIANCE.md).
const mepServiceType = "Traffic"

// EAMOrchestrator wires the Edge Application Management modules — the second
// CTRAIL instance. It reuses the agnostic pieces (security/ingress come from the
// hosting GatewayOrchestrator; state and topology packages are shared) and uses
// Kubernetes as the MEC orchestrator via the k8s backend client. The API-specific
// modules are the app mapper and the topology cluster resolution.
type EAMOrchestrator struct {
	appCatalog  *state.AppCatalog
	deployStore *state.DeploymentStore
	appMapper   mapper.AppMapper
	k8sClient   k8s.Client
	topology    *topology.TopologyEngine
	nsPrefix    string
	// mepClient registers externally-visible services in the OAI MEP registry.
	// nil when MEP registration is disabled.
	mepClient mep.Client
}

// NewEAMOrchestrator builds the EAM sub-orchestrator sharing the topology engine
// with the Traffic Influence pipeline and using the given Kubernetes client
// (a real client-go clientset in production, a fake clientset in tests).
// mepClient is optional (nil disables OAI MEP service registration).
func NewEAMOrchestrator(k8sClient k8s.Client, topologyEngine *topology.TopologyEngine, nsPrefix string, mepClient mep.Client) *EAMOrchestrator {
	if nsPrefix != "" {
		topologyEngine.SetNamespacePrefix(nsPrefix)
	}
	return &EAMOrchestrator{
		appCatalog:  state.NewAppCatalog(),
		deployStore: state.NewDeploymentStore(),
		appMapper:   mapper.NewKubernetesAppMapper(topologyEngine),
		k8sClient:   k8sClient,
		topology:    topologyEngine,
		nsPrefix:    nsPrefix,
		mepClient:   mepClient,
	}
}

// RegisterRoutes attaches the EAM endpoints to the given router group/engine.
func (e *EAMOrchestrator) RegisterRoutes(r gin.IRoutes) {
	r.POST("/apps", e.HandleSubmitApp)
	r.GET("/apps", e.HandleGetApps)
	r.GET("/apps/:appId", e.HandleGetApp)
	r.DELETE("/apps/:appId", e.HandleDeleteApp)

	r.POST("/deployments", e.HandleCreateDeployment)
	r.GET("/deployments", e.HandleGetDeployments)
	r.GET("/deployments/:appDeploymentId", e.HandleGetDeployment)
	r.PATCH("/deployments/:appDeploymentId", e.HandleUpdateDeployment)
	r.DELETE("/deployments/:appDeploymentId", e.HandleDeleteDeployment)

	r.GET("/clusters", e.HandleGetClusters)
}

// =============================================================================
// Applications (/apps) — ETSI MEC 010-2 application package resource.
// Onboarding metadata only; distinct from a running deployment.
// =============================================================================

// HandleSubmitApp onboards an AppManifest (POST /apps). It mirrors MEC 010-2
// app_packagesPOST: the app is a package, not yet instantiated.
func (e *EAMOrchestrator) HandleSubmitApp(c *gin.Context) {
	var manifest camara.AppManifest
	if err := c.ShouldBindJSON(&manifest); err != nil {
		writeCamaraError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid application manifest: "+err.Error())
		return
	}

	if manifest.Name == "" || manifest.AppRepo.ImagePath == "" {
		writeCamaraError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "name and appRepo.imagePath are required")
		return
	}

	// The Edge Cloud Provider assigns the appId when it is not supplied.
	if manifest.AppId == "" {
		manifest.AppId = uuid.NewString()
	} else if e.appCatalog.Exists(manifest.AppId) {
		writeCamaraError(c, http.StatusConflict, "ALREADY_EXISTS", "App already exists")
		return
	}

	if err := e.appCatalog.Store(&manifest); err != nil {
		writeCamaraError(c, http.StatusConflict, "ALREADY_EXISTS", err.Error())
		return
	}

	log.Printf("Onboarded app %s (%s)", manifest.AppId, manifest.Name)
	c.JSON(http.StatusCreated, camara.SubmittedApp{AppId: manifest.AppId})
}

// HandleGetApps lists all onboarded applications (GET /apps).
func (e *EAMOrchestrator) HandleGetApps(c *gin.Context) {
	apps := e.appCatalog.List()
	out := make([]camara.AppManifest, 0, len(apps))
	for _, a := range apps {
		out = append(out, *a)
	}
	c.JSON(http.StatusOK, out)
}

// HandleGetApp returns a single application (GET /apps/{appId}). The CAMARA
// response wraps the manifest in an appManifest envelope.
func (e *EAMOrchestrator) HandleGetApp(c *gin.Context) {
	appID := c.Param("appId")
	manifest, err := e.appCatalog.Get(appID)
	if err != nil {
		writeCamaraError(c, http.StatusNotFound, "NOT_FOUND", "App not found")
		return
	}
	c.JSON(http.StatusOK, camara.AppManifestWrapper{AppManifest: *manifest})
}

// HandleDeleteApp deletes an application (DELETE /apps/{appId}). It refuses with
// 409 ABORTED while any deployment still references the app, matching the CAMARA
// spec and the MEC package/instance dependency rule.
func (e *EAMOrchestrator) HandleDeleteApp(c *gin.Context) {
	appID := c.Param("appId")
	if !e.appCatalog.Exists(appID) {
		writeCamaraError(c, http.StatusNotFound, "NOT_FOUND", "App not found")
		return
	}
	if n := e.deployStore.CountByApp(appID); n > 0 {
		writeCamaraError(c, http.StatusConflict, "ABORTED", "App with a running application instance cannot be deleted")
		return
	}
	if err := e.appCatalog.Delete(appID); err != nil {
		writeCamaraError(c, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	log.Printf("Deleted app %s", appID)
	c.Status(http.StatusNoContent)
}

// writeCamaraError writes a CAMARA ErrorInfo body {status, code, message}.
func writeCamaraError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"status":  status,
		"code":    code,
		"message": message,
	})
}
