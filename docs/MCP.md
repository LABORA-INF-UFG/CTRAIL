# CTRAIL MCP integration

The MCP adapter lets agents operate CTRAIL's implemented CAMARA-facing APIs using
typed tools. It is a separate Go executable, `cmd/mcp`, using the
[official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk). The adapter
supports stdio and Streamable HTTP. It does not need NEF credentials, kubeconfigs,
or a second copy of the gateway's state.

## Architecture and API coverage

```text
Agent / MCP client
    | stdio or authenticated Streamable HTTP
    v
CTRAIL MCP adapter (typed tools, read-only option, bounded HTTP client)
    | existing CTRAIL HTTP API + operator-configured gateway credentials
    v
CTRAIL security -> ingress -> topology / mapping -> orchestration / state
    |                          |
    v                          v
3GPP NEF Traffic Influence     Kubernetes / optional OAI MEP
```

CTRAIL implements two connected gateway instances: Traffic Influence translates
CAMARA intent into 3GPP TS 29.522 requests, and Edge Application Management maps
app onboarding and lifecycle requests to Kubernetes. Gateway versions with a
shared endpoint registry connect app placement to network steering; earlier
versions use configured static topology. The adapter uses these
existing public routes, so authentication, permissions, mapping, lifecycle, and
backend errors remain the gateway's responsibility.

The implementation follows the checked-out route definitions in
`internal/modules/orchestrator.go` and `internal/modules/eam_orchestrator.go`,
and the request models in `internal/camara`. This matters because the local
development checkout includes optional placement-policy extensions and only a
subset of some checked-in upstream specifications. The adapter also builds on
the committed gateway baseline without requiring those extensions. A specification alone does not establish
that a route is implemented.

The adapter exposes no arbitrary HTTP request tool, administrative endpoints,
incoming NEF callbacks, direct backend clients, OAM operations, or unimplemented
Application Endpoint Registration routes. Adding an API requires explicit tool
registration and contract tests in `internal/mcpserver`.

## Build and connect through stdio

Requires Go 1.25+. The SDK requires this minimum; the gateway and test containers
and CI now use the same minimum Go version.

```sh
go build -o bin/ctrail-mcp ./cmd/mcp
export CTRAIL_BASE_URL=http://localhost:8080
export CTRAIL_USERNAME='<gateway-username>'
export CTRAIL_PASSWORD='<gateway-password>'
./bin/ctrail-mcp
```

For PowerShell:

```powershell
go build -o bin/ctrail-mcp.exe ./cmd/mcp
$env:CTRAIL_BASE_URL = 'http://localhost:8080'
$env:CTRAIL_USERNAME = '<gateway-username>'
$env:CTRAIL_PASSWORD = '<gateway-password>'
.\bin\ctrail-mcp.exe
```

Start CTRAIL independently with its normal configuration. Configure the MCP
client to launch the adapter executable; stdout is reserved for protocol messages
and diagnostics use stderr. The adapter does not load `.env` automatically.

Use [examples/mcp/stdio.json](../examples/mcp/stdio.json) as a client configuration
template. Replace the command with an absolute executable path (use escaped
backslashes and `.exe` on Windows). Replace the token placeholder with a valid
gateway-issued token, or remove `CTRAIL_BEARER_TOKEN` and supply Basic credentials
instead. Client configuration keys may vary by host. Supply credentials through
your client's secret configuration and do not commit real credentials.

No credentials are built in. Without credentials, public health works and
protected routes return the gateway's normal authentication errors. Gateway
authentication retains its current implementation and deployment limitations;
the adapter does not introduce an identity provider or change gateway security.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `CTRAIL_BASE_URL` | `http://localhost:8080` | Gateway root; an optional reverse-proxy path prefix is preserved |
| `CTRAIL_BEARER_TOKEN` | unset | Gateway Bearer token; mutually exclusive with Basic credentials |
| `CTRAIL_USERNAME`, `CTRAIL_PASSWORD` | unset | Gateway Basic authentication; set both |
| `CTRAIL_MCP_TIMEOUT` | `30s` | Positive timeout for each gateway request, e.g. `1m` |
| `CTRAIL_MCP_READ_ONLY` | `false` | If true, register only the nine read tools |
| `CTRAIL_MCP_TRANSPORT` | `stdio` | `stdio` or `http` |
| `CTRAIL_MCP_LISTEN_ADDR` | `127.0.0.1:8081` | HTTP listen address |
| `CTRAIL_MCP_HTTP_TOKEN` | unset | Required for HTTP; authenticates MCP clients separately from gateway credentials |

