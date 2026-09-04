# RV Remote Control

A small Go control plane for sending commands from a remote client to hardware inside an RV.

The RV is usually connected through mobile internet, so it cannot rely on a stable public IP address or inbound connections. The edge agent inside the vehicle therefore opens an **outbound long-lived bidirectional gRPC stream** to the cloud. The cloud service exposes a small HTTP API for remote clients and routes commands through the active stream.

The first supported command is switching channels on the JDY-33 relay exposed by [`ble-device-gateway`](https://github.com/zhenya30000/ble-device-gateway).

## Architecture

```text
Phone / remote client
        |
        | HTTPS
        v
+----------------------------+
| Cloud Control Service      |
|                            |
| HTTP API :8080             |
| device session registry    |
| gRPC endpoint :50052       |
+-------------+--------------+
              ^
              |
              | long-lived bidirectional gRPC
              | outbound connection from RV
              |
+-------------+--------------+
| RV Edge Agent              |
|                            |
| cloud connection lifecycle |
| heartbeat + command result |
+-------------+--------------+
              |
              | local gRPC
              v
+----------------------------+
| BLE Device Gateway         |
| RelayService.SetChannel    |
+-------------+--------------+
              |
              | BLE
              v
          JDY-33 relay
```

The cloud does not initiate a connection to the RV. This is intentional: the vehicle may be behind carrier-grade NAT, have a changing IP address, or temporarily lose connectivity.

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

The MVP deliberately does **not queue commands for offline vehicles**. A relay command is only accepted while the device has an active cloud session. This avoids applying an old command unexpectedly after the RV reconnects hours later.

## Repository layout

```text
api/control/v1/       cloud <-> edge protobuf contract
gen/control/v1/       generated protobuf / gRPC code

cmd/
├── cloud-control/    HTTP API + edge gRPC endpoint
└── edge-agent/       outbound cloud client + local gateway client

internal/
├── auth/             constant-time bearer token validation
├── cloud/            active device sessions and command routing
├── config/           environment configuration
├── edge/             stream lifecycle, heartbeat and command execution
├── gateway/          client for ble-device-gateway RelayService
├── httpapi/          phone-facing HTTP API
└── retry/            context-aware exponential reconnect backoff
```

## HTTP API

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

- `CONTROL_API_TOKEN` authenticates the phone-facing HTTP API;
- `EDGE_TOKEN` authenticates the outbound edge gRPC connection.

The edge also sends `x-device-id` as gRPC metadata.

Token comparison is constant-time. For production deployment the public endpoints should always be behind TLS. A later version can replace the shared edge token with per-device credentials or mutual TLS.

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

The edge container connects to the BLE gateway running on the host through `host.docker.internal:50051`.

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

## Edge connection lifecycle

The edge agent reconnects automatically:

```text
connect
  |
  v
stream active
  |
  v
network failure
  |
  v
1s -> 2s -> 4s -> 8s -> 16s -> 30s
  |
  v
connect again
```

If a connection remained healthy for at least 30 seconds, the retry delay resets to one second.

Heartbeats update the device's `last_seen` timestamp in the cloud registry.

## TLS

For local development:

```text
CLOUD_INSECURE=true
```

For AWS deployment, leave it false. The edge agent then uses TLS and the operating system certificate roots. `CLOUD_SERVER_NAME` can be set when explicit TLS server-name verification is required.

The application containers themselves can stay on plaintext HTTP/gRPC behind an AWS Application Load Balancer that terminates TLS.

## Tests

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
- edge relay command execution and failure reporting.

GitHub Actions also builds both Go binaries and both Docker images.

## Current scope

Implemented:

- HTTP control API;
- outbound edge connection;
- bidirectional gRPC control stream;
- active device registry;
- command IDs and acknowledgements;
- relay command forwarding to BLE Device Gateway;
- heartbeat / online status;
- context cancellation;
- reconnect with exponential backoff;
- local mock-friendly setup;
- bearer-token authentication;
- Docker images;
- Docker Compose;
- tests and CI.

Intentionally not implemented yet:

- persistent command history;
- offline command queue;
- multi-user authorization;
- per-device certificates;
- metrics / tracing;
- AWS infrastructure as code.

The next step is deploying the cloud side to AWS and then pointing the edge agent at the public TLS endpoint.
