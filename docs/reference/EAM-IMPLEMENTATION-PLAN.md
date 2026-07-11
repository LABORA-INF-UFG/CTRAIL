# CTRAIL — EAM Implementation Plan (Kubernetes as MEC Orchestrator)

Draft plan to implement the **CAMARA Edge Application Management** API as the second CTRAIL
instance, using **Kubernetes as the MEC orchestrator** (MEAO/MEPM role). It reuses the C2N
modular pipeline and honours the ETSI MEC patterns from `MEC-COMPLIANCE.md`.

Scope (locked): **Apps + Deployments + Clusters** (+ existing `edge-cloud-zones`); in-memory
catalog; K8s via the official **`client-go` SDK** (wrapped in `internal/k8s/`, playing the role
`internal/nef/` plays for C2N); OAI MEP registration behind a flag.

## 0. Concept: K8s in the MEC role

| ETSI MEC concept | Kubernetes realisation |
|---|---|
| MEAO / MEPM (LCM) | The K8s API server orchestrates workloads |
| App package / AppD (`/apps`) | In-memory `AppManifest` catalog (onboard-only metadata) |
| App instance / deployment (`/deployments`) | K8s `Namespace` + `Deployment` (+ `Service`) per target zone |
| App LCM op-occurrence | Tracked `DeploymentOp` record in `state` (202 → COMPLETED/FAILED) |
| instantiationState / operationalState | Derived from K8s Deployment `.status` (replicas ready) |
| Edge Cloud Zone → MEC host | `edgeCloudZoneId` → cluster/namespace (topology engine) |
| Mp1 service registration (MEC 011) | Optional `POST /register` to OAI MEP after RUNNING |

## 1. Codebase reality check (honest reuse)

The C2N "agnostic" modules are today **typed to Traffic Influence** (`builder` takes
`nef.TrafficInfluSub`, `state` stores `SubscriptionEntry`, `response` handles NEF responses).
So reuse here means **reuse the pattern/package with EAM-typed siblings**, not calling the exact
same functions. Two viable altitudes:

- **(A) Pragmatic siblings (recommended for first cut):** add EAM-typed types next to the TI
  ones in the same packages. Lowest risk, ships the second instance fast, still demonstrates
  the architecture. The genuine agnostic parts (`security`, `ingress` OpenAPI validator) are
  reused as-is.
- **(B) Generalise interfaces:** refactor `state`/`response` to backend-generic interfaces
  (`Store[T]`, `ResponseProcessor`) and retrofit TI onto them. Strengthens the paper's
  reusability claim with real code, but is a larger refactor touching C2N.

This plan implements **(A)** and notes the (B) seams so we can refactor later without rework.

**Backend-client boundary (revised for `client-go`):** the `builder` module builds *HTTP*
requests and is intrinsically NEF/HTTP-specific — the K8s backend does **not** flow through it.
Instead, `internal/k8s/` wraps the `client-go` typed clientset, exactly as `internal/nef/`
wraps `net/http`. So the reused-agnostic set for EAM is **`security`, `ingress`, `state`,
`response`**; the API-specific set is the **backend client (`k8s`), `mapper`, `topology`**.
Update the README/paper module counts to reflect this (it sharpens Claim 2 — API-specificity
is isolated to backend-client + mapper + topology).

## 2. New packages and files

```
internal/
├── camara/
│   └── edge_application.go       AppManifest, SubmittedApp, AppDeploymentInfo,
│                                 AppDeploymentReq, AppDeploymentPatch, ClusterInfo
├── k8s/                          NEW backend client (wraps client-go; mirrors internal/nef/)
│   └── client.go                 clientset wrapper: Apply/Get/Patch/Delete/List
└── modules/
    ├── mapper/
    │   └── app_mapper.go         AppManifest → k8s.Deployment/Service; AppMapper interface
    ├── state/
    │   └── eam_store.go          in-memory AppCatalog + DeploymentStore + op-occ tracking
    ├── topology/
    │   └── engine.go (extend)    ResolveCluster(zoneID) → cluster/namespace
    └── eam_orchestrator.go       EAM Gin handlers + route registration
```

Backend client shape (wraps `client-go`; mirrors `internal/nef/client.go`). It hides the
clientset behind a small interface so the orchestrator/mapper never import `k8s.io/*` directly
and so tests can inject the fake clientset:

