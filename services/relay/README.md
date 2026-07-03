# relay

WebSocket-to-terminal relay service. Accepts connections from the browser, authenticates them, and proxies stdin/stdout to a lab environment. The frontend never touches the lab directly — relay is the only component that does.

## Relay types

### relay-exec

Per-environment sidecar deployed by the labenv operator alongside each lab environment. Connects to lab pods via `kubectl exec`.

- Auth via injected headers (`X-Attempt-Id`, `X-User-Id`) set by the operator — not the client
- Scoped to a single attempt: `RELAY_MY_ATTEMPT_ID` and `RELAY_MY_NAMESPACE` are required at startup
- TLS transport to the Kubernetes API is built once at startup and reused across sessions
- No PocketBase dependency at runtime

**URL:** `/relay/exec/<attemptID>/<assetName>/`  
**Healthz:** `/healthz`

### relay-grader

Per-environment sidecar (bootstrap; not yet wired into the labenv operator) that reports task grading status for a lab attempt. On connect, sends `{"<taskId>": false, ...}` for every task loaded from `tasks.json`, then holds the connection open — no grading logic runs yet.

- Same auth model as relay-exec: injected headers (`X-Attempt-Id`, `X-User-Id`), attempt-scoped (no `assetName`)
- Tasks loaded once at startup from `RELAY_TASKS_FILE` (`[{id, type, template}]`, `type` must be `"term"`)
- `RELAY_MY_NAMESPACE` and `RELAY_TASKS_FILE` required at startup; `RELAY_MY_ATTEMPT_ID`/`RELAY_MY_OWNER_ID` required unless `RELAY_SKIP_AUTH=true`

**URL:** `/relay/grade/<attemptID>/`  
**Healthz:** `/healthz`

### relay-ssh

SSH-based relay — the only component with direct SSH access to lab VMs. Proxies terminal I/O over an SSH session.

**Healthz:** `/relay/ssh/healthz`

## WebSocket protocol

relay-exec and relay-ssh share the same framing convention for messages from the client:

| First byte | Meaning        | Payload                                     |
|------------|----------------|---------------------------------------------|
| `\x01`     | Terminal resize | 4 bytes: cols (uint16 LE), rows (uint16 LE) |
| `\x00`     | Token refresh  | `REFRESH\n<token>`                          |
| anything else | stdin data  | Forwarded as-is                             |

Resize frames are buffered (capacity 1). Bursts drop intermediate sizes — only the last resize in a burst applies.

relay-grader doesn't use this framing — it sends one JSON message on connect and otherwise stays idle (see relay-grader above).

## Structure

```
relay/
  pkg/
    pbclient/     # Thin PocketBase HTTP client (no SDK dependency)
    relaybase/    # Shared: WebSocket handler, auth, connection limiter, healthz, backoff reconnector
  exec/           # kubectl exec backend (Backend, KubeExecer, URL builder)
  ssh/            # SSH relay handler, proxy, key decrypt, Prometheus metrics
  grader/         # task loading (LoadTasks) + Backend (grade report over WS)
  cmd/
    relay-exec/   # relay-exec binary
    relay-ssh/    # relay-ssh binary
    relay-grader/ # relay-grader binary
```

## Running locally

```sh
# build all binaries
go build ./cmd/...

# run tests
go test ./...
```

Each binary reads configuration from environment variables. See `loadConfig()` in the respective `cmd/*/main.go` for the full list. All binaries support `LOG_LEVEL=debug` for verbose output.