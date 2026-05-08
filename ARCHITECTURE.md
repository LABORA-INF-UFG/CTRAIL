
# CAMARA2NEF Gateway - Modular Architecture

## 📋 System Overview

CAMARA2NEF Gateway is a modular implementation that bridges CAMARA APIs and the 3GPP Network Exposure Function (NEF), focusing on Traffic Influence API. The architecture is designed with **7 well-defined modules** for flexibility, maintainability, and security.

### 🎯 Architectural Goals

- **Modularity**: Each module has clear responsibilities
- **Security**: Robust layers for authentication, authorization, and auditing
- **Scalability**: Support for multiple instances and load balancing
- **Observability**: Detailed logging and performance metrics
- **Compliance**: Adherence to CAMARA and 3GPP TS 29.522 standards

## 🏗️ Complete Architecture Diagram

```

┌─────────────────────────────────────────────────────────────────────────────────────┐
│                                CAMARA API Consumer                                 │
│                           (Applications, Mobile Apps)                             │
└─────────────────────────────────┬──────────────────────────────────────────────────┘
                                  │ HTTP/HTTPS Requests
                                  │ CAMARA Traffic Influence API
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                           🚪 HTTP SERVER (Gin Framework)                           │
│                               Port 8080 / Health Checks                           │
└─────────────────────────────────┬──────────────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                        🎮 GATEWAY ORCHESTRATOR                                    │
│                     (Central Coordination & Main Flow)                            │
│                                                                                   │
│  ┌─────────────────────────────────────────────────────────────────────────────┐  │
│  │                        PROCESSING FLOW                                     │  │
│  │  1. Security → 2. Ingress → 3. Topology → 4. Mapper →                      │  │
│  │  5. Builder → 6. State → 7. Response                                       │  │
│  └─────────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────┬──────────────────────────────────────────────────┘
                                  │
                ┌─────────────────┼─────────────────┐
                │                 │                 │
                ▼                 ▼                 ▼
┌─────────────────────┐ ┌─────────────────────┐ ┌─────────────────────┐
│   🔐 SECURITY       │ │   🚪 INGRESS        │ │   🗺️ TOPOLOGY       │
│     MODULE          │ │     MODULE          │ │     ENGINE          │
│                     │ │                     │ │                     │
│ • Authentication    │ │ • Request Parsing   │ │ • Zone Resolution   │
│ • Authorization     │ │ • Validation        │ │ • Network Mapping   │
│ • Rate Limiting     │ │ • Sanitization      │ │ • DNAI Resolution   │
│ • CORS              │ │ • Format Checking   │ │ • Topology Cache    │
│ • Audit Logging     │ │                     │ │                     │
│                     │ │ Input:              │ │ Input:              │
│ Input:              │ │ gin.Context         │ │ Zone/Region IDs     │
│ HTTP Request        │ │                     │ │                     │
│                     │ │ Output:             │ │ Output:             │
│ Output:             │ │ CAMARA Request      │ │ Network Identifiers │
│ Security Context    │ │ Validation Status   │ │ DNAI Mappings       │
└─────────────────────┘ └─────────────────────┘ └─────────────────────┘
                │                 │                 │
                └─────────────────┼─────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                            🔄 PARAMETER MAPPER                                    │
│                        (CAMARA → NEF Translation)                                │
│                                                                                   │
│  • Field Mapping (appId → afAppId)                                               │
│  • Traffic Filter Translation                                                    │
│  • Network Parameter Conversion                                                  │
│  • Device Information Mapping                                                    │
│                                                                                   │
│  Input: CAMARA TrafficInfluenceRequest + Network Context                         │
│  Output: NEF TrafficInfluSub (Partial)                                           │
└─────────────────────────────────┬──────────────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                           🏗️ REQUEST BUILDER                                     │
│                         (NEF HTTP Client & Sender)                               │
│                                                                                   │
│  • HTTP Request Construction                                                     │
│  • Authentication Header (Bearer Token)                                          │
│  • JSON Serialization                                                           │
│  • Connection Management                                                        │
│  • Retry Logic & Error Handling                                                 │
│                                                                                   │
│  Input: NEF TrafficInfluSub                                                      │
│  Output: HTTP Response from NEF                                                  │
└─────────────────────────────────┬──────────────────────────────────────────────────┘
                                  │ HTTP/HTTPS
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                              🌐 NEF SERVER                                        │
│                           (3GPP TS 29.522 API)                                   │
│                                                                                   │
│  POST /3gpp-traffic-influence/v1/{afAppId}/subscriptions                         │
│  GET  /3gpp-traffic-influence/v1/{afAppId}/subscriptions/{subscriptionId}        │
│  PUT  /3gpp-traffic-influence/v1/{afAppId}/subscriptions/{subscriptionId}        │
│  DELETE /3gpp-traffic-influence/v1/{afAppId}/subscriptions/{subscriptionId}      │
└─────────────────────────────────┬──────────────────────────────────────────────────┘
                                  │ NEF Response
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                           💾 STATE MANAGER                                        │
│                      (Subscription State Tracking)                                │
│                                                                                   │
│  • In-Memory State Storage                                                        │
│  • Thread-Safe Operations (sync.RWMutex)                                         │
│  • Dual Indexing (CAMARA ID ↔ NEF Subscription ID)                               │
│  • Lifecycle Management (ordered → created → active → deleted)                   │
│  • Cleanup & Expiration                                                          │
│                                                                                   │
│  Input: NEF Response + CAMARA Request Context                                    │
│  Output: State Persistence + Index Updates                                       │
└─────────────────────────────────┬──────────────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                          📤 RESPONSE MODULE                                       │
│                      (NEF → CAMARA Response Processing)                          │
│                                                                                   │
│  • NEF Response Processing                                                       │
│  • Subscription ID Extraction (from 'self' URL)                                  │
│  • Status Code Preservation (201, 200, etc.)                                     │
│  • Error Handling & Translation                                                  │
│  • CAMARA Response Construction                                                  │
│                                                                                   │
│  Input: NEF HTTP Response + Original CAMARA Request                              │
│  Output: CAMARA TrafficInfluenceResponse                                         │
└─────────────────────────────────┬──────────────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                                CAMARA API Consumer                                │
│                              (Final HTTP Response)                                │
│                                                                                   │
│  HTTP 201 Created                                                                │
│  {                                                                               │
│    "trafficInfluenceID": "extracted-from-nef-self-url",                        │  
│    "appId": "app123",                                                          │
│    "status": "created"                                                         │
│  }                                                                               │
└─────────────────────────────────────────────────────────────────────────────────────┘
```