`NEF_TOKEN` and NRF OAuth settings belong to the gateway, not the adapter.
All agent requests use the configured gateway identity. HTTP mode is intended
for clients sharing that operator identity; it does not provide per-user token
delegation or MCP OAuth discovery. Use separate adapter instances and gateway
identities when clients need different privileges.

## Streamable HTTP

```sh
export CTRAIL_BASE_URL=http://localhost:8080
export CTRAIL_BEARER_TOKEN='<gateway-issued-token>'
export CTRAIL_MCP_TRANSPORT=http
export CTRAIL_MCP_HTTP_TOKEN='<dedicated-random-adapter-token>'
./bin/ctrail-mcp
```

Connect to `http://127.0.0.1:8081/mcp` with
`Authorization: Bearer <dedicated-random-adapter-token>`. The HTTP transport
requires a token even on loopback. The MCP token is never sent to CTRAIL.
[examples/mcp/http.json](../examples/mcp/http.json) shows the client shape.

The SDK handles protocol negotiation and stateless Streamable HTTP, including
legacy MCP initialization. Requests with an Origin header must use the same
host as the endpoint. SDK localhost protection remains enabled. For remote
hosting, terminate TLS at a trusted reverse proxy, preserve the public Host
header, restrict direct backend access, and set `CTRAIL_MCP_LISTEN_ADDR` explicitly.
When forwarding a public Host, connect the proxy to a non-loopback adapter address
(for example, its address on a private container network). SDK localhost protection
rejects public Host headers on connections arriving over loopback.
Use HTTPS for gateway credentials sent over a network as well.

## Tools

Every input has a JSON schema. `request`, `manifest`, and `patch` contain the
corresponding CAMARA-facing body with its original camelCase field names.
Write tools carry mutation annotations; read-only mode removes them entirely.

| Tool | CTRAIL route | Arguments |
|---|---|---|
| `ctrail_get_health` | `GET /health` | none |
| `ctrail_list_edge_cloud_zones` | `GET /edge-cloud-zones` | optional `region`, `status` |
| `ctrail_list_clusters` | `GET /clusters` | optional `region`, `clusterRef`, `edgeCloudZoneId` |
| `ctrail_list_traffic_influences` | `GET /traffic-influences` | optional `appId`, `edgeCloudZoneId`, `state` |
| `ctrail_get_traffic_influence` | `GET /traffic-influences/{id}` | `trafficInfluenceId` |
| `ctrail_create_traffic_influence` | `POST /traffic-influences` | `request` |
| `ctrail_update_traffic_influence` | `PUT /traffic-influences/{id}` | `trafficInfluenceId`, complete `request` |
| `ctrail_delete_traffic_influence` | `DELETE /traffic-influences/{id}` | `trafficInfluenceId` |
| `ctrail_list_apps` | `GET /apps` | none |
| `ctrail_get_app` | `GET /apps/{appId}` | `appId` |
| `ctrail_submit_app` | `POST /apps` | `manifest` |
| `ctrail_delete_app` | `DELETE /apps/{appId}` | `appId` |
| `ctrail_list_deployments` | `GET /deployments` | optional `appId`, `appDeploymentId` |
| `ctrail_get_deployment` | `GET /deployments/{id}` | `appDeploymentId` |
| `ctrail_create_deployment` | `POST /deployments` | `request` |
| `ctrail_update_deployment` | `PATCH /deployments/{id}` | `appDeploymentId`, `patch` |
| `ctrail_delete_deployment` | `DELETE /deployments/{id}` | `appDeploymentId` |

Application, deployment, and cluster routes require `EAM_ENABLED=true` and working
cluster configuration on the gateway. These tools remain discoverable when EAM
is disabled and return the upstream 404; zone discovery is independent of EAM.

## Deployment-to-steering example

1. Call `ctrail_list_edge_cloud_zones` and `ctrail_list_clusters`; select actual
   IDs returned by the gateway.
2. Call `ctrail_submit_app` with an application manifest. For example:

   ```json
   {
     "manifest": {
       "name": "edge-web",
       "appProvider": "example-provider",
       "version": "1.0.0",
       "packageType": "CONTAINER",
       "appRepo": {"type": "PUBLICREPO", "imagePath": "nginx:stable"},
       "requiredResources": {"infraKind": "kubernetes", "numCPU": "500m", "memory": 128},
       "componentSpec": [{
         "componentName": "web",
         "networkInterfaces": [{
           "interfaceId": "http", "protocol": "TCP", "port": 80,
           "visibilityType": "VISIBILITY_EXTERNAL"
         }]
       }],
       "placementPreference": "edge"
     }
   }
   ```

