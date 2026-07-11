# CTRAIL: CAMARA Transformation and Routing API Integration Layer

**CTRAIL** is an open-source modular gateway that implements the CAMARA Transformation
Function, translating CAMARA Service API calls into backend-specific interfaces across
three ecosystem domains: **network** (3GPP NEF), **cloud/edge** (MEC platforms), and
**management** (OAM — future work). It bridges the abstraction gap between
developer-friendly CAMARA Service APIs and the telecommunications-specific interfaces
required by the 5G Core.

### Implemented Endpoints

| Endpoint | CAMARA API | Backend | Domain |
|---|---|---|---|
| `POST /traffic-influences` | Traffic Influence | 3GPP NEF (TS 29.522) | Network |
| `GET /traffic-influences` | Traffic Influence | 3GPP NEF (TS 29.522) | Network |
| `GET /traffic-influences/:id` | Traffic Influence | 3GPP NEF (TS 29.522) | Network |
| `PUT /traffic-influences/:id` | Traffic Influence | 3GPP NEF (TS 29.522) | Network |
| `DELETE /traffic-influences/:id` | Traffic Influence | 3GPP NEF (TS 29.522) | Network |
| `POST /apps`, `GET /apps` | Edge Application Management | In-memory app catalog | Cloud/Edge |
| `GET /apps/:appId`, `DELETE /apps/:appId` | Edge Application Management | In-memory app catalog | Cloud/Edge |
| `POST /deployments`, `GET /deployments` | Edge Application Management | Kubernetes (client-go) | Cloud/Edge |
| `GET /deployments/:id`, `PATCH /deployments/:id`, `DELETE /deployments/:id` | Edge Application Management | Kubernetes (client-go) | Cloud/Edge |
| `GET /clusters` | Edge Application Management | Kubernetes / topology | Cloud/Edge |
| `GET /edge-cloud-zones` | Edge Application Management | Topology config | Cloud/Edge |

The **Traffic Influence** implementation is the first CTRAIL instance and accompanies the
paper **"C2N: Bridging CAMARA Service APIs and 3GPP Core Network Exposure Function"**
accepted at SBRC 2026.

The **Edge Application Management** implementation is the second CTRAIL instance. It uses
**Kubernetes as the MEC orchestrator** (the MEAO/MEPM role) for the application lifecycle,
and optionally registers exposed services in the **OAI MEP** `mp1` registry (ETSI MEC 011).
It reuses the API-agnostic modules (security, ingress, state, response) and replaces only the
backend client, mapper, and topology cluster resolution — validating the reusability claim
against a structurally different backend. Design and compliance notes are in
[`docs/reference/`](docs/reference/) (`EAM-BACKEND-ALIGNMENT.md`, `MEC-COMPLIANCE.md`,
`EAM-IMPLEMENTATION-PLAN.md`). To enable it, set `EAM_ENABLED=true` and provide a `KUBECONFIG`
(or run in-cluster).

---

## README Structure