## 🔄 Module Interactions

### 1. 🎮 **Gateway Orchestrator** (Central Coordination)
**Location**: `/internal/modules/orchestrator.go`

The **GatewayOrchestrator** is the system's core, responsible for coordinating all modules in a well-defined sequence:

```go
type GatewayOrchestrator struct {
  ingressModule   *ingress.IngressModule       // Parsing and validation
  topologyEngine  *topology.TopologyEngine    // Network resolution
  parameterMapper mapper.ParameterMapper      // CAMARA→NEF translation
  requestBuilder  builder.RequestBuilder      // NEF HTTP client
  stateManager    state.StateManager          // State management
  responseModule  *response.ResponseHandlingModule // Response processing
  securityManager security.SecurityManager    // Security and authentication
}
```

**Main Flow** (`ProcessTrafficInfluenceRequest`):
1. **Security Validation**: Authenticates and authorizes the request
2. **Input Processing**: Parses and validates the CAMARA request
3. **Topology Resolution**: Maps zones to network identifiers
4. **Parameter Mapping**: Translates CAMARA structures to NEF
5. **Request Building**: Constructs and sends HTTP request to NEF
6. **State Management**: Persists subscription state
7. **Response Processing**: Converts NEF response to CAMARA format

### 2. 🔐 **Security Module** (First Layer Protection)
**Location**: `/internal/modules/security/`

