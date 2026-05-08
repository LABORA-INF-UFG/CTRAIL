package ingress

import (
	"log"
	"net/http"

	"github.com/LABORA-INF-UFG/ctrail/internal/camara"
	"github.com/gin-gonic/gin"
)

// IngressModule validates and parses incoming CAMARA requests.
type IngressModule struct {
	validator Validator
}

// NewIngressModule creates an IngressModule with the default Traffic Influence validator.
func NewIngressModule() *IngressModule {
	return &IngressModule{
		validator: NewTrafficInfluenceValidator(),
	}
}

// ProcessRequest validates the incoming request and returns the parsed body.
// On failure it writes the error response and returns a non-nil error.
func (im *IngressModule) ProcessRequest(c *gin.Context) (*camara.TrafficInfluenceRequest, error) {
	log.Printf("Processing incoming request from %s", c.ClientIP())

	request, validationResult := im.validator.ValidateRequest(c)

	if !validationResult.IsValid {
		log.Printf("Validation failed for request: %+v", validationResult.Errors)
		HandleValidationError(c, validationResult)
		return nil, &ValidationFailedError{Errors: validationResult.Errors}
	}

	log.Printf("Request validated successfully for AppId: %s", request.AppId)
	return request, nil
}

// SetValidator replaces the validator (useful in tests).
func (im *IngressModule) SetValidator(validator Validator) {
	im.validator = validator
}

// Middleware returns a Gin handler that validates POST /traffic-influences
// requests and stores the parsed body under the "validatedRequest" context key.
func (im *IngressModule) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost && c.Request.URL.Path == "/traffic-influences" {
			request, err := im.ProcessRequest(c)
			if err != nil {
				c.Abort()
				return
			}
			c.Set("validatedRequest", request)
		}
		c.Next()
	}
}

// ValidationFailedError is returned when request validation does not pass.
type ValidationFailedError struct {
	Errors []ValidationError
}

func (e *ValidationFailedError) Error() string {
	return "Request validation failed"
}
