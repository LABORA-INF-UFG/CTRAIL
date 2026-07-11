# CTRAIL — ETSI MEC Compliance Mapping (Edge Application Management)

How the CTRAIL EAM instance stays compliant with **ETSI MEC patterns** across the relevant
reference points. The CAMARA EAM API is the northbound contract; the southbound must follow
the ETSI MEC behavioural patterns even when the concrete backend is Kubernetes + OAI MEP.

## 1. Reference points in scope

| Reference point | Between | ETSI GS | Local OpenAPI |
|---|---|---|---|
| **Mm1 / Mm3** | OSS↔MEO / MEO↔MEPM — app **package** mgmt + **lifecycle** mgmt | MEC 010-2 | `etsi-mec/gs010-2/MEC010-2_AppPkgMgmt.yaml`, `MEC010-2_AppLcm.yaml`, `MEC010-2_AppGrant.yaml` |
| **Mp1** | MEC platform ↔ MEC app — **service** registration / discovery | MEC 011 | `etsi-mec/gs011/MecServiceMgmtApi.yaml`, `MecAppSupportApi.yaml` |
| **Mp1** | MEC platform ↔ MEC app — radio info (RNIS) | MEC 012 | (OAI RNIS — out of EAM scope) |
| Architecture / reference-point definitions | — | MEC 003 | (prose spec, no OpenAPI) |

## 2. The MEC patterns CTRAIL must honour

These are the behavioural invariants that define "MEC-compliant", independent of backend:

1. **Package ≠ instance.** Onboarding an application descriptor (AppD / AppPkg) is a *separate
   resource and lifecycle* from instantiating it. MEC 010-2 splits `/app_packages`
   (`app_packagesPOST/GET`, `app_packageDELETE`) from `/app_instances`
   (`appInstancePOST/GET`). CAMARA mirrors this with `/apps` vs `/deployments`.
2. **Asynchronous lifecycle via operation occurrences.** `instantiate` / `terminate` /
   `operate` are tasks that return **`202 Accepted`** and are tracked through
   `/app_lcm_op_occs/{appLcmOpOccId}` (`PROCESSING → COMPLETED | FAILED | ROLLED_BACK`).
   CAMARA's `POST /deployments` and `DELETE` also return `202` — CTRAIL must back this with an
   op-occurrence-style tracked operation, not a fake-synchronous result.
3. **Explicit lifecycle states.** `instantiationState` (`NOT_INSTANTIATED` / `INSTANTIATED`)
   and `operationalState` (`STARTED` / `STOPPED`). CTRAIL must derive these from real backend
   (K8s Deployment) status, not invent them.
4. **Subscribe/notify for status change.** MEC uses `/subscriptions` +
   `AppInstanceStateChange` notifications. CAMARA exposes the same intent via the
   `onAppDeploymentStatusChange` callback. Map one to the other.
5. **Granting before instantiation (Mm3, MEC 010-2 AppGrant).** Resource grant is a distinct
   step. For the testbed we may auto-grant, but the pattern (capacity/zone admission) should be
   represented in the topology/state modules rather than skipped silently.
6. **Mp1 service registration is app-driven.** In MEC 011 a *running app instance* registers its
   service at `POST /applications/{appInstanceId}/services`. CTRAIL registers on behalf of the
   deployment after it is RUNNING — see §4 on the OAI MEP divergence.
7. **`ProblemDetails` (RFC 7807) errors** southbound vs. CAMARA `ErrorInfo` northbound — the
   `response` module must translate both error shapes.

## 3. CAMARA EAM → MEC 010-2 operation mapping

| CAMARA EAM | MEC 010-2 (Mm1/Mm3) operation | MEC pattern enforced |
|---|---|---|
| `POST /apps` (onboard `AppManifest`) | `app_packagesPOST` (onboard AppPkg/AppD) | package resource, onboardingState |
| `GET /apps`, `GET /apps/{id}` | `app_packagesGET`, `app_packageGET` | — |
| `DELETE /apps/{id}` | `app_packageDELETE` | 409 if referenced by an instance |
| `POST /deployments` | `appInstancePOST` + `appLcmInstanciatePOST` | **202 + op-occ**, instantiationState |
| `GET /deployments`, `GET /deployments/{id}` | `appInstanceGET`, `appInstanceIdGET` | instantiation/operational state from backend |
| `PATCH /deployments/{id}` | `appLcmOperatePOST` (+ re-instantiate scope) | merge-patch → operate/scale |
| `DELETE /deployments/{id}` | `appLcmTerminatePOST` + `appInstanceIdDELETE` | **202 + op-occ**, graceful terminate |
| `onAppDeploymentStatusChange` | `appLcmSubscriptionsPOST` + `AppInstanceStateChange` | subscribe/notify |
| `GET /clusters`, `GET /edge-cloud-zones` | (infra view — MEC 010-1 / MEAO) | static topology / K8s cluster info |

## 4. Mp1 / OAI MEP compliance note (important divergence)

The **canonical MEC 011** service-registration path is
`POST /applications/{appInstanceId}/services` (`AppServices_POST`), with discovery at
`GET /services`. **OAI MEP implements a simplified, non-standard surface** (`/register`,
`/discover`) — see `oai-mep/swagger.yaml`. Consequences:

- Against the **real testbed**, CTRAIL must speak OAI MEP's `/register` shape (our `internal/mep`
  client targets that).
- To claim **MEC 011 compliance**, CTRAIL's *internal* service-registration model should be the
  MEC 011 `ServiceInfo` schema, with an adapter that down-converts to OAI MEP's `Service` object.
  Document the gap explicitly in the paper rather than presenting OAI MEP as full MEC 011.

## 5. Compliance checklist (acceptance criteria for the implementation)

- [ ] `AppManifest` round-trips to a MEC 010-2 `AppD`/`AppPkg`-shaped internal model (package ≠ instance).
- [ ] `POST /deployments` returns `202` + `Location`, backed by a tracked op-occurrence record in `state`.
- [ ] Deployment status reports real `instantiationState`/`operationalState` derived from K8s.
- [ ] `DELETE /deployments/{id}` performs a terminate task (202), not an immediate hard delete.
- [ ] Status-change notifications follow the subscribe/notify pattern (CAMARA callback ↔ MEC notification).
- [ ] Southbound `ProblemDetails` ↔ northbound CAMARA `ErrorInfo` translation in `response`.
- [ ] Internal service-registration model = MEC 011 `ServiceInfo`; OAI MEP adapter is a documented down-conversion.
- [ ] Each mapping decision cites the MEC clause/operationId (traceability for the paper).

## 6. Downloaded specifications

- `etsi-mec/gs010-2/MEC010-2_AppLcm.yaml` — application **lifecycle** mgmt (instantiate/terminate/operate, op-occs, subscriptions).
- `etsi-mec/gs010-2/MEC010-2_AppPkgMgmt.yaml` — application **package** mgmt (onboarding AppD/AppPkg).
- `etsi-mec/gs010-2/MEC010-2_AppGrant.yaml` — application instance operation **granting** (Mm3).
- `etsi-mec/gs011/MecServiceMgmtApi.yaml` — Mp1 **service** mgmt (register/discover, subscriptions).
- `etsi-mec/gs011/MecAppSupportApi.yaml` — Mp1 **application support** (traffic rules, DNS, timing, confirm-ready).
- Source mirrors: https://github.com/etsi-forge/mec.gs010-2-app-pkg-lcm-api ·
  https://github.com/etsi-forge/mec.gs011-app-enablement-api
- ETSI Forge (canonical): https://forge.etsi.org/rep/mec