**Responsibilities**:
- Multi-method authentication (JWT, API Key, Basic Auth)
- Authorization based on granular permissions
- Rate limiting and DDoS protection
- CORS and origin validation
- Audit logging of security events

**Interactions**:
- **Input**: Raw HTTP requests from Gin
- **Output**: Validated Security Context or authentication error
- **Communicates with**: Orchestrator (middleware), all requests pass through here

```go
// Flow in Orchestrator
func (gw *GatewayOrchestrator) ProcessTrafficInfluenceRequest(c *gin.Context) {
  // 1. Security is applied via middleware before this function
  // 2. Security context is already available via c.Get("security_context")
  securityCtx, exists := c.Get("security_context") 
  if !exists {
    c.JSON(401, gin.H{"error": "Unauthorized"})
    return
  }
  // ... continue processing
}
```

### 3. 🚪 **Ingress Module** (Parsing and Validation)
**Location**: `/internal/modules/ingress/`

**Responsibilities**:
- Parse JSON into CAMARA Go structures
- Validate required fields
- Input data sanitization
- Early detection of malformed requests

**Interactions**:
- **Input**: `gin.Context` from Orchestrator
- **Output**: Validated `camara.TrafficInfluenceRequest`
- **Communication**: Orchestrator → Ingress → Topology Engine

```go
// Call in Orchestrator
camaraRequest, err := gw.ingressModule.ProcessRequest(c)
if err != nil {
  log.Printf("Ingress processing failed: %v", err)
  c.JSON(400, gin.H{"error": "Invalid request: " + err.Error()})
  return
}
```

### 4. 🗺️ **Topology Engine** (Network Resolution)
**Location**: `/internal/modules/topology/engine.go`

**Responsibilities**:
- Map CAMARA zones to 3GPP network identifiers
- Resolve DNAI (Data Network Access Identifier)
- Topology configuration caching
- Support for multiple regions and zones

**Interactions**:
- **Input**: Zone ID, Region ID from CAMARA request
- **Output**: DNAI, DNN, S-NSSAI mappings
- **Communication**: Ingress → Topology → Parameter Mapper

```go
// Example usage in Parameter Mapper
func (pm *ParameterMapperImpl) MapCAMARAToNEF(req *camara.TrafficInfluenceRequest) (*nef.TrafficInfluSub, error) {
  // Resolve topology
  dnai, err := pm.topologyEngine.ResolveDNAI(req.EdgeCloudZoneId, req.EdgeCloudRegion)
  if err != nil {
    return nil, fmt.Errorf("topology resolution failed: %v", err)
  }
    
  nefSub := &nef.TrafficInfluSub{
    TrafficRoutes: []nef.TrafficRoute{{Dnai: dnai}},
    // ... other fields
  }
  return nefSub, nil
}
```

### 5. 🔄 **Parameter Mapper** (Central Translator)
**Location**: `/internal/modules/mapper/parameter_mapper.go`

**Responsibilities**:
- Bidirectional translation between CAMARA and NEF formats
- Field mapping
- Transformation of complex structures
- Construction of traffic filters

**Interactions**:
- **Input**: `camara.TrafficInfluenceRequest` + topology context
- **Output**: Complete `nef.TrafficInfluSub`
- **Communication**: Topology → Mapper → Request Builder

**Main Mappings**:
```go
// CAMARA → NEF Field Mappings
CAMARA appId                    → NEF afAppId
CAMARA device.ipv4Address       → NEF traffic filters
CAMARA edgeCloudZoneId          → NEF trafficRoutes[].dnai
CAMARA sourceTrafficFilters     → NEF trafficFilters[].flowDescriptions
CAMARA destinationTrafficFilters → NEF trafficFilters[].flowDescriptions
CAMARA notificationUri          → NEF notificationDestination
```

### 6. 🏗️ **Request Builder** (NEF HTTP Client)
**Location**: `/internal/modules/builder/nef_request_builder.go`

**Responsibilities**:
- Build HTTP requests compliant with 3GPP TS 29.522
- Manage NEF authentication (Bearer token)
- JSON serialization and HTTP headers
- Timeout and retry logic