3. Use the returned app ID to call `ctrail_create_deployment`:

   ```json
   {"request":{"appDeploymentName":"edge-web","appId":"<returned-app-id>","edgeCloudZones":["<discovered-zone-id>"]}}
   ```

   Use explicit `edgeCloudZones` with the committed gateway baseline.
   On gateway versions with placement policy, omit both `edgeCloudZones` and
   `kubernetesClusterRefs` for automatic placement; explicit zones take precedence.
   The manifest's optional `placementPreference` is honored only by those versions.

4. Call `ctrail_get_deployment` with the returned `appDeploymentId`. HTTP 202 means
   accepted. The current public API does not expose endpoint readiness; verify
   service readiness with the operator before relying on app-aware steering.
5. Call `ctrail_create_traffic_influence` for that same app and zone, using the
   actual device address appropriate to your test network:

   ```json
   {
     "request": {
       "appId": "<returned-app-id>",
       "edgeCloudZoneId": "<discovered-zone-id>",
       "device": {"ipv4Address": {"publicAddress": "<device-ipv4-address>"}},
       "destinationTrafficFilters": {"destinationPort": 80, "destinationProtocol": "TCP"}
     }
   }
   ```

   Gateways with the endpoint registry extension resolve the deployed endpoint
   if available, falling back to static topology otherwise. The committed baseline
   uses static topology, which must match the service endpoint. Control-plane
   success does not prove data-plane installation or latency. Migration and active
   re-steering require gateway versions implementing those backend features.
6. To clean up, delete the traffic influence, delete the deployment, and then
   delete the app. The gateway rejects deletion of an app still in use.

Agents can also read `ctrail://api-guide` through MCP resources for this workflow
and the API's current limitations.

## Responses and failure handling

Successful results contain the same JSON in `structuredContent` and text content:

```json
{"status":202,"body":{"appDeploymentId":"deployment-id"},"location":"/deployments/deployment-id"}
```

The adapter preserves the gateway's JSON response; non-JSON responses are text
and empty bodies are `null`. `location`, `retryAfter`, and `correlator` are present
when CTRAIL sends the corresponding headers. Non-2xx HTTP responses set MCP
`isError=true` and preserve the upstream status and body, including CAMARA error
documents. Transport and local validation failures are MCP tool errors.

Requests have a timeout and responses are limited to 4 MiB. Redirects are returned
as errors instead of being followed. Identifiers must be single path segments;
unknown argument properties and invalid argument types are rejected before any
API request. Credentials and the upstream host cannot be selected by tool input.
API response text must be treated as data rather than agent instructions.

Writes are not automatically retried. A timeout or lost response can occur after
the gateway has applied a change; query existing resources before repeating it.
Traffic Influence PUT expects a complete request. Deployment PATCH replaces
supplied arrays and leaves omitted fields unchanged.

## Container

```sh
docker build -f Dockerfile.mcp -t ctrail-mcp .
docker run --rm -i \
  -e CTRAIL_BASE_URL=http://host.docker.internal:8080 \
  -e CTRAIL_BEARER_TOKEN \
  ctrail-mcp
```

For HTTP, add `-p 127.0.0.1:8081:8081`, set `CTRAIL_MCP_TRANSPORT=http` and
`CTRAIL_MCP_LISTEN_ADDR=0.0.0.0:8081`, and pass `CTRAIL_MCP_HTTP_TOKEN` using `-e`.
The image runs as a non-root user. `host.docker.internal` refers to the host on
Docker Desktop; on Linux add `--add-host=host.docker.internal:host-gateway` or
connect the adapter and gateway to the same Docker network with the gateway's
service name in `CTRAIL_BASE_URL`. For stdio, use `-i` without a TTY (`-t`).

## Validation

```sh
go test ./internal/mcpserver/... ./cmd/mcp
go test -race ./internal/mcpserver/... ./cmd/mcp
go test ./...
go vet ./...
```

The MCP tests use the official SDK client and mock HTTP servers. They cover all
17 tools' methods, paths, filters, authentication, JSON bodies, structured results,
empty and asynchronous responses, upstream errors, read-only discovery, unknown
arguments, path injection, redirects, timeout/cancellation, response limits,
resource discovery, HTTP authentication, origin checks, and legacy initialization.
The command test also launches the stdio entry point as a subprocess and checks
protocol output and shutdown. A functional test drives the Traffic Influence
lifecycle through the secured gateway with a simulated NEF backend, without
depending on uncommitted backend extensions. Tests do not contact a live mobile network
or Kubernetes cluster.
