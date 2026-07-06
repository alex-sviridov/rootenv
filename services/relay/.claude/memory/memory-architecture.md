---
description: Non-obvious architectural details and invariants for the relay module
paths:
  - "relay/*"
---

# Relay Architecture

_Record non-obvious structural details, invariants, and design constraints here as they are discovered._

## relay-exec goroutine model

`Backend.Serve` runs 3 goroutines per session:
1. **exec goroutine** — calls `Execer.Exec`; sends result to `execDone` buffered chan, then closes `stdoutW`
2. **stdout→WS goroutine** — reads from `stdoutR` pipe in a 32 KB loop, writes each chunk as a binary WS message; signals `stdoutDone` on exit
3. **WS→stdin goroutine** — reads WS messages; decodes `\x01`+4-byte resize frames into `resizeCh` (cap 1, drops on full); writes everything else to `stdinW`; on exit calls `defer cancel()` + `defer stdinW.Close()` + `defer close(resizeCh)`

`Serve` blocks on `<-execDone` then `<-stdoutDone`. The WS-read goroutine is intentionally not waited on — it exits on its own once the WS conn is closed.

**Critical invariant:** the WS-read goroutine calls `cancel()` on exit. This propagates WS disconnect to the exec context, causing a well-behaved `Execer` to return. Without this, a hanging pod process would keep the session alive until the HTTP server shut down.

**Resize channel:** capacity 1; extra frames are dropped silently (last resize wins during a burst). This matches relay-ssh behavior.

**File layout:** `exec/backend.go` — `Execer` interface + `Backend.Serve`; `exec/kube.go` — `KubeExecer` (real k8s), `podExecURL`, `chanSizeQueue`.

## relay-exec Forwarder goroutine model

`exec.Backend.Serve`'s existing stdout→WS goroutine gained one more call per chunk: `b.Forwarder.Send(assetName, buf[:n])`, right after the `conn.Write` to the browser. No new goroutine in `Serve` itself.

`Forwarder` (in `exec/forwarder.go`) owns its own background goroutine (started in `NewForwarder`, only if `addr != ""`) that dials relay-grader's internal port, drains a buffered channel (cap 256) into the connection, and reconnects with exponential backoff (500ms → 10s cap) on any write/dial failure. `Send` is a non-blocking channel send with a `default: drop` case — this is the sole mechanism keeping relay-exec's hot path safe from grader unavailability.

**Critical invariant:** `Forwarder.Send` must never be changed to a blocking send or to retry synchronously — doing so would let a stuck/slow relay-grader stall real terminal sessions.