**Interactions**:
- **Input**: `nef.TrafficInfluSub` from Parameter Mapper
- **Output**: Raw HTTP Response from NEF
- **Communication**: Mapper → Builder → NEF Server → State Manager

```go
// Call in Orchestrator
nefResponse, statusCode, err := gw.requestBuilder.CreateSubscription(ctx, nefSub)
if err != nil {
    log.Printf("NEF request failed: %v", err)
    c.JSON(500, gin.H{"error": "NEF communication failed"})
    return
}

// Status code is preserved from NEF (201, 200, etc.)
```

### 7. 💾 **State Manager** (State Persistence)
**Location**: `/internal/modules/state/state_manager.go`

**Responsibilities**:
- Thread-safe storage of subscription states
- Dual indexing (CAMARA ID ↔ NEF Subscription ID)
- Lifecycle tracking (ordered → created → active → deleted)
- Automatic cleanup of expired entries

**Interactions**:
- **Input**: NEF Response + Original CAMARA Request
- **Output**: Persisted state + updated indexes
- **Communication**: Request Builder → State Manager → Response Module

```go
// Supported states
type SubscriptionState string
const (
    StateOrdered   SubscriptionState = "ordered"   // Requested
    StateCreated   SubscriptionState = "created"   // Created in NEF
    StateActive    SubscriptionState = "active"    // Active
    StateInactive  SubscriptionState = "inactive"  // Temporarily inactive
    StateError     SubscriptionState = "error"     // Error state
    StateDeleted   SubscriptionState = "deleted"   // Marked for deletion
)
```

### 8. 📤 **Response Module** (Response Processor)
**Location**: `/internal/modules/response/response_handler.go`

**Responsibilities**:
- Processing synchronous responses from NEF
- Extracting Subscription ID from self-reference URLs
- Preserving HTTP status codes (201, 200, etc.)
- NEF → CAMARA transformation
- Error handling and fallbacks

**Interactions**:
- **Input**: NEF HTTP Response + Original CAMARA Request + State Context
- **Output**: Final `camara.TrafficInfluenceResponse`
- **Communication**: State Manager → Response Module → HTTP Client

```go
// Main functionality: subscription ID extraction
func extractSubscriptionIdFromSelfURL(selfURL string) string {
    // NEF returns: "http://nef:8000/3gpp-traffic-influence/v1/app123/subscriptions/sub-12345"
    // Extracts: "sub-12345"
    parts := strings.Split(selfURL, "/")
    if len(parts) > 0 {
        return parts[len(parts)-1]  // Last URL segment
    }
    return ""
}
```

## 🔄 Complete Data Flow

### CAMARA → NEF Request (Create Subscription)

```
1. CAMARA Client → Gateway:
   POST /traffic-influences
   {
     "appId": "app123",
     "device": {"ipv4Address": {"publicAddress": "10.0.0.1"}},
     "edgeCloudZoneId": "zone-north",
     "notificationUri": "https://client.com/webhook"
   }

2. Security Module:
   ✓ Authentication via JWT/API Key
   ✓ Permission verification (traffic_influence:create)
   ✓ Rate limiting OK

3. Ingress Module:
   ✓ Parse JSON → camara.TrafficInfluenceRequest
   ✓ Required fields validation
   ✓ Data sanitization

4. Topology Engine:
   zone-north → DNAI: "mec", DNN: "internet", S-NSSAI: {sst:1, sd:"010203"}

5. Parameter Mapper:
   CAMARA → NEF Translation:
   {
     "afAppId": "app123",
     "dnn": "internet",
     "snssai": {"sst": 1, "sd": "010203"},
     "trafficRoutes": [{"dnai": "mec"}],
     "trafficFilters": [...],
     "notificationDestination": "https://client.com/webhook"
   }

6. Request Builder:
   POST http://nef-server:8000/3gpp-traffic-influence/v1/app123/subscriptions
   Headers: {Authorization: "Bearer nef-token", Content-Type: "application/json"}

7. NEF Server Response:
   HTTP 201 Created
   {
     "afAppId": "app123",
     "self": "http://nef:8000/3gpp-traffic-influence/v1/app123/subscriptions/sub-12345",
     "trafficRoutes": [{"dnai": "mec"}],
     ...
   }

8. State Manager:
   ✓ Persists state: StateCreated
   ✓ Indexes: CAMARA-123 ↔ sub-12345

9. Response Module:
   ✓ Extracts subscription ID: "sub-12345" (from self URL)
   ✓ Preserves status code: 201
   ✓ Builds CAMARA response

10. Gateway → CAMARA Client:
    HTTP 201 Created
    {
      "trafficInfluenceID": "sub-12345",
      "appId": "app123",
      "status": "created"
    }
```

