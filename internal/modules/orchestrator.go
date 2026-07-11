package modules

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/LABORA-INF-UFG/ctrail/internal/k8s"
	"github.com/LABORA-INF-UFG/ctrail/internal/mep"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/builder"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/ingress"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/mapper"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/response"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/security"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/state"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/topology"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GatewayOrchestrator wires all gateway modules together and exposes Gin handlers.
type GatewayOrchestrator struct {
	ingressModule   *ingress.IngressModule
	topologyEngine  *topology.TopologyEngine
	parameterMapper mapper.ParameterMapper
	requestBuilder  builder.RequestBuilder
	stateManager    state.StateManager
	responseModule  *response.ResponseHandlingModule
	securityManager security.SecurityManager
	// eam is the optional Edge Application Management sub-orchestrator. It is nil
	// until EnableEAM is called (main wires it when a Kubernetes backend is
	// available; tests inject a fake clientset).
	eam *EAMOrchestrator
}

// NewGatewayOrchestrator constructs a GatewayOrchestrator targeting the given NEF endpoint.
func NewGatewayOrchestrator(nefBaseURL, nefToken string) *GatewayOrchestrator {
	log.Printf("Initializing Gateway Orchestrator with NEF URL: %s", nefBaseURL)

	stateManager := state.NewInMemoryStateManager()
	topologyEngine := topology.NewTopologyEngine()

	securityConfig := &security.SecurityConfig{
		RateLimitPerMinute: 1000,
		RequireHTTPS:       false,
		EnableAuditLog:     true,
		MaxRequestSize:     10 * 1024 * 1024,
	}
	securityManager := security.NewDefaultSecurityManager(securityConfig)

	ingressModule := ingress.NewIngressModule()
	parameterMapper := mapper.NewTrafficInfluenceMapper(topologyEngine)
	requestBuilder := builder.NewNEFRequestBuilder(nefBaseURL, nefToken)
	responseModule := response.NewResponseHandlingModule(stateManager)

	orchestrator := &GatewayOrchestrator{
		ingressModule:   ingressModule,
		topologyEngine:  topologyEngine,
		parameterMapper: parameterMapper,
		requestBuilder:  requestBuilder,
		stateManager:    stateManager,
		responseModule:  responseModule,
		securityManager: securityManager,
	}

	log.Printf("Gateway Orchestrator initialized successfully")
	return orchestrator
}

// EnableEAM activates the Edge Application Management instance, backed by the
// given Kubernetes client. It shares the orchestrator's topology engine so zone
// definitions are common to both CTRAIL instances. nsPrefix overrides the
// Kubernetes namespace prefix used for deployments ("" keeps the default).
// mepClient is optional (nil disables OAI MEP service registration).
func (gw *GatewayOrchestrator) EnableEAM(k8sClient k8s.Client, nsPrefix string, mepClient mep.Client) {
	gw.eam = NewEAMOrchestrator(k8sClient, gw.topologyEngine, nsPrefix, mepClient)
	log.Printf("Edge Application Management instance enabled")
}

