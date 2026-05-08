package response

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/LABORA-INF-UFG/ctrail/internal/modules/state"
	"github.com/LABORA-INF-UFG/ctrail/internal/nef"
	"github.com/gin-gonic/gin"
)

// ResponseProcessor defines the contract for processing NEF responses.
type ResponseProcessor interface {
	ProcessNEFResponse(resp *http.Response, originalRequest *camara.TrafficInfluenceRequest) (*camara.TrafficInfluenceResponse, error)
	ProcessNEFResponseWithID(resp *http.Response, originalRequest *camara.TrafficInfluenceRequest, trafficInfluenceID string) (*camara.TrafficInfluenceResponse, error)
	ProcessNEFError(resp *http.Response) error
}

// NotificationHandler defines the contract for receiving NEF callbacks.
type NotificationHandler interface {
	HandleNotification(c *gin.Context) error
	SetupNotificationEndpoint(router *gin.Engine, callbackPath string)
}

// ResponseHandlingModule bundles the response processor and notification handler.
type ResponseHandlingModule struct {
	stateManager        state.StateManager
	responseProcessor   ResponseProcessor
	notificationHandler NotificationHandler
}

// NewResponseHandlingModule creates a ResponseHandlingModule backed by the given state manager.
func NewResponseHandlingModule(stateManager state.StateManager) *ResponseHandlingModule {
	module := &ResponseHandlingModule{
		stateManager: stateManager,
	}

	module.responseProcessor = NewNEFResponseProcessor(stateManager)
	module.notificationHandler = NewNEFNotificationHandler(stateManager)

	return module
}

// NEFResponseProcessor processes HTTP responses from the NEF.
type NEFResponseProcessor struct {
	stateManager state.StateManager
}

// NewNEFResponseProcessor creates a response processor.
func NewNEFResponseProcessor(stateManager state.StateManager) *NEFResponseProcessor {
	return &NEFResponseProcessor{
		stateManager: stateManager,
	}
}

// ProcessNEFResponse processes a successful NEF response using the NEF subscription ID as the
// traffic influence ID (backward-compatible variant).
func (nrp *NEFResponseProcessor) ProcessNEFResponse(resp *http.Response, originalRequest *camara.TrafficInfluenceRequest) (*camara.TrafficInfluenceResponse, error) {
	return nrp.ProcessNEFResponseWithID(resp, originalRequest, "")
}