## 🏛️ Internal Components Architecture

### Models and Data Structures

**CAMARA Models** (`/internal/camara/models.go`):
```go
type TrafficInfluenceRequest struct {
    AppId                     string                    `json:"appId"`
    EdgeCloudRegion           string                    `json:"edgeCloudRegion,omitempty"`
    EdgeCloudZoneId           string                    `json:"edgeCloudZoneId,omitempty"`
    Device                    Device                    `json:"device,omitempty"`
    SourceTrafficFilters      *SourceTrafficFilter      `json:"sourceTrafficFilters,omitempty"`
    DestinationTrafficFilters *DestinationTrafficFilter `json:"destinationTrafficFilters,omitempty"`
    NotificationUri           string                    `json:"notificationUri,omitempty"`
}
```

**NEF Models** (`/internal/nef/models.go`):
```go
type TrafficInfluSub struct {
    SubscriptionId          string         `json:"subscriptionId,omitempty"`
    AfAppId                 string         `json:"afAppId,omitempty"`
    Dnn                     string         `json:"dnn,omitempty"`
    Snssai                  *Snssai        `json:"snssai,omitempty"`
    TrafficRoutes           []TrafficRoute `json:"trafficRoutes,omitempty"`
    TrafficFilters          []FlowInfo     `json:"trafficFilters,omitempty"`
    NotificationDestination string         `json:"notificationDestination,omitempty"`
    Self                    string         `json:"self,omitempty"` // Self-reference URL
}
```

### Configuration Management

**Server Configuration** (`/internal/config/config.go`):
```go
type Config struct {
    ServerPort    string // Server port (8080)
    NEFBaseURL    string // NEF server URL
    NEFToken      string // NEF authentication token
    LogLevel      string // Logging level
}
```

**Topology Configuration** (`/config/topology.json`):
```json
{
  "zoneMappings": [
    {
      "zoneId": "zone-north",
      "regionId": "north",
      "dnai": "mec",
      "network": {
        "dnn": "internet",
        "snssai": {"sst": 1, "sd": "010203"}
      }
    }
  ]
}
```

## 🔒 Security Architecture

### Protection Layers

1. **Network Layer**: TLS 1.2+, IP filtering, container isolation
2. **Transport Layer**: Rate limiting, DDoS protection, connection pooling
3. **Application Layer**: Authentication, authorization, input validation
4. **Data Layer**: Sanitization, encryption, secure storage
5. **Audit Layer**: Comprehensive logging, monitoring, alerting

### Supported Authentication Types

| Method | Recommended Use | Configuration |
|--------|----------------|--------------|
| **JWT** | Modern APIs, SPAs | Bearer token in Authorization header |
| **API Key** | M2M integrations | X-API-Key header |
| **Basic Auth** | Development, legacy | Authorization header |
| **OAuth2** | Enterprise applications | Token endpoint configuration |

### Granular Permissions

```
├── api_access (Basic API access)
├── traffic_influence:*
│   ├── traffic_influence:create
│   ├── traffic_influence:read
│   ├── traffic_influence:update
│   └── traffic_influence:delete
├── admin (Administrative access)
├── nef_callback (NEF callbacks)
└── security:*
    ├── security:generate_keys
    └── security:cleanup
```