// ProcessTrafficInfluenceRequest handles POST /traffic-influences.
func (gw *GatewayOrchestrator) ProcessTrafficInfluenceRequest(c *gin.Context) {
	log.Printf("Processing Traffic Influence request from %s", c.ClientIP())

	securityCtx, err := gw.securityManager.AuthenticateRequest(c.Request)
	if err != nil {
		log.Printf("Security validation failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "Authentication failed",
			"details": err.Error(),
		})
		return
	}

	requiredPermissions := []string{"traffic_influence:create"}
	if err := gw.securityManager.ValidatePermissions(securityCtx, requiredPermissions); err != nil {
		log.Printf("Permission validation failed: %v", err)
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "Insufficient permissions",
			"details": err.Error(),
		})
		return
	}

	camaraRequest, err := gw.ingressModule.ProcessRequest(c)
	if err != nil {
		log.Printf("Ingress validation failed: %v", err)
		return // response already written by ingress module
	}

	if err := gw.parameterMapper.ValidateMapping(camaraRequest); err != nil {
		log.Printf("Mapping validation failed: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request for mapping",
			"details": err.Error(),
		})
		return
	}

	nefSubscription, err := gw.parameterMapper.MapToNEF(camaraRequest)
	if err != nil {
		log.Printf("Parameter mapping failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Parameter mapping failed",
			"details": err.Error(),
		})
		return
	}

	trafficInfluenceID := uuid.New().String()
	log.Printf("Generated trafficInfluenceID: %s for AppId: %s", trafficInfluenceID, camaraRequest.AppId)

	nefRequest, err := gw.requestBuilder.BuildCreateRequest(nefSubscription)
	if err != nil {
		log.Printf("NEF request building failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to build NEF request",
			"details": err.Error(),
		})
		return
	}

	nefResponse, err := gw.requestBuilder.ExecuteRequest(c.Request.Context(), nefRequest)
	if err != nil {
		log.Printf("NEF request execution failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "NEF request failed",
			"details": err.Error(),
		})
		return
	}

	responseProcessor := gw.responseModule.GetResponseProcessor()

	if nefResponse.StatusCode >= 400 {
		if err := responseProcessor.ProcessNEFError(nefResponse); err != nil {
			log.Printf("NEF error processing failed: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{
				"error":   "NEF request failed",
				"details": err.Error(),
			})
		} else {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": "NEF request failed",
			})
		}
		return
	}

	camaraResponse, err := responseProcessor.ProcessNEFResponseWithID(nefResponse, camaraRequest, trafficInfluenceID)
	if err != nil {
		log.Printf("Response processing failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to process NEF response",
			"details": err.Error(),
		})
		return
	}

	httpStatus := http.StatusOK
	switch nefResponse.StatusCode {
	case 201:
		httpStatus = http.StatusCreated
	case 200:
		httpStatus = http.StatusOK
	case 202:
		httpStatus = http.StatusAccepted
	default:
		httpStatus = http.StatusOK
	}

	log.Printf("Traffic Influence request processed successfully: %s (NEF status: %d, returning: %d)",
		camaraResponse.TrafficInfluenceID, nefResponse.StatusCode, httpStatus)
	c.JSON(httpStatus, camaraResponse)
}

// ProcessTrafficInfluenceUpdate handles PUT /traffic-influences/:id.
func (gw *GatewayOrchestrator) ProcessTrafficInfluenceUpdate(c *gin.Context) {
	trafficInfluenceID := c.Param("id")
	if trafficInfluenceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Traffic Influence ID is required",
		})
		return
	}

	log.Printf("Processing Traffic Influence update for ID: %s", trafficInfluenceID)

	camaraRequest, err := gw.ingressModule.ProcessRequest(c)
	if err != nil {
		log.Printf("Ingress validation failed: %v", err)
		return
	}

	existingEntry, err := gw.stateManager.GetSubscription(trafficInfluenceID)
	if err != nil {
		log.Printf("Subscription not found: %v", err)
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Traffic Influence subscription not found",
		})
		return
	}

	appIdDebug := "nil"
	if existingEntry.OriginalRequest != nil {
		appIdDebug = existingEntry.OriginalRequest.AppId
	}
	log.Printf("Found entry - TrafficInfluenceID: %s, NEFSubscriptionID: %s, AppId: %s",
		existingEntry.TrafficInfluenceID, existingEntry.NEFSubscriptionID, appIdDebug)

	nefSubscription, err := gw.parameterMapper.MapToNEF(camaraRequest)
	if err != nil {
		log.Printf("Parameter mapping failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Parameter mapping failed",
			"details": err.Error(),
		})
		return
	}

	nefRequest, err := gw.requestBuilder.BuildUpdateRequest(existingEntry.NEFSubscriptionID, nefSubscription)
	if err != nil {
		log.Printf("NEF update request building failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to build NEF update request",
			"details": err.Error(),
		})
		return
	}

	nefResponse, err := gw.requestBuilder.ExecuteRequest(c.Request.Context(), nefRequest)
	if err != nil {
		log.Printf("NEF update request execution failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "NEF update request failed",
			"details": err.Error(),
		})
		return
	}

	responseProcessor := gw.responseModule.GetResponseProcessor()

	if nefResponse.StatusCode >= 400 {
		if err := responseProcessor.ProcessNEFError(nefResponse); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{
				"error":   "NEF update failed",
				"details": err.Error(),
			})
		} else {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": "NEF update failed",
			})
		}
		return
	}

	// For UPDATE, preserve the gateway-assigned trafficInfluenceID across the NEF round-trip.
	camaraResponse, err := responseProcessor.ProcessNEFResponseWithID(nefResponse, camaraRequest, trafficInfluenceID)
	if err != nil {
		log.Printf("Update response processing had issues (may be expected for PUT): %v", err)
		if nefResponse.StatusCode == http.StatusOK {
			camaraResponse = &camara.TrafficInfluenceResponse{
				TrafficInfluenceID: trafficInfluenceID,
				AppId:              camaraRequest.AppId,
				State:              "active",
			}
			if camaraRequest != nil {
				camaraResponse.EdgeCloudZoneId = camaraRequest.EdgeCloudZoneId
				camaraResponse.EdgeCloudRegion = camaraRequest.EdgeCloudRegion
				camaraResponse.AppInstanceId = camaraRequest.AppInstanceId
				camaraResponse.NotificationUri = camaraRequest.NotificationUri
				camaraResponse.SourceTrafficFilters = camaraRequest.SourceTrafficFilters
				camaraResponse.DestinationTrafficFilters = camaraRequest.DestinationTrafficFilters
			}
			if err := gw.stateManager.UpdateSubscriptionState(trafficInfluenceID, state.StateActive, ""); err != nil {
				log.Printf("Warning: Failed to update subscription state: %v", err)
			}
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to process NEF update response",
				"details": err.Error(),
			})
			return
		}
	}

	log.Printf("Traffic Influence update processed successfully: %s", trafficInfluenceID)
	c.JSON(http.StatusOK, camaraResponse)
}

