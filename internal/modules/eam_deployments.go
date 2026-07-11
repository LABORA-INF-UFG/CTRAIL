package modules

import (
	"context"
	"log"
	"net/http"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/LABORA-INF-UFG/ctrail/internal/k8s"
	"github.com/LABORA-INF-UFG/ctrail/internal/mep"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/state"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
)

// Deployments (/deployments) — ETSI MEC 010-2 application instance lifecycle,
// realised on Kubernetes. Creation and termination are asynchronous tasks
// (HTTP 202) tracked by a lifecycle operation occurrence in the state store.

// HandleCreateDeployment deploys an onboarded app to one or more edge cloud
// zones (POST /deployments). It mirrors MEC 010-2 appLcmInstanciatePOST: returns
// 202 Accepted with a Location header, backed by an op-occurrence record.
func (e *EAMOrchestrator) HandleCreateDeployment(c *gin.Context) {
	var req camara.AppDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeCamaraError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid deployment request: "+err.Error())
		return
	}
	if req.AppDeploymentName == "" || req.AppId == "" || len(req.EdgeCloudZones) == 0 {
		writeCamaraError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "appDeploymentName, appId and edgeCloudZones are required")
		return
	}

	manifest, err := e.appCatalog.Get(req.AppId)
	if err != nil {
		writeCamaraError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Unknown appId "+req.AppId)
		return
	}

	deploymentID := uuid.NewString()
	entry := &state.DeploymentEntry{
		AppDeploymentID:   deploymentID,
		AppDeploymentName: req.AppDeploymentName,
		AppID:             req.AppId,
		Request:           &req,
		State:             state.DeploymentDeploying,
		OpState:           state.OpProcessing,
	}

	placements, err := e.applyToZones(c.Request.Context(), manifest, deploymentID, req.EdgeCloudZones)
	if err != nil {
		// Best-effort rollback of whatever was created before the failure.
		e.tearDown(c.Request.Context(), placements)
		writeCamaraError(c, http.StatusInternalServerError, "INTERNAL", "Failed to deploy to edge cloud zones: "+err.Error())
		return
	}

	entry.Placements = placements
	entry.OpState = state.OpCompleted
	if err := e.deployStore.Store(entry); err != nil {
		e.tearDown(c.Request.Context(), placements)
		writeCamaraError(c, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	log.Printf("Created deployment %s of app %s across %d zone(s)", deploymentID, req.AppId, len(placements))
	c.Header("Location", "/deployments/"+deploymentID)
	c.JSON(http.StatusAccepted, gin.H{"appDeploymentId": deploymentID})
}

// applyToZones resolves each zone to a Kubernetes namespace and creates the
// Deployment (and Service, when the app exposes ports) there. It returns the
// placements created so the caller can persist or roll them back.
func (e *EAMOrchestrator) applyToZones(ctx context.Context, manifest *camara.AppManifest, deploymentID string, zones []string) ([]state.ZonePlacement, error) {
	var placements []state.ZonePlacement

	for _, zoneID := range zones {
		cluster, err := e.topology.ResolveCluster(zoneID)
		if err != nil {
			return placements, err
		}

		if err := e.k8sClient.EnsureNamespace(ctx, cluster.Namespace, map[string]string{
			"ctrail.io/edge-zone": zoneID,
		}); err != nil {
			return placements, err
		}

		dep, err := e.appMapper.MapToDeployment(manifest, deploymentID, zoneID)
		if err != nil {
			return placements, err
		}
		if _, err := e.k8sClient.ApplyDeployment(ctx, cluster.Namespace, dep); err != nil {
			return placements, err
		}

		placement := state.ZonePlacement{
			ZoneID:        zoneID,
			AppInstanceID: uuid.NewString(),
			Namespace:     cluster.Namespace,
			WorkloadName:  dep.Name,
		}

		svc, err := e.appMapper.MapToService(manifest, deploymentID, zoneID)
		if err != nil {
			return placements, err
		}
		if svc != nil {
			if _, err := e.k8sClient.ApplyService(ctx, cluster.Namespace, svc); err != nil {
				return placements, err
			}
			// Register an externally-visible service on the OAI MEP mp1 registry.
			if e.mepClient != nil && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
				placement.MEPServiceID = e.registerWithMEP(ctx, manifest, svc)
			}
		}

		placements = append(placements, placement)
	}

	return placements, nil
}

