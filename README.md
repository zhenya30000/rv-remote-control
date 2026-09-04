# RV Remote Control

A small Go control plane for sending commands from a remote client to hardware inside an RV.

The RV is usually connected through mobile internet, so it cannot rely on a stable public IP address or inbound connections. The edge agent inside the vehicle therefore opens an **outbound long-lived bidirectional gRPC stream** to the cloud. The cloud service exposes a small authenticated HTTP API and routes commands through the active device session.

The first supported command is switching channels on the JDY-33 relay exposed by [`ble-device-gateway`](https://github.com/zhenya30000/ble-device-gateway).

The cloud side is currently deployed on **Google Cloud Run**. A full remote path has been verified from an HTTPS client through Cloud Run, the bidirectional gRPC stream, the local edge agent, and the BLE gateway in mock mode.

## Architecture

```text
Phone / remote client
        |
        | HTTPS
        v
+----------------------------------+
| Google Cloud Run                 |
|                                  |
| TLS termination                  |
| HTTP/2 -> h2c                    |
+----------------+-----------------+
                 |
                 | single container port :8080
                 v
+----------------------------------+
| Cloud Control Service            |
|                                  |
| REST API + gRPC on one port      |
| in-memory device session registry|
+----------------+-----------------+
                 ^
                 |
                 | long-lived bidirectional gRPC
                 | outbound TLS connection from RV
                 |
+----------------+-----------------+
| RV Edge Agent                    |
|                                  |
| connection lifecycle             |
| heartbeat + command execution    |
+----------------+-----------------+
                 |
                 | local gRPC
                 v
+----------------------------------+
| BLE Device Gateway               |
| RelayService.SetChannel          |
+----------------+-----------------+
                 |
                 | BLE
                 v
             JDY-33 relay
```

The cloud never initiates a connection to the RV. This is intentional: the vehicle may be behind carrier-grade NAT, have a changing IP address, or temporarily lose connectivity.

### One-port Cloud Run transport

Cloud Run exposes a single ingress port to the container, while this service needs both a regular HTTP API and native gRPC.

`cloud-control` therefore serves both protocols on `:8080`:

- HTTP/1.1 or HTTP/2 requests are routed to the REST API;
- HTTP/2 requests with `Content-Type: application/grpc` are routed to the gRPC server;
- local deployments use plaintext HTTP/h2c;
- the public Cloud Run endpoint terminates TLS and forwards HTTP/2 to the container.

The edge agent connects to the public Cloud Run hostname over TLS on port `443`.

## Command flow

```text
PUT /v1/devices/rv-001/relays/2
{"enabled": true}
          |
          v
Cloud creates command ID
          |
          v
active device gRPC stream
          |
          v
Edge Agent
          |
          v
local RelayService.SetChannel
          |
          v
BLE Device Gateway -> JDY-33
          |
          v
CommandResult
          |
          v
HTTP 200 "applied"
```

The MVP deliberately does **not queue commands for offline vehicles**. A relay command is accepted only while the device has an active cloud session. This avoids applying an old command unexpectedly after the RV reconnects hours later.

## Repository layout

```text
api/control/v1/       cloud <-> edge protobuf contract
gen/control/v1/       generated protobuf / gRPC code

cmd/
├── cloud-control/    REST API + edge gRPC endpoint
└── edge-agent/       outbound cloud client + local gateway client

internal/
├── auth/             constant-time bearer token validation
├── cloud/            active device sessions and command routing
├── config/           environment configuration
├── edge/             stream lifecycle, heartbeat and command execution
├── gateway/          client for ble-device-gateway RelayService
├── httpapi/          remote-client HTTP API
└── retry/            context-aware exponential reconnect backoff
```

## HTTP API

### Health

```http
GET /health
```

Example:

```bash
curl http://localhost:8080/health
```

Expected response:

```json
{"status":"ok"}
```

### Device status

```http
GET /v1/devices/{deviceId}/status
Authorization: Bearer <CONTROL_API_TOKEN>
```

Example:

```bash
curl \
  -H "Authorization: Bearer dev-api-token" \
  http://localhost:8080/v1/devices/rv-001/status
```

An active edge session returns connection information such as `connected_at` and `last_seen`. An unknown or disconnected device returns `online: false`.

### Set relay channel

```http
PUT /v1/devices/{deviceId}/relays/{channel}
Authorization: Bearer <CONTROL_API_TOKEN>
Content-Type: application/json
```

Request:

```json
{
  "enabled": true
}
```

Example:

```bash
curl \
  -X PUT \
  -H "Authorization: Bearer dev-api-token" \
  -H "Content-Type: application/json" \
  -d '{"enabled":true}' \
  http://localhost:8080/v1/devices/rv-001/relays/2
```

Successful response:

```json
{
  "command_id": "7b4e...",
  "status": "applied",
  "device_id": "rv-001",
  "channel": 2,
  "enabled": true
}
```

If the RV is offline, the API returns `503 Service Unavailable`.

## Authentication

The MVP uses two independent bearer tokens:

- `CONTROL_API_TOKEN` authenticates the remote-client HTTP API;
- `EDGE_TOKEN` authenticates the outbound edge gRPC connection.

The edge also sends `x-device-id` as gRPC metadata.

Token comparison is constant-time. Public traffic is protected by TLS when deployed to Cloud Run. A later version can replace the shared edge token with per-device credentials or mutual TLS.

## Local end-to-end test

### 1. Start BLE Device Gateway in mock mode

In the `ble-device-gateway` repository:

```bash
GATEWAY_MODE=mock go run ./cmd/server
```

It should listen on:

```text
localhost:50051
```

### 2. Configure this project

```bash
cp .env.example .env
```

Use simple local values, for example:

```text
CONTROL_API_TOKEN=dev-api-token
EDGE_TOKEN=dev-edge-token
DEVICE_ID=rv-001
```

### 3. Run cloud and edge services

```bash
docker compose up --build
```

The cloud service listens on `:8080`. The edge container connects to it over h2c and reaches the BLE gateway running on the host through `host.docker.internal:50051`.

### 4. Check connection status

```bash
curl \
  -H "Authorization: Bearer dev-api-token" \
  http://localhost:8080/v1/devices/rv-001/status
```

Expected result:

```json
{
  "device_id": "rv-001",
  "online": true
}
```

### 5. Send a relay command

```bash
curl \
  -X PUT \
  -H "Authorization: Bearer dev-api-token" \
  -H "Content-Type: application/json" \
  -d '{"enabled":true}' \
  http://localhost:8080/v1/devices/rv-001/relays/2
```

The command travels through the cloud gRPC stream, reaches the edge agent and becomes a local `RelayService.SetChannel` call to BLE Device Gateway.

Because the gateway is in mock mode, no physical relay is required.

## Google Cloud Run deployment

The current deployment uses:

```text
Docker image
    -> Artifact Registry (europe-west3)
    -> Cloud Run
    -> public HTTPS / HTTP2 endpoint
```

The container image is built from `Dockerfile.cloud`. Cloud Run provides the `PORT` environment variable; `cloud-control` uses it automatically and serves both REST and gRPC on that port.

A minimal manual deployment flow is:

```bash
PROJECT_ID=$(gcloud config get-value project)
REGION=europe-west3
TAG=$(git rev-parse --short HEAD)
IMAGE="$REGION-docker.pkg.dev/$PROJECT_ID/rv-remote-control/cloud-control:$TAG"

docker build -f Dockerfile.cloud -t "$IMAGE" .
docker push "$IMAGE"

gcloud run deploy rv-remote-control \
  --image="$IMAGE" \
  --region="$REGION" \
  --use-http2
```

The service requires `EDGE_TOKEN` and `CONTROL_API_TOKEN`. They are currently supplied as runtime configuration and should be moved to Secret Manager before treating the deployment as production infrastructure.

### Remote edge test

With the cloud service deployed and the local BLE gateway running, the edge can connect directly to Cloud Run:

```bash
CLOUD_GRPC_ADDR=<cloud-run-hostname>:443
CLOUD_INSECURE=false
```

The verified remote path is:

```text
HTTPS client
  -> Cloud Run
  -> cloud-control
  -> bidirectional gRPC/TLS
  -> local edge-agent
  -> local gRPC
  -> BLE Device Gateway mock
  -> CommandResult
  -> HTTP response
```

The remote test has verified:

- TLS connectivity from the edge agent to Cloud Run;
- long-lived bidirectional gRPC through Cloud Run;
- heartbeat and `online` status updates;
- command routing to the local BLE gateway;
- acknowledgement propagation back to the HTTP client.

The remaining hardware smoke test is running the same cloud path against the physical JDY-33 relay instead of the gateway mock.

## Edge connection lifecycle

The edge agent reconnects automatically:

```text
connect
  |
  v
stream active
  |
  v
network failure / stream termination
  |
  v
1s -> 2s -> 4s -> 8s -> 16s -> 30s
  |
  v
connect again
```

If a connection remained healthy for at least 30 seconds, the retry delay resets to one second.

Heartbeats update the device's `last_seen` timestamp in the cloud registry. This also makes the edge tolerant of cloud-side stream termination: it reconnects instead of assuming a permanent session.

## Scaling note

The active-device registry is intentionally in memory for this MVP. That keeps the control path small and easy to reason about, but it also means the current design assumes a single active `cloud-control` instance.

Horizontal scaling would require moving session coordination outside the process, for example through a shared broker or other routing layer, so that an HTTP request can reach the instance that owns a device's active gRPC stream.

## Tests and CI

```bash
go test ./...
go test -race ./...
go vet ./...
```

The tests cover:

- command routing through an active device session;
- offline-device behavior;
- authenticated HTTP relay control;
- HTTP authorization;
- health endpoint behavior;
- edge relay command execution and failure reporting.

GitHub Actions also builds both Go binaries and both Docker images.

## Current scope

Implemented:

- authenticated HTTP control API;
- public health endpoint;
- outbound edge connection;
- bidirectional gRPC control stream;
- REST and gRPC multiplexed on one Cloud Run-compatible port;
- active device registry;
- command IDs and acknowledgements;
- relay command forwarding to BLE Device Gateway;
- heartbeat / online status;
- context cancellation;
- reconnect with exponential backoff;
- TLS connection to the public cloud endpoint;
- local mock-friendly setup;
- Docker images and Docker Compose;
- Google Artifact Registry image deployment;
- Google Cloud Run deployment;
- local and remote end-to-end tests;
- tests and CI.

Intentionally not implemented yet:

- persistent command history;
- offline command queue;
- multi-user authorization;
- per-device certificates;
- shared session state for horizontal scaling;
- metrics / tracing;
- Secret Manager integration;
- automated GitHub Actions deployment / Workload Identity Federation;
- infrastructure as code.
