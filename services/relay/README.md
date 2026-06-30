# relay

WebSocket-to-terminal relay service. Accepts connections from the browser, authenticates them, and proxies stdin/stdout to a lab environment. The frontend never touches the lab directly — relay is the only component that does.

## One relay type

### relay-exec

Per-environment sidecar deployed by the labenv operator alongside each lab environment. Connects to lab pods via `kubectl exec`.

- Auth via injected headers (`X-Attempt-Id`, `X-User-Id`) set by the operator — not the client
- Scoped to a single attempt: `RELAY_MY_ATTEMPT_ID` and `RELAY_MY_NAMESPACE` are required at startup
- TLS transport to the Kubernetes API is built once at startup and reused across sessions
- No PocketBase dependency at runtime

**URL:** `/relay/exec/<attemptID>/<assetName>/`  
**Healthz:** `/healthz`

## WebSocket protocol

Both relay types share the same framing convention for messages from the client:

| First byte | Meaning        | Payload                                     |
|------------|----------------|---------------------------------------------|
| `\x01`     | Terminal resize | 4 bytes: cols (uint16 LE), rows (uint16 LE) |
| `\x00`     | Token refresh  | `REFRESH\n<token>`                          |
| anything else | stdin data  | Forwarded as-is                             |

Resize frames are buffered (capacity 1). Bursts drop intermediate sizes — only the last resize in a burst applies.

## Structure

```
relay/
  pkg/
    pbclient/     # Thin PocketBase HTTP client (no SDK dependency)
    relaybase/    # Shared: WebSocket handler, auth, connection limiter, healthz, backoff reconnector
  exec/           # kubectl exec backend (Backend, KubeExecer, URL builder)
  ssh/            # SSH relay handler, proxy, key decrypt, Prometheus metrics
  cmd/
    relay-exec/   # relay-exec binary
```

## Running locally

```sh
# build all binaries
go build ./cmd/...

# run tests
go test ./...
```

Each binary reads configuration from environment variables. See `loadConfig()` in the respective `cmd/*/main.go` for the full list. Both support `LOG_LEVEL=debug` for verbose output.