// ProcessNEFResponseWithID processes a NEF response using a pre-assigned trafficInfluenceID.
func (nrp *NEFResponseProcessor) ProcessNEFResponseWithID(resp *http.Response, originalRequest *camara.TrafficInfluenceRequest, trafficInfluenceID string) (*camara.TrafficInfluenceResponse, error) {
	log.Printf("Processing NEF response with status: %d", resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("NEF Response Body: %s", string(body))

	var nefSub nef.TrafficInfluSub
	if err := json.Unmarshal(body, &nefSub); err != nil {
		return nil, fmt.Errorf("failed to unmarshal NEF response: %w", err)
	}

	if nefSub.SubscriptionId == "" && nefSub.Self != "" {
		nefSub.SubscriptionId = nrp.extractSubscriptionIdFromSelfURL(nefSub.Self)
		log.Printf("Extracted NEF subscription ID from self URL: %s", nefSub.SubscriptionId)
	}

	if nefSub.SubscriptionId == "" {
		return nil, fmt.Errorf("NEF response missing subscriptionId and unable to extract from self URL")
	}

	// If no trafficInfluenceID was supplied, fall back to the NEF subscription ID for backward compatibility.
	if trafficInfluenceID == "" {
		trafficInfluenceID = nefSub.SubscriptionId
	}

	log.Printf("Mapping: TrafficInfluenceID=%s -> NEFSubscriptionID=%s", trafficInfluenceID, nefSub.SubscriptionId)

	camaraResp, err := nrp.translateToCAMARAResponseWithID(&nefSub, originalRequest, trafficInfluenceID)
	if err != nil {
		return nil, fmt.Errorf("failed to translate NEF response: %w", err)
	}

	if err := nrp.updateSubscriptionState(trafficInfluenceID, &nefSub, originalRequest, resp.StatusCode); err != nil {
		log.Printf("Warning: Failed to update subscription state: %v", err)
	}

	log.Printf("Successfully processed NEF response for subscription: %s (NEF: %s)", trafficInfluenceID, nefSub.SubscriptionId)
	return camaraResp, nil
}

// ProcessNEFError reads and returns an error response from the NEF as a Go error.
func (nrp *NEFResponseProcessor) ProcessNEFError(resp *http.Response) error {
	log.Printf("Processing NEF error response with status: %d", resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read error response body: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("NEF Error Response: %s", string(body))

	var nefError struct {
		Type     string `json:"type,omitempty"`
		Title    string `json:"title,omitempty"`
		Status   int    `json:"status,omitempty"`
		Detail   string `json:"detail,omitempty"`
		Instance string `json:"instance,omitempty"`
	}

	if json.Unmarshal(body, &nefError) == nil && nefError.Title != "" {
		return fmt.Errorf("NEF error %d: %s - %s", resp.StatusCode, nefError.Title, nefError.Detail)
	}

	return fmt.Errorf("NEF request failed with status %d: %s", resp.StatusCode, string(body))
}

func (nrp *NEFResponseProcessor) translateToCAMARAResponseWithID(nefSub *nef.TrafficInfluSub, originalRequest *camara.TrafficInfluenceRequest, trafficInfluenceID string) (*camara.TrafficInfluenceResponse, error) {
	response := &camara.TrafficInfluenceResponse{
		TrafficInfluenceID: trafficInfluenceID,
		AppId:              nefSub.AfServiceId,
		State:              "ordered", // initial state per CAMARA spec
	}

	if originalRequest != nil {
		response.EdgeCloudZoneId = originalRequest.EdgeCloudZoneId
		response.EdgeCloudRegion = originalRequest.EdgeCloudRegion
		response.AppInstanceId = originalRequest.AppInstanceId
		response.NotificationUri = originalRequest.NotificationUri
		response.SourceTrafficFilters = originalRequest.SourceTrafficFilters
		response.DestinationTrafficFilters = originalRequest.DestinationTrafficFilters
	}

	return response, nil
}

func (nrp *NEFResponseProcessor) updateSubscriptionState(trafficInfluenceID string, nefSub *nef.TrafficInfluSub, originalRequest *camara.TrafficInfluenceRequest, statusCode int) error {
	entry, err := nrp.stateManager.GetSubscription(trafficInfluenceID)
	if err != nil {
		entry = &state.SubscriptionEntry{
			TrafficInfluenceID: trafficInfluenceID,
			NEFSubscriptionID:  nefSub.SubscriptionId,
			OriginalRequest:    originalRequest,
			NEFSubscription:    nefSub,
		}

		if err := nrp.stateManager.StoreSubscription(entry); err != nil {
			return fmt.Errorf("failed to store new subscription: %w", err)
		}
	} else {
		if err := nrp.stateManager.UpdateNEFSubscriptionID(trafficInfluenceID, nefSub.SubscriptionId); err != nil {
			return fmt.Errorf("failed to update NEF subscription ID: %w", err)
		}
	}

	var newState state.SubscriptionState
	switch statusCode {
	case 201:
		newState = state.StateCreated
	case 200:
		newState = state.StateActive
	default:
		newState = state.StateOrdered
	}

	return nrp.stateManager.UpdateSubscriptionState(trafficInfluenceID, newState, "")
}

// extractSubscriptionIdFromSelfURL extracts the subscription ID from a NEF self URL.
// Expected format: .../subscriptions/{subscriptionId}
func (nrp *NEFResponseProcessor) extractSubscriptionIdFromSelfURL(selfURL string) string {
	parts := strings.Split(selfURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

// NEFNotificationHandler processes NEF callback notifications.
type NEFNotificationHandler struct {
	stateManager state.StateManager
}

// NewNEFNotificationHandler creates a notification handler.
func NewNEFNotificationHandler(stateManager state.StateManager) *NEFNotificationHandler {
	return &NEFNotificationHandler{
		stateManager: stateManager,
	}
}

// HandleNotification processes an incoming NEF notification and forwards it to the application.
func (nnh *NEFNotificationHandler) HandleNotification(c *gin.Context) error {
	log.Printf("Received notification from NEF at %s", c.Request.URL.Path)

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return fmt.Errorf("failed to read notification body: %w", err)
	}

	log.Printf("Notification body: %s", string(body))

	var notification NEFNotification
	if err := json.Unmarshal(body, &notification); err != nil {
		return fmt.Errorf("failed to unmarshal notification: %w", err)
	}

	if err := nnh.processNotification(&notification); err != nil {
		return fmt.Errorf("failed to process notification: %w", err)
	}

	if err := nnh.forwardNotification(&notification); err != nil {
		log.Printf("Warning: Failed to forward notification: %v", err)
	}

	c.JSON(http.StatusNoContent, nil)
	return nil
}

// SetupNotificationEndpoint registers the NEF callback endpoint on the router.
func (nnh *NEFNotificationHandler) SetupNotificationEndpoint(router *gin.Engine, callbackPath string) {
	log.Printf("Setting up notification endpoint at: %s", callbackPath)

	router.POST(callbackPath, func(c *gin.Context) {
		if err := nnh.HandleNotification(c); err != nil {
			log.Printf("Error handling notification: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to process notification",
			})
		}
	})
}

// NEFNotification represents an incoming callback from the NEF.
type NEFNotification struct {
	SubscriptionId  string               `json:"subscriptionId"`
	EventType       string               `json:"eventType"`
	ResourceUri     string               `json:"resourceUri,omitempty"`
	TrafficInfluSub *nef.TrafficInfluSub `json:"trafficInfluSub,omitempty"`
	Timestamp       time.Time            `json:"timestamp"`
	AdditionalData  map[string]any       `json:"additionalData,omitempty"`
}

func (nnh *NEFNotificationHandler) processNotification(notification *NEFNotification) error {
	log.Printf("Processing notification for subscription: %s, event: %s",
		notification.SubscriptionId, notification.EventType)

	entry, err := nnh.stateManager.GetSubscriptionByNEFID(notification.SubscriptionId)
	if err != nil {
		return fmt.Errorf("subscription not found for NEF ID %s: %w", notification.SubscriptionId, err)
	}

	if err := nnh.stateManager.UpdateLastNotification(entry.TrafficInfluenceID); err != nil {
		log.Printf("Warning: Failed to update last notification timestamp: %v", err)
	}

	switch notification.EventType {
	case "TRAFFIC_INFLUENCE_ACTIVE":
		return nnh.stateManager.UpdateSubscriptionState(entry.TrafficInfluenceID, state.StateActive, "")
	case "TRAFFIC_INFLUENCE_INACTIVE":
		return nnh.stateManager.UpdateSubscriptionState(entry.TrafficInfluenceID, state.StateInactive, "")
	case "TRAFFIC_INFLUENCE_ERROR":
		errorMsg := "Unknown error from NEF"
		if notification.AdditionalData != nil {
			if detail, ok := notification.AdditionalData["error"].(string); ok {
				errorMsg = detail
			}
		}
		return nnh.stateManager.UpdateSubscriptionState(entry.TrafficInfluenceID, state.StateError, errorMsg)
	default:
		log.Printf("Unknown event type: %s", notification.EventType)
		return nil
	}
}

func (nnh *NEFNotificationHandler) forwardNotification(notification *NEFNotification) error {
	entry, err := nnh.stateManager.GetSubscriptionByNEFID(notification.SubscriptionId)
	if err != nil {
		return fmt.Errorf("subscription not found: %w", err)
	}

	if entry.OriginalRequest == nil || entry.OriginalRequest.NotificationUri == "" {
		log.Printf("No notification URI configured for subscription: %s", entry.TrafficInfluenceID)
		return nil
	}

	camaraNotification := map[string]any{
		"trafficInfluenceId": entry.TrafficInfluenceID,
		"eventType":          nnh.translateEventType(notification.EventType),
		"timestamp":          notification.Timestamp,
		"appId":              entry.OriginalRequest.AppId,
	}

	notificationBody, err := json.Marshal(camaraNotification)
	if err != nil {
		return fmt.Errorf("failed to marshal CAMARA notification: %w", err)
	}

	return nnh.sendNotificationToApp(entry.OriginalRequest.NotificationUri, notificationBody, entry.OriginalRequest.NotificationAuthToken)
}

func (nnh *NEFNotificationHandler) translateEventType(nefEventType string) string {
	switch nefEventType {
	case "TRAFFIC_INFLUENCE_ACTIVE":
		return "TRAFFIC_INFLUENCE_ACTIVE"
	case "TRAFFIC_INFLUENCE_INACTIVE":
		return "TRAFFIC_INFLUENCE_INACTIVE"
	case "TRAFFIC_INFLUENCE_ERROR":
		return "TRAFFIC_INFLUENCE_ERROR"
	default:
		return "UNKNOWN"
	}
}

func (nnh *NEFNotificationHandler) sendNotificationToApp(uri string, body []byte, authToken string) error {
	log.Printf("Forwarding notification to application: %s", uri)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create notification request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authToken))
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("notification failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	log.Printf("Notification successfully forwarded to application")
	return nil
}

// GetResponseProcessor returns the module's response processor.
func (rhm *ResponseHandlingModule) GetResponseProcessor() ResponseProcessor {
	return rhm.responseProcessor
}

// GetNotificationHandler returns the module's notification handler.
func (rhm *ResponseHandlingModule) GetNotificationHandler() NotificationHandler {
	return rhm.notificationHandler
}