```go
// internal/k8s/client.go
import (
    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/client-go/kubernetes"
)

type Client interface {
    ApplyNamespace(ctx context.Context, name string, labels map[string]string) error
    ApplyDeployment(ctx context.Context, ns string, d *appsv1.Deployment) (*appsv1.Deployment, error)
    GetDeployment(ctx context.Context, ns, name string) (*appsv1.Deployment, error)
    PatchDeployment(ctx context.Context, ns, name string, mergePatch []byte) (*appsv1.Deployment, error)
    ApplyService(ctx context.Context, ns string, s *corev1.Service) (*corev1.Service, error)
    DeleteNamespace(ctx context.Context, name string) error
    ListNamespaces(ctx context.Context, labelSelector string) ([]corev1.Namespace, error)
}

// NewClient builds a clientset from in-cluster config or a kubeconfig path.
func NewClient(kubeconfig string) (Client, error)      // wraps kubernetes.Interface
// NewClientFromInterface lets tests pass fake.NewSimpleClientset().
func NewClientFromInterface(cs kubernetes.Interface) Client
```

## 3. Kubernetes access via `client-go`

- Auth/connection handled by `client-go`: **in-cluster** config (ServiceAccount) when running
  in the cluster, else a `KUBECONFIG` path. No manual bearer/TLS handling.
- Operations map to typed clientset calls (`AppsV1().Deployments(ns).Apply/Get/Patch`,
  `CoreV1().Namespaces().Apply/Delete/List`, `CoreV1().Services(ns).Apply`).

| EAM action | client-go call |
|---|---|
| Ensure zone namespace | `CoreV1().Namespaces().Apply` (label `ctrail.io/edge-zone`) |
| Create/replace deployment | `AppsV1().Deployments(ns).Apply` (server-side apply) |
| Get deployment status | `AppsV1().Deployments(ns).Get` → `.Status.ReadyReplicas` |
| Patch deployment | `AppsV1().Deployments(ns).Patch(types.MergePatchType, …)` |
| Delete deployment/namespace | `AppsV1().Deployments(ns).Delete` / `Namespaces().Delete` |
| Expose service | `CoreV1().Services(ns).Apply` |
| List clusters | `CoreV1().Namespaces().List(labelSelector=ctrail.io/edge-zone)` |

## 4. AppManifest → Kubernetes object mapping (the new "Table 1" for EAM)

| CAMARA `AppManifest` field | Kubernetes target | Phase |
|---|---|---|
| `name`, `appId` | `Deployment.metadata.name` + labels `ctrail.io/app-id` | Direct |
| `appRepo.imagePath` | `PodSpec.containers[].image` | Direct |
| `appRepo.credentials`/`authType` | `imagePullSecrets` (+ Secret) | Structural |
| `requiredResources` (CPU/mem/storage) | `resources.requests`/`limits` | Logical |
| `componentSpec[].componentName` | `containers[].name` | Direct |
| `componentSpec[].networkInterfaces[].port`/`protocol` | `containers[].ports` + `Service.ports` | Structural |
| `networkInterfaces[].visibilityType` (external/internal) | `Service.type` (LoadBalancer/ClusterIP) | Logical |
| deployment `edgeCloudZones[]` | target namespace/cluster (topology) | Logical |

Reuses C2N's **three-phase mapping** framing (direct / logical+topology / structural) — good for
the paper's Claim 1 symmetry.

## 5. Async lifecycle & state (MEC op-occurrence pattern)

`state/eam_store.go` adds three in-memory, mutex-guarded stores mirroring `InMemoryStateManager`:

- **AppCatalog**: `appId → AppManifest` (onboard/list/get/delete; 409 on delete if referenced).
- **DeploymentStore**: `appDeploymentId → DeploymentEntry{ camaraReq, k8sRefs[], state, opOcc }`
  with reverse index `k8s namespace/name → appDeploymentId` (the CAMARA↔backend correlation
  that C2N does for NEF IDs).
- **op-occurrence**: `POST /deployments` returns **202 + Location** immediately, records
  `PROCESSING`; a goroutine (or synchronous-then-poll for the mock) applies K8s objects and
  transitions `COMPLETED`/`FAILED`. `GET /deployments/{id}` derives
  `instantiationState`/`operationalState` from live K8s `.status.readyReplicas`.

## 6. Orchestrator & routes

`eam_orchestrator.go` holds `appMapper`, `k8sClient`, `eamStore`, `topologyEngine`, `mepClient`
and registers under both `SetupRoutes` and `SetupSecureRoutes` (scopes
`edge-application-management:apps:write` etc.):

