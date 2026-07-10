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

Per-environment sidecar that grades exercises for a lab attempt by matching each task's regex `template` against real terminal output. On connect, sends the current `{"<taskId>": bool, ...}` grade map for every task loaded from `tasks.json`, then pushes a fresh grade map to every connected client whenever any grade changes — the connection otherwise stays open and idle.

- Same auth model as relay-exec: injected headers (`X-Attempt-Id`, `X-User-Id`), attempt-scoped (no `assetName`)
- Tasks loaded once at startup from `RELAY_TASKS_FILE` (`[{id, type, template, asset}]`, `type` must be `"term"`; `asset` is optional — omitted means the task isn't asset-scoped and can be satisfied by any asset's output)
- `RELAY_MY_NAMESPACE` and `RELAY_TASKS_FILE` required at startup; `RELAY_MY_ATTEMPT_ID`/`RELAY_MY_OWNER_ID` required unless `RELAY_SKIP_AUTH=true`
- Grading input: relay-exec streams a copy of every terminal output chunk to relay-grader's internal port (`RELAY_GRADER_INTERNAL_PORT`, default `8081`) as newline-delimited JSON (`{"asset":"<name>","data":"<raw chunk>"}\n`) over a plain TCP connection scoped by NetworkPolicy — no auth on this link, since only relay-exec pods in the same namespace can reach the port. relay-grader reassembles each asset's stream, strips ANSI escapes, keeps a rolling 10-line buffer per asset, and re-runs each unsatisfied task's regex against the relevant buffer(s) whenever new lines arrive
- Grades are sticky — once a task passes it stays passed for the life of the relay-grader process; a restart resets all grades to false (no persistence)
- relay-exec forwarding is entirely optional and best-effort: if `RELAY_GRADER_ADDR` is unset on relay-exec, or relay-grader is down/unreachable/slow, terminal sessions are completely unaffected — chunks are simply dropped, never blocking or erroring the terminal path

**URL:** `/relay/grade/<attemptID>/`  
**Healthz:** `/healthz`

See [`docs/lab-grade.md`](../../docs/lab-grade.md) for the full grading design and exercise authoring format.

## WebSocket protocol

relay-exec uses the following framing convention for messages from the client:

| First byte | Meaning        | Payload                                     |
|------------|----------------|---------------------------------------------|
| `\x01`     | Terminal resize | 4 bytes: cols (uint16 LE), rows (uint16 LE) |
| `\x00`     | Token refresh  | `REFRESH\n<token>`                          |
| anything else | stdin data  | Forwarded as-is                             |

Resize frames are buffered (capacity 1). Bursts drop intermediate sizes — only the last resize in a burst applies.

relay-grader doesn't use this framing — it sends a JSON grade map on connect and again whenever a grade changes, and otherwise stays idle (see relay-grader above).

## Structure

```
relay/
  pkg/
    pbclient/     # Thin PocketBase HTTP client (no SDK dependency)
    relaybase/    # Shared: WebSocket handler, auth, connection limiter, healthz, backoff reconnector
  exec/           # kubectl exec backend (Backend, KubeExecer, URL builder, Forwarder to relay-grader)
  ssh/            # SSH relay handler, proxy, key decrypt, Prometheus metrics
  grader/         # task loading (LoadTasks), grading Backend (regex matching, sticky grades, broadcast), internal NDJSON listener
  cmd/
    relay-exec/   # relay-exec binary
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