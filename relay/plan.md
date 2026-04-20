Relay Development Iterations — High-Level Plan
Context
The relay is a Go WebSocket-to-SSH proxy that bridges the browser terminal (xterm.js) to lab VMs. All surrounding infrastructure is ready — PocketBase schema, lab definitions, frontend components — but the relay has zero Go code. This plan breaks the build into focused, shippable iterations, each adding a testable layer on top of the previous one.

Iteration 1 — Skeleton & Wiring
Goal: A running Go HTTP server, integrated into Docker Compose and routed through Nginx.

go.mod + minimal main.go (HTTP server, /healthz endpoint, graceful shutdown)
Dockerfile + Dockerfile.dev (with live-reload via air)
Add relay service to docker-compose.yaml and compose-dev.yaml
Add /relay/ upstream + location block to nginx-dev.conf (and production config)
Env-var config loader: POCKETBASE_URL, PORT, LOG_LEVEL
Done when: docker compose up starts relay, nginx routes /relay/ to it, /healthz returns 200.

Iteration 2 — Auth & Authorization
Goal: Relay can validate a PocketBase token and confirm the user owns the requested server.

pkg/pbclient — thin PocketBase HTTP client:
RELAY_BACKEND_URL
RELAY_BACKEND_USERNAME
RELAY_BACKEND_PASSWORD
ValidateToken(token) → (userID, error)
GetServer(serverID) → (server, error) — returns full server record including attempt FK and connection JSON
GetAttempt(attemptID) → (attempt, error) — returns attempt record including user FK
Authorization check: GetServer → GetAttempt → assert attempt.user == tokenUserID
WebSocket endpoint GET /relay/{serverID}/ — upgrade, validate, close on any auth/authz failure
Unit tests for pbclient (mock HTTP server)
Done when: invalid tokens, unknown server IDs, and servers belonging to another user cause clean WebSocket close; valid token + owned server returns 101 Upgrade (no SSH yet).

Iteration 3 — SSH Proxying
Goal: Full bidirectional terminal session over WebSocket ↔ SSH.

SSH dial using connection.{host, port, user} from server record and default private-key RELAY_PRIVATE_KEY_PATH
Allocate PTY, request interactive shell
Goroutine pair: WebSocket→SSH stdin, SSH stdout→WebSocket (raw binary frames)
Tear down both sides cleanly on either end closing

Security work deferred from earlier iterations (implement here, alongside the read/write pump):
- Idle timeout: reset a timer on every read; close with StatusGoingAway if no data flows for N minutes (default 30m, env RELAY_IDLE_TIMEOUT). Requires an active reader loop — not possible in the stub.
- Backpressure: bound the SSH→WebSocket write buffer (e.g. channel of fixed capacity); drop the connection if the buffer stays full for longer than a deadline rather than letting memory grow unbounded. Fast producers (e.g. cat large_file) are normal SSH traffic, so the buffer must be generous before dropping.
- Rate limiting: if needed, apply token-bucket rate limiting on the WebSocket→SSH write path (stdin direction), not on reads. Terminal output (SSH→WS) must never be rate-limited as it would corrupt the stream.

Done when: opening a WebSocket to a real server yields a live shell; closing the tab terminates the SSH session; idle connections close automatically; a flood of inbound data does not grow relay memory without bound.

Iteration 4 — Terminal Resize
Goal: Window resize events from xterm.js propagate to the PTY.

Define framing protocol for resize messages (single control byte prefix, e.g. \x01 + cols/rows as 2×uint16 LE; plain bytes otherwise treated as stdin)
Relay detects resize frames and calls pty.WindowChange(cols, rows)
Document protocol in relay/.claude/rules/relay.md and memory/memory-relay-interface.md
Update frontend WebSocket code to send resize frames on xterm.Terminal.onResize
Done when: resizing the browser window resizes the PTY correctly.

Iteration 5 — Observability & Hardening
Goal: Production-ready relay with structured logging, connection limits, and timeouts.

Structured JSON logging (connection open/close, user ID, server ID, duration, error reason)
Idle-connection timeout (configurable, default 30 min)
Max concurrent connections guard (configurable)
Graceful shutdown: stop accepting new connections, wait for in-flight sessions to drain (with deadline)
Integration test: spin up a mock SSH server, connect via WebSocket, assert I/O round-trip
Done when: logs are machine-parseable; stress test shows no goroutine leaks; graceful shutdown drains connections cleanly.

Critical Files
File	Role
relay/main.go	Entry point, HTTP server, graceful shutdown
relay/pkg/pbclient/	PocketBase HTTP client (auth + data)
relay/pkg/handler/	WebSocket upgrade, auth flow, SSH proxy
relay/Dockerfile / Dockerfile.dev	Container builds
infra/docker-compose.yaml	Relay service definition
infra/nginx-dev.conf	/relay/ routing
relay/.claude/rules/relay.md	Spec (update resize protocol in Iter 4)
relay/.claude/memory/memory-relay-interface.md	Interface contract (update in Iter 4)
Verification (end-to-end)
docker compose up — all services green, /healthz returns 200
Open a lab attempt in the browser, open Console tab — WebSocket handshake succeeds
Type a command (ls) — output appears in xterm.js terminal
Resize browser window — PTY adapts (no line-wrap artifacts)
Close tab — relay logs clean disconnection; SSH session terminated on VM
Bad token — WebSocket closes immediately; relay logs auth failure