// ProcessTrafficInfluenceDelete handles DELETE /traffic-influences/:id.
func (gw *GatewayOrchestrator) ProcessTrafficInfluenceDelete(c *gin.Context) {
	trafficInfluenceID := c.Param("id")
	if trafficInfluenceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Traffic Influence ID is required",
		})
		return
	}

	log.Printf("Processing Traffic Influence delete for ID: %s", trafficInfluenceID)

	existingEntry, err := gw.stateManager.GetSubscription(trafficInfluenceID)
	if err != nil {
		log.Printf("Subscription not found: %v", err)
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Traffic Influence subscription not found",
		})
		return
	}

	afId := ""
	if existingEntry.OriginalRequest != nil {
		afId = existingEntry.OriginalRequest.AppId
	}
	nefRequest, err := gw.requestBuilder.BuildDeleteRequest(existingEntry.NEFSubscriptionID, afId)
	if err != nil {
		log.Printf("NEF delete request building failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to build NEF delete request",
			"details": err.Error(),
		})
		return
	}

	nefResponse, err := gw.requestBuilder.ExecuteRequest(c.Request.Context(), nefRequest)
	if err != nil {
		log.Printf("NEF delete request execution failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "NEF delete request failed",
			"details": err.Error(),
		})
		return
	}

	if nefResponse.StatusCode >= 400 && nefResponse.StatusCode != 404 {
		responseProcessor := gw.responseModule.GetResponseProcessor()
		if err := responseProcessor.ProcessNEFError(nefResponse); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{
				"error":   "NEF delete failed",
				"details": err.Error(),
			})
		} else {
			c.JSON(http.StatusBadGateway, gin.H{
				"error": "NEF delete failed",
			})
		}
		return
	}

	if err := gw.stateManager.UpdateSubscriptionState(trafficInfluenceID, state.StateDeleted, ""); err != nil {
		log.Printf("Warning: Failed to update subscription state to deleted: %v", err)
	}

	log.Printf("Traffic Influence delete processed successfully: %s", trafficInfluenceID)
	c.JSON(http.StatusNoContent, nil)
}

