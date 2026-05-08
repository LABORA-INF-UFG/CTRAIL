package ingress

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/gin-gonic/gin"
)

// ValidationError describes a single field-level validation failure.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationResult holds the outcome of a full request validation pass.
type ValidationResult struct {
	IsValid bool              `json:"isValid"`
	Errors  []ValidationError `json:"errors,omitempty"`
}

// Validator defines the validation contract for incoming requests.
type Validator interface {
	ValidateRequest(c *gin.Context) (*camara.TrafficInfluenceRequest, *ValidationResult)
	ValidateHeaders(c *gin.Context) *ValidationResult
	ValidateSecurity(c *gin.Context) *ValidationResult
}

// TrafficInfluenceValidator validates CAMARA Traffic Influence requests.
type TrafficInfluenceValidator struct {
	requiredHeaders []string
	apiKeys         map[string]bool // in production, replace with a proper key store
}

// NewTrafficInfluenceValidator creates a validator with the default required headers.
func NewTrafficInfluenceValidator() *TrafficInfluenceValidator {
	return &TrafficInfluenceValidator{
		requiredHeaders: []string{
			"Content-Type",
		},
		apiKeys: map[string]bool{
			"test-api-key": true,
		},
	}
}

// ValidateRequest runs all validation phases and returns the parsed request on success.
func (v *TrafficInfluenceValidator) ValidateRequest(c *gin.Context) (*camara.TrafficInfluenceRequest, *ValidationResult) {
	result := &ValidationResult{IsValid: true, Errors: []ValidationError{}}

	headerResult := v.ValidateHeaders(c)
	if !headerResult.IsValid {
		result.IsValid = false
		result.Errors = append(result.Errors, headerResult.Errors...)
	}

	securityResult := v.ValidateSecurity(c)
	if !securityResult.IsValid {
		result.IsValid = false
		result.Errors = append(result.Errors, securityResult.Errors...)
	}

	var req camara.TrafficInfluenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		result.IsValid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "body",
			Message: fmt.Sprintf("Invalid JSON format: %s", err.Error()),
		})
		return nil, result
	}

	bodyResult := v.validateRequestBody(&req)
	if !bodyResult.IsValid {
		result.IsValid = false
		result.Errors = append(result.Errors, bodyResult.Errors...)
	}

	if !result.IsValid {
		return nil, result
	}

	return &req, result
}

// ValidateHeaders checks that required HTTP headers are present and well-formed.
func (v *TrafficInfluenceValidator) ValidateHeaders(c *gin.Context) *ValidationResult {
	result := &ValidationResult{IsValid: true, Errors: []ValidationError{}}

	for _, header := range v.requiredHeaders {
		value := c.GetHeader(header)
		if value == "" {
			result.IsValid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   fmt.Sprintf("header.%s", header),
				Message: fmt.Sprintf("Required header '%s' is missing", header),
			})
		}
	}

	contentType := c.GetHeader("Content-Type")
	if contentType != "" && !strings.Contains(contentType, "application/json") {
		result.IsValid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "header.Content-Type",
			Message: "Content-Type must be application/json",
		})
	}

	return result
}

// ValidateSecurity performs optional API key validation. The key is not
// mandatory at this layer — the security module handles primary auth.
func (v *TrafficInfluenceValidator) ValidateSecurity(c *gin.Context) *ValidationResult {
	result := &ValidationResult{IsValid: true, Errors: []ValidationError{}}

	apiKey := c.GetHeader("X-API-Key")
	if apiKey == "" {
		return result
	}

	if !v.apiKeys[apiKey] {
		result.IsValid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "header.X-API-Key",
			Message: "Invalid API key",
		})
	}

	return result
}

func (v *TrafficInfluenceValidator) validateRequestBody(req *camara.TrafficInfluenceRequest) *ValidationResult {
	result := &ValidationResult{IsValid: true, Errors: []ValidationError{}}

	if req.AppId == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "appId",
			Message: "AppId is required",
		})
	}

	if req.EdgeCloudZoneId == "" && req.EdgeCloudRegion == "" && req.AppInstanceId == "" {
		result.IsValid = false
		result.Errors = append(result.Errors, ValidationError{
			Field:   "location",
			Message: "At least one of edgeCloudZoneId, edgeCloudRegion, or appInstanceId must be provided",
		})
	}

	if req.Device.IPv4Address != nil {
		if req.Device.IPv4Address.PublicAddress == "" {
			result.IsValid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   "device.ipv4Address.publicAddress",
				Message: "Public address is required when IPv4 address is specified",
			})
		}
	}

	if req.DestinationTrafficFilters != nil {
		if req.DestinationTrafficFilters.DestinationPort < 0 || req.DestinationTrafficFilters.DestinationPort > 65535 {
			result.IsValid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   "destinationTrafficFilters.destinationPort",
				Message: "Destination port must be between 0 and 65535",
			})
		}
	}

	if req.SourceTrafficFilters != nil {
		if req.SourceTrafficFilters.SourcePort < 0 || req.SourceTrafficFilters.SourcePort > 65535 {
			result.IsValid = false
			result.Errors = append(result.Errors, ValidationError{
				Field:   "sourceTrafficFilters.sourcePort",
				Message: "Source port must be between 0 and 65535",
			})
		}
	}

	return result
}

// HandleValidationError writes a structured 400 response with field-level details.
func HandleValidationError(c *gin.Context, result *ValidationResult) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error":   "Validation failed",
		"details": result.Errors,
	})
}

// HandleAuthenticationError writes a 401 response.
func HandleAuthenticationError(c *gin.Context, err error) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"error":   "Authentication failed",
		"message": err.Error(),
	})
}

// HandleAuthorizationError writes a 403 response.
func HandleAuthorizationError(c *gin.Context, err error) {
	c.JSON(http.StatusForbidden, gin.H{
		"error":   "Authorization failed",
		"message": err.Error(),
	})
}