// registerWithMEP registers a deployment's service in the OAI MEP registry and
// returns its assigned id (empty on failure — registration is best-effort so a
// MEP outage never fails a deployment).
func (e *EAMOrchestrator) registerWithMEP(ctx context.Context, manifest *camara.AppManifest, svc *corev1.Service) string {
	port := 0
	if len(svc.Spec.Ports) > 0 {
		port = int(svc.Spec.Ports[0].Port)
	}
	registered, err := e.mepClient.Register(ctx, mep.Service{
		Name:        svc.Name,
		Type:        mepServiceType,
		Host:        svc.Name + "." + svc.Namespace,
		Port:        port,
		Path:        "/",
		Endpoints:   []mep.Endpoint{{Name: svc.Name, Path: "/", Method: "GET"}},
		Description: "CTRAIL-managed edge application " + manifest.Name,
	})
	if err != nil {
		log.Printf("MEP registration failed for %s (continuing): %v", svc.Name, err)
		return ""
	}
	return registered.Sid
}

// tearDown deletes the Kubernetes objects for the given placements, ignoring
// not-found errors. Namespaces are left in place as they may be shared by other
// deployments in the same zone.
func (e *EAMOrchestrator) tearDown(ctx context.Context, placements []state.ZonePlacement) {
	for _, p := range placements {
		if err := e.k8sClient.DeleteDeployment(ctx, p.Namespace, p.WorkloadName); err != nil {
			log.Printf("teardown: delete deployment %s/%s: %v", p.Namespace, p.WorkloadName, err)
		}
		if err := e.k8sClient.DeleteService(ctx, p.Namespace, p.WorkloadName); err != nil {
			log.Printf("teardown: delete service %s/%s: %v", p.Namespace, p.WorkloadName, err)
		}
		if e.mepClient != nil && p.MEPServiceID != "" {
			if err := e.mepClient.Deregister(ctx, p.MEPServiceID); err != nil {
				log.Printf("teardown: MEP deregister %s: %v", p.MEPServiceID, err)
			}
		}
	}
}

// HandleGetDeployments lists deployments (GET /deployments), optionally filtered
// by appId or appDeploymentId query parameters.
func (e *EAMOrchestrator) HandleGetDeployments(c *gin.Context) {
	appIDFilter := c.Query("appId")
	idFilter := c.Query("appDeploymentId")

	entries := e.deployStore.List(appIDFilter)
	out := make([]camara.AppDeploymentInfo, 0, len(entries))
	for _, entry := range entries {
		if idFilter != "" && entry.AppDeploymentID != idFilter {
			continue
		}
		e.refreshState(c.Request.Context(), entry)
		out = append(out, toDeploymentInfo(entry))
	}
	c.JSON(http.StatusOK, out)
}

// HandleGetDeployment returns a single deployment (GET /deployments/{id}),
// refreshing its observed lifecycle state from Kubernetes.
func (e *EAMOrchestrator) HandleGetDeployment(c *gin.Context) {
	id := c.Param("appDeploymentId")
	entry, err := e.deployStore.Get(id)
	if err != nil {
		writeCamaraError(c, http.StatusNotFound, "NOT_FOUND", "Deployment not found")
		return
	}
	e.refreshState(c.Request.Context(), entry)
	c.JSON(http.StatusOK, toDeploymentInfo(entry))
}