// ProcessTrafficInfluenceList handles GET /traffic-influences with optional query param filters.
func (gw *GatewayOrchestrator) ProcessTrafficInfluenceList(c *gin.Context) {
	log.Printf("Processing Traffic Influence list request")

	appId := c.Query("appId")
	edgeCloudZoneId := c.Query("edgeCloudZoneId")
	stateFilter := c.Query("state")

	log.Printf("Filters: appId=%s, edgeCloudZoneId=%s, state=%s", appId, edgeCloudZoneId, stateFilter)

	entries, err := gw.stateManager.ListSubscriptions()
	if err != nil {
		log.Printf("Failed to list subscriptions: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to retrieve subscriptions",
			"details": err.Error(),
		})
		return
	}

	var results []*camara.TrafficInfluenceResponse
	for _, entry := range entries {
		if appId != "" && entry.OriginalRequest.AppId != appId {
			continue
		}
		if edgeCloudZoneId != "" && entry.OriginalRequest.EdgeCloudZoneId != edgeCloudZoneId {
			continue
		}
		if stateFilter != "" && string(entry.State) != stateFilter {
			continue
		}

		camaraResponse, err := gw.parameterMapper.MapFromNEF(entry.NEFSubscription, entry.OriginalRequest)
		if err != nil {
			log.Printf("Warning: Failed to map subscription %s: %v", entry.TrafficInfluenceID, err)
			continue
		}

		// use gateway-assigned ID, not the NEF subscription ID
		camaraResponse.TrafficInfluenceID = entry.TrafficInfluenceID
		camaraResponse.State = string(entry.State)
		results = append(results, camaraResponse)
	}

	log.Printf("Traffic Influence list returned %d subscriptions (filtered from %d total)", len(results), len(entries))
	c.JSON(http.StatusOK, results)
}

// ProcessTrafficInfluenceGet handles GET /traffic-influences/:id.
func (gw *GatewayOrchestrator) ProcessTrafficInfluenceGet(c *gin.Context) {
	trafficInfluenceID := c.Param("id")
	if trafficInfluenceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Traffic Influence ID is required",
		})
		return
	}

	log.Printf("Processing Traffic Influence get for ID: %s", trafficInfluenceID)

	entry, err := gw.stateManager.GetSubscription(trafficInfluenceID)
	if err != nil {
		log.Printf("Subscription not found: %v", err)
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Traffic Influence subscription not found",
		})
		return
	}

	// Check if subscription has been deleted
	if entry.State == state.StateDeleted {
		log.Printf("Subscription %s has been deleted", trafficInfluenceID)
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Traffic Influence subscription not found",
		})
		return
	}

	camaraResponse, err := gw.parameterMapper.MapFromNEF(entry.NEFSubscription, entry.OriginalRequest)
	if err != nil {
		log.Printf("Failed to map subscription to CAMARA format: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to format subscription response",
		})
		return
	}

	// use gateway-assigned ID, not the NEF subscription ID
	camaraResponse.TrafficInfluenceID = entry.TrafficInfluenceID
	camaraResponse.State = string(entry.State)

	log.Printf("Traffic Influence get processed successfully: %s", trafficInfluenceID)
	c.JSON(http.StatusOK, camaraResponse)
}

// SetupRoutes registers all gateway routes on the given engine (no auth middleware).
func (gw *GatewayOrchestrator) SetupRoutes(router *gin.Engine) {
	log.Printf("Setting up gateway routes")

	trafficInfluence := router.Group("/traffic-influences")
	{
		trafficInfluence.POST("", gw.ProcessTrafficInfluenceRequest)
		trafficInfluence.GET("", gw.ProcessTrafficInfluenceList)
		trafficInfluence.GET("/:id", gw.ProcessTrafficInfluenceGet)
		trafficInfluence.PUT("/:id", gw.ProcessTrafficInfluenceUpdate)
		trafficInfluence.DELETE("/:id", gw.ProcessTrafficInfluenceDelete)
	}

	notificationHandler := gw.responseModule.GetNotificationHandler()
	notificationHandler.SetupNotificationEndpoint(router, "/nef-notifications")

	router.GET("/edge-cloud-zones", gw.HandleListEdgeCloudZones)

	if gw.eam != nil {
		gw.eam.RegisterRoutes(router)
	}

	admin := router.Group("/admin")
	{
		admin.GET("/health", gw.HandleHealthCheck)
		admin.GET("/stats", gw.HandleStatsCheck)
		admin.GET("/subscriptions", gw.HandleListSubscriptions)
	}

	log.Printf("Gateway routes configured successfully")
}

