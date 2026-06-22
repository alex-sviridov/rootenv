---
description: Coding style, tooling, and workflow preferences specific to the relay module
paths:
  - "relay/*"
---

# Relay Preferences

_Record relay-specific coding conventions, patterns to follow or avoid, and tooling preferences here._

## Test structure

- **White-box tests** (package `exec`, not `exec_test`) live in `kube_test.go` — they access unexported fields (`KubeExecer.host`, `newExecutor`) to verify internal structure like transport identity.
- **Black-box integration tests** (package `exec_test`) live in `backend_test.go` and `backend_edge_test.go` — they drive `Backend.Serve` through a real `httptest.Server` + `websocket.Dial`.
- **Test helpers in edge tests:** use purpose-built execer types (`stdinCaptureExecer`, `blockingExecer`, `slowResizeExecer`, `hangExecer`) that signal completion via channels rather than polling with `time.Sleep`. `recordingExecer` drains stdin/resize in goroutines — never block on both in sequence or you deadlock.
- **Handler tests** mount on a real `http.ServeMux` with the production route pattern so `r.PathValue("assetName")` is populated. Direct `h.ServeHTTP` calls return 400 because path values are empty.

## Coding conventions

- Execers must drain stdin and resize concurrently (both can block); never `ReadAll(stdin)` while holding the resize channel synchronously.
- `Execer` implementations should respect context cancellation — `Serve` cancels ctx on WS disconnect and relies on exec returning promptly.