- [Basic Information](#basic-information)
- [Dependencies](#dependencies)
- [Security Concerns](#security-concerns)
- [Installation](#installation)
- [Minimal Test](#minimal-test)
- [Experiments](#experiments)
  - [Claim 1 — Three-Phase Semantic Mapping](#claim-1--three-phase-semantic-mapping)
  - [Claim 2 — Modular Architecture and API-Agnostic Reusability](#claim-2--modular-architecture-and-api-agnostic-reusability)
- [LICENSE](#license)

---

## Basic Information

### Execution Environment

| Component | Specification |
|-----------|---------------|
| OS | Linux (Ubuntu 22.04 LTS recommended) |
| Architecture | x86\_64 |
| CPU | 2+ cores |
| RAM | 2 GB minimum |
| Disk | 2 GB (source + Docker image) |

### Repository Structure

```
CTRAIL/
├── cmd/server/main.go              # Application entry point
├── internal/
│   ├── camara/                     # CAMARA API data models
│   │   ├── models.go               # Traffic Influence models
│   │   ├── edge_cloud_zones.go     # Edge cloud zone models
│   │   └── edge_application.go     # Edge Application Management models (Apps/Deployments/Clusters)
│   ├── nef/                        # 3GPP NEF data models and HTTP client [Traffic Influence backend]
│   ├── k8s/                        # Kubernetes backend client (client-go) [EAM MEC orchestrator]
│   ├── mep/                        # OAI MEP mp1 service-registry client (ETSI MEC 011) [optional]
│   ├── config/                     # Configuration loader
│   └── modules/
│       ├── orchestrator.go         # Traffic Influence pipeline + route setup
│       ├── eam_orchestrator.go     # Edge Application Management: Apps handlers + wiring
│       ├── eam_deployments.go      # Deployment lifecycle handlers (202 + op-occurrence)
│       ├── eam_clusters.go         # Clusters endpoint
│       ├── security/               # Authentication, rate limiting, CORS [API-agnostic]
│       ├── ingress/                # Request parsing and OpenAPI validation [API-agnostic]
│       ├── topology/               # Zone → network (TI) and zone → cluster (EAM) resolution [API-specific]
│       ├── mapper/                 # CAMARA→NEF and CAMARA→Kubernetes translation [API-specific]
│       ├── builder/                # NEF HTTP request construction [Traffic Influence]
│       ├── state/                  # Lifecycle + ID correlation (TI subscriptions, EAM catalog/deployments) [API-agnostic]
│       └── response/               # Response transformation to CAMARA format [API-agnostic]
├── config/topology.json            # Zone-to-network topology mappings
├── openapi/                        # CAMARA and 3GPP OpenAPI specifications
│   ├── camara/traffic_influence.yaml
│   ├── camara/edge_application_management.yaml
│   └── 3gpp/TS29522_TrafficInfluence.yaml
├── tests/functional/               # Functional tests with mock NEF
│   ├── traffic_influence_test.go   # 15 tests — full CRUD lifecycle
│   └── edge_cloud_zones_test.go    # 13 tests — zone discovery and filtering
├── Dockerfile                      # Production multi-stage container build
├── Dockerfile.test                 # Test runner container
├── docker-compose.yml              # Deployment with free5GC network
├── docker-compose.test.yml         # Isolated test runner
├── .env.example                    # Environment variable template
├── Makefile                        # Build and run shortcuts
├── ARCHITECTURE.md                 # Detailed module documentation
└── DOCKER.md                       # Docker deployment guide
```

---

## Dependencies

### Runtime Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| Go | 1.24.2+ | Build and run from source |
| Docker | 24.0+ | Containerized execution |
| Docker Compose | v2.x | Container orchestration |

### Go Module Dependencies

| Module | Version | Purpose |
|--------|---------|---------|
| `github.com/gin-gonic/gin` | v1.10.0 | HTTP routing framework |
| `github.com/golang-jwt/jwt/v5` | v5.2.3 | JWT token verification |
| `github.com/google/uuid` | v1.6.0 | Correlation ID generation |

All Go dependencies are declared in `go.mod` and downloaded automatically via `go mod download`.

---

## Security Concerns

CTRAIL implements HTTP Basic Auth and Bearer Token authentication. The test credentials
used in the functional test suite (`admin` / `adminpassword123`) are hardcoded for
testing only and must not be used in any production or real network deployment.

**Risk to reviewers**: None. The artifact runs in an isolated environment.

- Functional tests use an in-process mock NEF server; no external connections are made.
- Docker deployment creates an isolated network and makes no outbound connections unless
  `NEF_BASE_URL` is configured to point to a real NEF endpoint.
- No privileged containers or host network access is required for the functional tests.

---

## Installation

### Option A: Run Tests with Docker (No NEF required)

This option uses a dedicated test compose file (`docker-compose.test.yml`) that
**automatically runs the full functional test suite** on startup and exits. Note: the
default `docker-compose.yml` starts the gateway server — it does **not** run tests.

```bash
# 1. Clone the repository
git clone https://github.com/LABORA-INF-UFG/CTRAIL.git
cd CTRAIL

# 2. Build and run the test container — tests execute automatically
docker compose -f docker-compose.test.yml up --build
```

Expected output (the container runs `go test ./tests/functional/... -v` and exits):

```
ctrail-test-runner  | === RUN   TestCreateTrafficInfluence_Success
ctrail-test-runner  | --- PASS: TestCreateTrafficInfluence_Success (0.00s)
ctrail-test-runner  | === RUN   TestCreateTrafficInfluence_InvalidJSON
ctrail-test-runner  | 2026/05/07 14:30:36 FAILED_AUTH: map[event:authentication_failed ...]
ctrail-test-runner  | --- PASS: TestCreateTrafficInfluence_InvalidJSON (0.00s)
ctrail-test-runner  | ...
ctrail-test-runner  | === RUN   TestListEdgeCloudZones_ReturnsAllZones
ctrail-test-runner  | --- PASS: TestListEdgeCloudZones_ReturnsAllZones (0.00s)
ctrail-test-runner  | ...
ctrail-test-runner  | === RUN   TestTrafficInfluence_FullCRUDLifecycle
ctrail-test-runner  | --- PASS: TestTrafficInfluence_FullCRUDLifecycle (0.00s)
ctrail-test-runner  | === RUN   TestHealthCheck
ctrail-test-runner  | --- PASS: TestHealthCheck (0.00s)
ctrail-test-runner  | PASS
ctrail-test-runner  | ok    github.com/LABORA-INF-UFG/ctrail/tests/functional
ctrail-test-runner exited with code 0
```

> The `FAILED_AUTH` log in `TestCreateTrafficInfluence_InvalidJSON` is **expected** — it
> is part of the security module test that verifies authentication failure is correctly
> rejected.

A final exit code of `0` means all tests passed. Any non-zero exit code indicates a failure.

### Option B: Run Tests Locally (Go required)

```bash
# 1. Clone the repository
git clone https://github.com/LABORA-INF-UFG/CTRAIL.git
cd CTRAIL

# 2. Install Go 1.24.2+ if needed
#    https://go.dev/doc/install

# 3. Download dependencies
go mod download

# 4. Run tests
go test ./tests/functional/... -v
```

### Option C: Run the Gateway with Docker

```bash
# 1. Clone and enter the repository
git clone https://github.com/LABORA-INF-UFG/CTRAIL.git
cd CTRAIL

# 2. Configure environment
cp .env.example .env
# Edit .env: set NEF_BASE_URL and NEF_TOKEN for your NEF instance

# 3. Build and start
docker compose up --build -d

# 4. Verify
curl http://localhost:8080/health
```

---

## Minimal Test

This test verifies that the gateway is functional using the embedded mock NEF. It does
not require a real 5G Core or NEF instance.

```bash
go test ./tests/functional/... -v -run TestTrafficInfluence_FullCRUDLifecycle
```

Expected output (internal server logs omitted for brevity):

```
=== RUN   TestTrafficInfluence_FullCRUDLifecycle
    traffic_influence_test.go: Created subscription: <uuid>
    traffic_influence_test.go: Read subscription successfully
    traffic_influence_test.go: Updated subscription successfully
    traffic_influence_test.go: Listed subscriptions successfully
    traffic_influence_test.go: Deleted subscription successfully
    traffic_influence_test.go: Verified subscription deletion
    traffic_influence_test.go: Full CRUD lifecycle test completed successfully
--- PASS: TestTrafficInfluence_FullCRUDLifecycle (0.00s)
PASS
ok  	github.com/LABORA-INF-UFG/ctrail/tests/functional
```

This test exercises the complete CAMARA→NEF translation pipeline (all 7 modules) using
a mock NEF that validates the translated 3GPP TS 29.522 request format.

---

## Experiments

### Claim 1 — Three-Phase Semantic Mapping

**Paper reference**: Section 4, Table 1 (CAMARA to NEF Field Mappings).

**Claim**: CTRAIL translates CAMARA Traffic Influence requests into 3GPP NEF format
through three transformation phases — direct mapping, logical transformation with
topology enrichment, and structural mapping — bridging the 205-byte CAMARA request to
the 402-byte 3GPP NEF format.

**Requirements**: Go 1.24.2+ or Docker. No real 5G infrastructure needed.

**Steps**:

```bash
# Run the full test suite to validate all three mapping phases
go test ./tests/functional/... -v
```

**Identifying each phase in the code**:

| Phase | Description | Location |
|-------|-------------|----------|
| Direct | `appId` → `afServiceId`, `notificationUri` → `notifDestination` | `internal/modules/mapper/parameter_mapper.go` |
| Logical | `edgeCloudZoneId` → `dnai` + `dnn` + `snssai` (via topology lookup) | `internal/modules/topology/engine.go` + `config/topology.json` |
| Structural | `device` + `trafficFilters` → `FlowInfo[]` with IP 5-tuple format | `internal/modules/mapper/parameter_mapper.go` |

**Expected result**: All tests pass. Each `TestCreate*` test verifies that CTRAIL
produces a well-formed NEF subscription from a CAMARA request.

**Estimated time**: < 30 seconds.

---

### Claim 2 — Modular Architecture and API-Agnostic Reusability

**Paper reference**: Section 4, Figure 1, and the architectural discussion (~70% reusability).

**Claim**: Five of seven modules (Security, Ingress, Request Builder, State Manager,
Response Module) are API-agnostic and reusable when implementing additional CAMARA APIs.
Only the Topology Engine and Parameter Mapper are API-specific. The `GET /edge-cloud-zones`
endpoint validates this claim: it reuses all five agnostic modules with no modifications.

**Requirements**: Go 1.24.2+ or Docker. No real 5G infrastructure needed.

**Steps**:

```bash
# Run all functional tests to exercise the full 7-module pipeline
go test ./tests/functional/... -v

# Inspect the module structure
ls internal/modules/
```

**Module classification**:

```
internal/modules/
├── security/       [API-agnostic] Authentication, RBAC, rate limiting, audit logging
├── ingress/        [API-agnostic] HTTP parsing, OpenAPI schema validation
├── topology/       [API-specific] Maps edgeCloudZoneId → DNAI/DNN/S-NSSAI
├── mapper/         [API-specific] Translates CAMARA fields to NEF fields (Table 1)
├── builder/        [API-agnostic] Constructs NEF HTTP requests, connection pooling
├── state/          [API-agnostic] Bidirectional CAMARA↔NEF ID mapping, lifecycle
└── response/       [API-agnostic] Transforms NEF responses to CAMARA format
```

The `orchestrator.go` wires the seven modules into the four-layer pipeline
(Security → Ingress → Topology/Mapper → Builder/State → Response).

**Expected result**: All 28 tests pass (15 Traffic Influence + 13 Edge Cloud Zones),
confirming the complete pipeline is functional across both implemented endpoints.
The 5 API-agnostic modules handle all cross-cutting concerns transparently behind a mock
backend, validating the reusability claim.

**Estimated time**: < 30 seconds.

---

## LICENSE

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