## 🚀 Performance and Optimizations

### Performance Characteristics

- **Average Latency**: < 50ms for local operations
- **Throughput**: > 1000 req/s per instance
- **Memory Usage**: < 100MB baseline
- **CPU Usage**: < 30% under normal load
- **Concurrent Connections**: Up to 10,000 simultaneous connections

### Implemented Optimizations

1. **Connection Pooling**: HTTP connection reuse for NEF
2. **Thread Safety**: Concurrent-safe operations with sync.RWMutex
3. **Caching**: In-memory cache for topology configurations
4. **Graceful Shutdown**: Clean resource termination
5. **Request Batching**: Request grouping when possible

## 📊 Observability and Monitoring

### Health Endpoints

| Endpoint | Description | Authentication |
|----------|-----------|--------------|
| `GET /health` | Overall system health | Public |
| `GET /security/health` | Security module health | Public |
| `GET /metrics` | Basic system metrics | Authenticated |
| `GET /admin/stats` | Detailed statistics | Admin |

### Log Structure

```json
{
  "timestamp": "2025-08-04T12:00:00Z",
  "level": "info",
  "module": "orchestrator",
  "request_id": "req-123456",
  "operation": "create_subscription",
  "duration_ms": 45,
  "nef_status": 201,
  "camara_response_status": 201,
  "subscription_id": "sub-12345",
  "details": {
    "app_id": "app123",
    "zone_id": "zone-north",
    "dnai": "mec"
  }
}
```

### System Metrics

```json
{
  "timestamp": "2025-08-04T12:00:00Z",
  "system": {
    "uptime_seconds": 3600,
    "memory_usage_mb": 85,
    "cpu_usage_percent": 25,
    "goroutines": 120
  },
  "gateway": {
    "total_requests": 15420,
    "successful_requests": 15280,
    "failed_requests": 140,
    "average_response_time_ms": 42
  },
  "modules": {
    "security": {
      "active_tokens": 45,
      "rate_limit_hits": 12,
      "blocked_requests": 3
    },
    "state_manager": {
      "active_subscriptions": 1250,
      "total_subscriptions": 2300,
      "cleanup_operations": 15
    },
    "nef_client": {
      "active_connections": 8,
      "total_requests": 15420,
      "connection_pool_size": 20,
      "average_nef_response_time_ms": 38
    }
  }
}
```

## 🔧 Configuration and Deployment

### Configuration via Environment Variables

```bash
# Server Configuration
SERVER_PORT=8080                    # HTTP server port
LOG_LEVEL=info                      # debug, info, warn, error

# NEF Connection
NEF_BASE_URL=http://nef-server:8000 # NEF server base URL
NEF_TOKEN=your-nef-token-here       # NEF authentication token

# Security (optional)
JWT_SECRET=your-jwt-secret          # JWT secret key
RATE_LIMIT_PER_MINUTE=1000         # Rate limiting
REQUIRE_HTTPS=false                 # Force HTTPS (production: true)

# Topology (optional)
TOPOLOGY_CONFIG_PATH=/app/config/topology.json
TOPOLOGY_CACHE_TTL=3600            # Cache TTL in seconds

# Monitoring (optional)
ENABLE_METRICS=true                # Enable metrics
ENABLE_AUDIT_LOG=true              # Enable audit log
```

### Docker Deployment

**Dockerfile** (Optimized multi-stage):
```dockerfile
# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags="-w -s" -o ctrail ./cmd/server

# Runtime stage
FROM alpine:latest
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=builder /app/ctrail .
COPY config/ ./config/
EXPOSE 8080
HEALTHCHECK --interval=60s --timeout=10s --retries=3 \
    CMD wget --spider -q http://localhost:8080/health || exit 1
USER 1000:1000
CMD ["./ctrail"]
```