```
POST/GET   /apps                     submitApp / getApps
GET/DELETE /apps/{appId}             getApp / deleteApp
POST/GET   /deployments              createAppDeployment (202) / getAppDeployments
GET/PATCH/DELETE /deployments/{id}   getAppDeployment / update (merge-patch) / delete (202)
GET        /clusters                 getClusters (filter region/zone/clusterRef)
GET        /edge-cloud-zones         (existing)
```

Wire a second constructor `NewEAMOrchestrator(cfg)` or extend `NewGatewayOrchestrator` to also
build the EAM deps; keep C2N wiring untouched.

## 7. Config additions (`internal/config/config.go` + `.env.example`)

`KUBECONFIG` (empty ⇒ in-cluster config), `K8S_NAMESPACE_PREFIX` (default `ctrail-`),
`MEP_ENABLED` (default false), `MEP_BASE_URL` (`http://oai-mep.org/service_registry/v1`).
Auth/TLS are delegated to `client-go` — no token/CA env vars needed.

**Go module deps** (`go.mod`): add `k8s.io/client-go`, `k8s.io/api`, `k8s.io/apimachinery`
(pin to a release matching the testbed cluster's minor version). This is the only heavyweight
addition; run `go mod tidy` and commit the updated `go.sum`.

## 8. Testing (fake clientset — the client-go analogue of the mock NEF)

`tests/functional/`:
- `apps_test.go` — onboard/list/get/delete lifecycle, 409-on-referenced-delete.
- `deployments_test.go` — create (202 + Location), status transitions, PATCH (zone replace),
  delete (202); asserts the **typed K8s objects** created (namespace + deployment + service)
  by reading them back from the fake clientset.
- `clusters_test.go` — list + region/zone filters.
- Backend double = `k8s.io/client-go/kubernetes/fake.NewSimpleClientset()` injected via
  `k8s.NewClientFromInterface(...)`. Zero infra, typed, official — strictly better than the
  hand-rolled httptest JSON the mock NEF uses. To simulate a rollout completing, seed the fake
  with a Deployment whose `.status.readyReplicas` is set (or use a reactor).

## 9. Phased build order (each phase ends green: `go build ./... && go test ./tests/functional/...`)

1. **Models** — `camara/edge_application.go` (K8s object types come from `k8s.io/api`, no local models).
2. **K8s client + deps** — add `client-go`/`api`/`apimachinery` to `go.mod`; `k8s/client.go`
   wrapper + `NewClientFromInterface`; unit-test apply/get/delete against `fake` clientset.
3. **Mapper** — `app_mapper.go` (AppManifest → Deployment/Service) + topology `ResolveCluster`.
4. **State** — `eam_store.go` (catalog, deployment store, op-occ).
5. **Apps endpoints** — handlers + routes + `apps_test.go`. ✅ Compliance: package≠instance.
6. **Deployments endpoints** — 202/Location, status derivation, PATCH, delete + `deployments_test.go`.
   ✅ Compliance: async op-occ, real lifecycle states.
7. **Clusters endpoint** — `getClusters` + `clusters_test.go`.
8. **OAI MEP registration** — `internal/mep/` client, called post-RUNNING behind `MEP_ENABLED`.
9. **Docs** — README endpoint table, ARCHITECTURE.md, and tie each handler to its
   `MEC-COMPLIANCE.md` §5 criterion + MEC operationId.

## 10. MEC-compliance acceptance (from `MEC-COMPLIANCE.md` §5)

Each phase must satisfy its criterion: package≠instance (5), 202+op-occ (6), real
instantiation/operational state (6), terminate-as-task (6), `ProblemDetails`↔`ErrorInfo` in
`response` (5/6), MEC 011 `ServiceInfo` internal model with OAI MEP down-conversion (8),
operationId citations for traceability (9).

## 11. Risks / open items

- **Real testbed K8s auth/RBAC** — needs a ServiceAccount token with deployment/namespace rights;
  until then everything runs against the mock (zero-infra, like C2N tests).
- **Sync vs async** — first cut may apply K8s synchronously then still return 202 + a COMPLETED
  op-occ; a background reconciler is a later enhancement.
- **PATCH semantics** — CAMARA merge-patch replaces arrays (zones) wholesale; deletion of
  dropped-zone namespaces must be handled.
- **Altitude (A) vs (B)** — if the paper leans hard on code-level reusability, budget for the
  (B) interface refactor after the instance works.