// HandleHealthCheck returns the health status of all gateway modules.
func (gw *GatewayOrchestrator) HandleHealthCheck(c *gin.Context) {
	stats := gw.stateManager.GetStatistics()

	health := gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"modules": gin.H{
			"ingress":  "operational",
			"topology": "operational",
			"mapper":   "operational",
			"builder":  "operational",
			"state":    "operational",
			"response": "operational",
		},
		"statistics": stats,
	}

	c.JSON(http.StatusOK, health)
}

// HandleStatsCheck returns detailed subscription statistics.
func (gw *GatewayOrchestrator) HandleStatsCheck(c *gin.Context) {
	stats := gw.stateManager.GetStatistics()
	c.JSON(http.StatusOK, stats)
}

// HandleListSubscriptions returns all raw subscription entries from the state manager.
func (gw *GatewayOrchestrator) HandleListSubscriptions(c *gin.Context) {
	subscriptions, err := gw.stateManager.ListSubscriptions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to list subscriptions",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"subscriptions": subscriptions,
		"count":         len(subscriptions),
	})
}

// HandleListEdgeCloudZones returns all available edge cloud zones, optionally
// filtered by region and/or status. Implements CAMARA Edge Application Management
// GET /edge-cloud-zones.
func (gw *GatewayOrchestrator) HandleListEdgeCloudZones(c *gin.Context) {
	regionFilter := c.Query("region")
	statusFilter := c.Query("status")

	mappings := gw.topologyEngine.GetAllZoneMappings()

	zones := make([]camara.EdgeCloudZone, 0, len(mappings))
	for _, m := range mappings {
		if regionFilter != "" && m.RegionID != regionFilter {
			continue
		}
		if statusFilter != "" && m.Status != statusFilter {
			continue
		}

		name := m.Name
		if name == "" {
			name = m.ZoneID
		}
		provider := m.Provider
		if provider == "" {
			provider = "unknown"
		}
		status := camara.EdgeCloudZoneStatus(m.Status)
		if status == "" {
			status = camara.EdgeCloudZoneStatusUnknown
		}

		zones = append(zones, camara.EdgeCloudZone{
			EdgeCloudZoneId:     m.ZoneID,
			EdgeCloudZoneName:   name,
			EdgeCloudProvider:   provider,
			EdgeCloudZoneStatus: status,
			EdgeCloudRegion:     m.RegionID,
		})
	}

	c.JSON(http.StatusOK, zones)
}

// SetupSecureRoutes registers all routes with security middleware applied.
func (gw *GatewayOrchestrator) SetupSecureRoutes(router *gin.Engine) {
	router.Use(gw.securityManager.SecurityGinMiddleware())
	router.Use(gw.securityManager.CORSGinMiddleware())
	router.Use(gw.securityManager.RateLimitGinMiddleware())
	router.Use(gw.securityManager.RequestLoggerGinMiddleware())

	public := router.Group("/")
	{
		public.GET("/health", gw.HandleHealthCheck)
		public.GET("/security/health", gw.HandleSecurityHealthCheck)
	}

	authenticated := router.Group("/")
	authenticated.Use(gw.securityManager.AuthenticationGinMiddleware([]string{}))
	{
		authenticated.GET("/stats", gw.HandleStatsCheck)
		authenticated.GET("/security/metrics", gw.HandleSecurityMetrics)
	}

	camaraAPI := router.Group("/")
	camaraAPI.Use(gw.securityManager.AuthenticationGinMiddleware([]string{}))
	{
		camaraAPI.POST("/traffic-influences", gw.ProcessTrafficInfluenceRequest)
		camaraAPI.GET("/traffic-influences", gw.ProcessTrafficInfluenceList)
		camaraAPI.GET("/traffic-influences/:id", gw.ProcessTrafficInfluenceGet)
		camaraAPI.PUT("/traffic-influences/:id", gw.ProcessTrafficInfluenceUpdate)
		camaraAPI.DELETE("/traffic-influences/:id", gw.ProcessTrafficInfluenceDelete)

		camaraAPI.GET("/edge-cloud-zones", gw.HandleListEdgeCloudZones)

		if gw.eam != nil {
			gw.eam.RegisterRoutes(camaraAPI)
		}
	}

	api := router.Group("/api/v1")
	api.Use(gw.securityManager.AuthenticationGinMiddleware([]string{"api_access"}))
	{
		trafficInfluence := api.Group("/traffic-influences")
		trafficInfluence.Use(gw.securityManager.AuthenticationGinMiddleware([]string{"traffic_influence:read"}))
		{
			trafficInfluence.POST("/", gw.securityManager.AuthenticationGinMiddleware([]string{"traffic_influence:create"}), gw.ProcessTrafficInfluenceRequest)
			trafficInfluence.GET("/:id", gw.ProcessTrafficInfluenceGet)
			trafficInfluence.PUT("/:id", gw.securityManager.AuthenticationGinMiddleware([]string{"traffic_influence:update"}), gw.ProcessTrafficInfluenceUpdate)
			trafficInfluence.DELETE("/:id", gw.securityManager.AuthenticationGinMiddleware([]string{"traffic_influence:delete"}), gw.ProcessTrafficInfluenceDelete)
		}

		admin := api.Group("/admin")
		admin.Use(gw.securityManager.AuthenticationGinMiddleware([]string{"admin"}))
		{
			admin.GET("/subscriptions", gw.HandleListSubscriptions)
			admin.POST("/security/api-keys", gw.HandleGenerateAPIKey)
			admin.DELETE("/security/cleanup", gw.HandleSecurityCleanup)
		}
	}

	callbacks := router.Group("/callbacks/nef")
	callbacks.Use(gw.securityManager.AuthenticationGinMiddleware([]string{"nef_callback"}))
	{
		callbacks.POST("/notifications", gw.HandleNEFNotification)
	}
}