// HandleDeleteDeployment terminates a deployment (DELETE /deployments/{id}).
// Like MEC 010-2 appLcmTerminatePOST it is an asynchronous task: it returns 202
// and removes the Kubernetes workloads.
func (e *EAMOrchestrator) HandleDeleteDeployment(c *gin.Context) {
	id := c.Param("appDeploymentId")
	entry, err := e.deployStore.Get(id)
	if err != nil {
		writeCamaraError(c, http.StatusNotFound, "NOT_FOUND", "Deployment not found")
		return
	}

	e.tearDown(c.Request.Context(), entry.Placements)
	if err := e.deployStore.Delete(id); err != nil {
		writeCamaraError(c, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	log.Printf("Terminated deployment %s", id)
	c.Status(http.StatusAccepted)
}

// HandleUpdateDeployment updates a deployment (PATCH /deployments/{id}) using
// JSON Merge Patch semantics (RFC 7396): array fields, when present, replace the
// existing value wholesale. Re-targeting edgeCloudZones reconciles Kubernetes by
// creating workloads for added zones and tearing down removed ones — the MEC
// appLcmOperatePOST equivalent.
func (e *EAMOrchestrator) HandleUpdateDeployment(c *gin.Context) {
	id := c.Param("appDeploymentId")
	entry, err := e.deployStore.Get(id)
	if err != nil {
		writeCamaraError(c, http.StatusNotFound, "NOT_FOUND", "Deployment not found")
		return
	}

	var patch camara.AppDeploymentPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		writeCamaraError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid patch: "+err.Error())
		return
	}

	if patch.AppDeploymentName != nil {
		entry.AppDeploymentName = *patch.AppDeploymentName
	}

	if patch.EdgeCloudZones != nil {
		manifest, err := e.appCatalog.Get(entry.AppID)
		if err != nil {
			writeCamaraError(c, http.StatusConflict, "ABORTED", "App backing this deployment no longer exists")
			return
		}
		desired := *patch.EdgeCloudZones
		if len(desired) == 0 {
			writeCamaraError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "edgeCloudZones cannot be empty")
			return
		}

		current := map[string]state.ZonePlacement{}
		for _, p := range entry.Placements {
			current[p.ZoneID] = p
		}
		desiredSet := map[string]bool{}
		for _, z := range desired {
			desiredSet[z] = true
		}

		// Tear down placements for zones no longer desired.
		var kept []state.ZonePlacement
		for _, p := range entry.Placements {
			if !desiredSet[p.ZoneID] {
				e.tearDown(c.Request.Context(), []state.ZonePlacement{p})
			} else {
				kept = append(kept, p)
			}
		}

		// Create placements for newly desired zones.
		var toAdd []string
		for _, z := range desired {
			if _, exists := current[z]; !exists {
				toAdd = append(toAdd, z)
			}
		}
		added, err := e.applyToZones(c.Request.Context(), manifest, entry.AppDeploymentID, toAdd)
		if err != nil {
			e.tearDown(c.Request.Context(), added)
			writeCamaraError(c, http.StatusInternalServerError, "INTERNAL", "Failed to update deployment zones: "+err.Error())
			return
		}
		entry.Placements = append(kept, added...)
	}

	if err := e.deployStore.Update(entry); err != nil {
		writeCamaraError(c, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	e.refreshState(c.Request.Context(), entry)
	log.Printf("Updated deployment %s", id)
	c.JSON(http.StatusOK, toDeploymentInfo(entry))
}

// refreshState queries Kubernetes for each placement and updates the stored
// deployment's observed state: RUNNING once every workload has a ready replica,
// otherwise DEPLOYING. This is the MEC instantiation/operational state derived
// from the real backend rather than assumed.
func (e *EAMOrchestrator) refreshState(ctx context.Context, entry *state.DeploymentEntry) {
	if len(entry.Placements) == 0 {
		return
	}
	allReady := true
	for _, p := range entry.Placements {
		dep, err := e.k8sClient.GetDeployment(ctx, p.Namespace, p.WorkloadName)
		if err != nil {
			if k8s.IsNotFound(err) {
				allReady = false
				continue
			}
			log.Printf("refreshState: get deployment %s/%s: %v", p.Namespace, p.WorkloadName, err)
			return
		}
		if dep.Status.ReadyReplicas < 1 {
			allReady = false
		}
	}

	newState := state.DeploymentDeploying
	if allReady {
		newState = state.DeploymentRunning
	}
	if newState != entry.State {
		_ = e.deployStore.SetState(entry.AppDeploymentID, newState, state.OpCompleted, "")
		entry.State = newState
	}
}

// toDeploymentInfo maps an internal entry to the CAMARA AppDeploymentInfo body.
func toDeploymentInfo(entry *state.DeploymentEntry) camara.AppDeploymentInfo {
	zones := make([]string, 0, len(entry.Placements))
	instances := make([]string, 0, len(entry.Placements))
	for _, p := range entry.Placements {
		zones = append(zones, p.ZoneID)
		instances = append(instances, p.AppInstanceID)
	}
	return camara.AppDeploymentInfo{
		AppDeploymentName: entry.AppDeploymentName,
		AppDeploymentId:   entry.AppDeploymentID,
		AppId:             entry.AppID,
		EdgeCloudZones:    zones,
		AppInstances:      instances,
	}
}