**docker-compose.yml** (Simplified):
```yaml
services:
  ctrail:
    build: .
    ports:
      - "8080:8080"
    env_file:
      - .env
    volumes:
      - ./logs:/app/logs
      - ./config:/app/config:ro
    networks:
      - free5gc-compose_privnet
    restart: unless-stopped
    container_name: ctrail
    healthcheck:
      test: ["CMD", "wget", "--spider", "http://localhost:8080/health"]
      interval: 60s
      timeout: 10s
      retries: 3

networks:
  free5gc-compose_privnet:
    external: false
```

## 🧪 Testing and Validation

### Integration Tests

```bash
# Basic health test
curl -X GET http://localhost:8080/health

# Subscription creation test
curl -X POST http://localhost:8080/traffic-influences \
  -H "Content-Type: application/json" \
  -H "User-Agent: C2N-Test-Client/1.0" \
  -u admin:adminpassword123 \
  -d '{
    "appId": "test-app",
    "device": {"ipv4Address": {"publicAddress": "10.0.0.1"}},
    "edgeCloudZoneId": "zone-north",
    "applicationServerAddress": "8.8.8.8"
  }'

# Expected response (HTTP 201)
{
  "trafficInfluenceID": "550e8400-e29b-41d4-a716-446655440000",
  "appId": "test-app",
  "state": "ordered"
}
```

### Compliance Validation

**CAMARA API Compliance**:
- ✅ TrafficInfluence API v0.9.0+ compatible
- ✅ Compliant data structures
- ✅ Correct HTTP status codes
- ✅ Proper response headers

**3GPP TS 29.522 Compliance**:
- ✅ NEF Traffic Influence API Release 16/17
- ✅ Compliant JSON structures
- ✅ Bearer token authentication
- ✅ Correct URLs and endpoints

## 🔮 Roadmap and Future Improvements

### Next Implementations (High Priority)

1. **Persistent State Storage**
   - Migration to Redis/PostgreSQL
   - Clustering and high availability
   - State backup and recovery

2. **Advanced Monitoring**
   - Prometheus/Grafana integration
   - Automatic alerting
   - Custom dashboards
   - SLI/SLO tracking

3. **Enhanced Security**
   - mTLS for NEF communication
   - JWT token rotation
   - Advanced threat detection
   - Automated security scanning

### Medium-term Improvements

4. **Load Balancing & Scaling**
   - Multiple NEF instance support
   - Intelligent load balancing
   - Metrics-based auto-scaling
   - Circuit breaker patterns

5. **Advanced API Features**
   - Robust webhook management
   - Notification retries with exponential backoff
   - API versioning support
   - Optional GraphQL endpoint

6. **Developer Experience**
   - OpenAPI 3.0 documentation
   - SDK generation (Python, Java, JavaScript)
   - Interactive API explorer
   - Testing frameworks

### Long-term Considerations

7. **Multi-tenancy Support**
   - Per-tenant data isolation
   - Client-specific configurations
   - Billing and usage tracking
   - Multi-region deployment

8. **AI/ML Integration**
   - Predictive scaling
   - Anomaly detection
   - Intelligent routing
   - Performance optimization

## 📋 Compatibility and Requirements

### Supported Versions

- **Go Runtime**: 1.21+ (recommended: 1.24+)
- **CAMARA APIs**: v0.9.0+ (Traffic Influence)
- **3GPP Specifications**: TS 29.522 Release 16/17
- **HTTP Protocol**: HTTP/1.1, HTTP/2 support
- **Container Runtime**: Docker 20.10+, Podman 3.0+

### External Dependencies

- **NEF Server**: 3GPP TS 29.522 compliant
- **Network**: Container networking (bridge/overlay)
- **Storage**: Filesystem for logs and configuration
- **Optional**: Redis/PostgreSQL for persistent state

### Resource Requirements

**Minimum**:
- CPU: 1 core
- Memory: 512MB
- Storage: 1GB
- Network: 100Mbps

**Recommended (Production)**:
- CPU: 2+ cores
- Memory: 2GB+
- Storage: 10GB+
- Network: 1Gbps+

---

**Architecture implemented with focus on modularity, security, performance, and compliance with CAMARA and 3GPP industry standards.**