// HandleSecurityHealthCheck returns the health status of the security module.
func (gw *GatewayOrchestrator) HandleSecurityHealthCheck(c *gin.Context) {
	health := gw.securityManager.SecurityHealthCheck()
	c.JSON(http.StatusOK, health)
}

// HandleSecurityMetrics returns security-related metrics.
func (gw *GatewayOrchestrator) HandleSecurityMetrics(c *gin.Context) {
	metrics := gw.securityManager.ExportSecurityMetrics()
	c.JSON(http.StatusOK, metrics)
}

// HandleGenerateAPIKey creates a new API key for the given client ID.
func (gw *GatewayOrchestrator) HandleGenerateAPIKey(c *gin.Context) {
	var request struct {
		ClientID string `json:"clientId" binding:"required"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request",
			"details": err.Error(),
		})
		return
	}

	apiKey, err := gw.securityManager.GenerateAPIKey(request.ClientID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to generate API key",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"apiKey":    apiKey,
		"clientId":  request.ClientID,
		"createdAt": time.Now(),
	})
}

// HandleSecurityCleanup removes expired tokens and security data.
func (gw *GatewayOrchestrator) HandleSecurityCleanup(c *gin.Context) {
	cleaned := gw.securityManager.CleanupExpiredTokens()

	c.JSON(http.StatusOK, gin.H{
		"message":       "Security cleanup completed",
		"tokensRemoved": cleaned,
		"timestamp":     time.Now(),
	})
}

// HandleNEFNotification processes incoming NEF callback notifications.
func (gw *GatewayOrchestrator) HandleNEFNotification(c *gin.Context) {
	log.Printf("Received NEF notification from %s", c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"message":   "Notification received",
		"timestamp": time.Now(),
	})
}

// Shutdown gracefully stops the orchestrator and cleans up transient state.
func (gw *GatewayOrchestrator) Shutdown(ctx context.Context) error {
	log.Printf("Shutting down Gateway Orchestrator")

	cleaned := gw.securityManager.CleanupExpiredTokens()
	log.Printf("Cleaned up %d expired security tokens", cleaned)

	if cleanedSubs, err := gw.stateManager.CleanupExpiredSubscriptions(24 * time.Hour); err != nil {
		log.Printf("Warning: Failed to cleanup expired subscriptions: %v", err)
	} else {
		log.Printf("Cleaned up %d expired subscriptions", cleanedSubs)
	}

	log.Printf("Gateway Orchestrator shutdown completed")
	return nil
}
