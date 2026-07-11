# CTRAIL — Edge Application Management Backend Alignment

This document aligns the **CAMARA Edge Application Management (EAM)** API (the second CTRAIL
instance) with the available edge backends on the testbed: **OAI MEP** and **Kubernetes**.
It is the basis for the EAM implementation plan.

## 1. The core finding: OAI MEP ≠ application lifecycle

CAMARA EAM is an **application lifecycle management (LCM)** API: onboard app manifests,
deploy/instantiate them onto edge zones, update and terminate them, and discover the
clusters/zones where they run. In ETSI MEC terms this maps to **MEC 010-2** (MEAO / MEPM-V
LCM over the `Mm` reference points).

**OAI MEP does not implement LCM.** Its own README states it explicitly:

> "This repository is a **work in progress** — at the moment MEP only provides service
> discovery and registry for other MEC applications via the `mp1` interface."

OAI MEP exposes only the **ETSI MEC 011 service registry/discovery** (`Mp1`):

| OAI MEP endpoint | Method | Purpose |
|---|---|---|
| `/register` | POST | Register a MEC service in the registry |
| `/register/{serviceId}` | DELETE | Deregister a MEC service |
| `/discover` | GET | List all registered services |
| `/discover/{serviceType}` | GET | Filter services by type |

- Base URL (gateway): `http://oai-mep.org/service_registry/v1/`
- Base URL (direct): `http://172.29.248.1:8080/v1/`
- Swagger UI: `http://oai-mep.org/service_registry/v1/ui`
- Stack: Kong gateway + DaRS (Python) + MongoDB + Postgres, run via docker-compose.
- The OAI-MEC group contains only `oai-mep` (Mp1 registry) and `oai-rnis` (RNIS, MEC 012).
  There is **no** app-onboarding/instantiation component.

## 2. How MEC apps are actually deployed on the testbed

Per the free5GC integration guide (`free5gc.org/blog/20250625/20250625/`) and the Eurecom
"Lightweight edge Slice Orchestration Framework" paper, MEC applications on this testbed are
deployed as **Kubernetes / MicroK8s workloads via Helm charts**, isolated per
namespace (with vCPU/RAM/storage limits and network policies). Traffic steering toward the
deployed MEC app is then done through the **NEF Traffic Influence** path — i.e. the existing
C2N/CTRAIL instance.

So the testbed gives us two complementary backends:

- **Kubernetes API** → the actual orchestrator (deploy/update/terminate workloads, list
  clusters/namespaces). This is the LCM backend.
- **OAI MEP (`Mp1`)** → the MEC service registry. Once a deployment exposes a service, it is
  registered here so other MEC apps can discover it.
- **free5GC NEF** → already wired by C2N for traffic steering to the edge app (DNAI/DNN/S-NSSAI).

## 3. CAMARA EAM → backend mapping (Apps + Deployments + Clusters)

| CAMARA EAM endpoint | Semantics | Primary backend | Notes |
|---|---|---|---|
| `POST /apps` | onboard `AppManifest` | App catalog (store) | Manifest persisted (in-memory/CRD/ConfigMap). Not OAI MEP. |
| `GET /apps`, `GET /apps/{appId}` | list / get app | App catalog | |
| `DELETE /apps/{appId}` | delete app | App catalog | 409 if a deployment references it. |
| `POST /deployments` | deploy app to zones | **Kubernetes API** | Create Deployment(s) in namespace per target zone/cluster. |
| `GET /deployments`, `GET /deployments/{id}` | list / get deployment | **Kubernetes API** + state | Status from K8s; ID correlation in state module. |
| `PATCH /deployments/{id}` | update (RFC 7396 merge) | **Kubernetes API** | Re-target zones / rename → scale/update workloads. |
| `DELETE /deployments/{id}` | terminate | **Kubernetes API** | Delete Deployment(s) / namespace. |
| `GET /clusters` | list clusters | **Kubernetes API** / topology | Cluster info, optionally filtered by region/zone. |
| `GET /edge-cloud-zones` | list zones | topology config | **Already implemented** (static from `topology.json`). |
| *(value-add)* on deployment exposing a service | register MEC service | **OAI MEP `POST /register`** | Optional `Mp1` registration so the app is discoverable. |

## 4. Fit with the CTRAIL modular architecture (validates Claim 2)

The second instance reuses the API-agnostic modules and replaces only the two API-specific ones:

- **Reused as-is (agnostic):** `security`, `ingress` (OpenAPI validation against the EAM spec),
  `builder` (HTTP request construction — now toward K8s API server / OAI MEP), `state`
  (CAMARA `appId`/`appDeploymentId` ↔ backend K8s object correlation), `response`.
- **API-specific (new/extended):**
  - `topology` — extend zone resolution to also resolve **zone → cluster/namespace** (K8s context).
  - `mapper` — new translation: CAMARA `AppManifest` → K8s `Deployment`/`Service` spec, and
    deployment → OAI MEP `Service` (Mp1) registration object.
- A backend **client** analogous to `internal/nef/` is needed: `internal/k8s/` (client-go or
  REST) and a thin `internal/mep/` client for the OAI MEP registry.

## 5. Resolved decisions

1. **Scope**: Apps + Deployments + Clusters (+ existing edge-cloud-zones). Skip legacy
   `/appinstances` (superseded by `/deployments`).
2. **App catalog persistence**: **in-memory**, via the existing state-manager pattern (zero
   infra, matches the functional-test approach).
3. **Kubernetes access**: **raw REST via the existing `builder` module** (like the NEF client),
   not client-go — maximizes reuse of the agnostic modules and keeps dependencies minimal.
4. **OAI MEP registration**: included as an optional value-add behind a config flag.
5. **Deployment ↔ instance model**: `/deployments` only.

## 6. Downloaded references

- `docs/reference/oai-mep/swagger.yaml` — OAI MEP Mp1 service registry OpenAPI (MEC 011).
- `docs/reference/oai-mep/README.md` — OAI MEP architecture and deployment.
- free5GC + OAI-MEC integration guide: https://free5gc.org/blog/20250625/20250625/
- OAI MEP repo: https://gitlab.eurecom.fr/oai/orchestration/oai-mec/oai-mep
- OAI RNIS repo (MEC 012, `Mp2`): https://gitlab.eurecom.fr/oai/orchestration/oai-mec/oai-rnis
- Eurecom slice orchestration paper: https://www.eurecom.fr/publication/6811/download/comsys-publi-6811.pdf
- CAMARA EAM spec (local): `openapi/camara/edge_application_management.yaml`
