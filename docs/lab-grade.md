# Lab Grading

How exercises embedded in lab content get checked automatically, end to end: from the YAML source an author writes, through PocketBase and the Kubernetes operator, to the live regex matching that runs against a student's terminal.

## Overview

```
lab YAML (```exercise blocks)
   │  labs-sync.py: extract + validate + rewrite placeholders
   ▼
PocketBase `labs.exercises` field (id, description, type, asset?, template)
   │  attempt-controller: copy into LabEnvironment CRD
   ▼
LabEnvironment.spec.exercises
   │  labenv-operator: serialize into ConfigMap
   ▼
grader-tasks ConfigMap (tasks.json) — mounted into relay-grader
   │
   ▼
relay-grader ── grades against ──► terminal output streamed from relay-exec
   │
   ▼
frontend WebSocket (/relay/grade/<attemptID>/) — gray/green badges, pass counts
```

Two things move independently at runtime:
- **Terminal output** flows from relay-exec (which proxies the student's shell) to relay-grader over an internal, unauthenticated (NetworkPolicy-scoped) TCP connection.
- **Grades** flow from relay-grader to the browser over the existing PocketBase-authenticated WebSocket, pushed on connect and again every time a grade changes.

## Authoring an exercise

Exercises are embedded directly in a task's markdown as a fenced code block with the info-string `exercise`:

````markdown
```exercise
description: Create /tmp/labfile owned by bob
type: term
asset: server-0
template:
chown\s+bob\s+/tmp/labfile
```
````

Field rules:

- **`description`** — required. Shown to the student as the exercise's label.
- **`type`** — required. `"term"` is the only type relay-grader currently supports.
- **`asset`** — optional. When present, must match a `name` in the lab's `environment.assets[]`; the exercise is only checked against that asset's terminal output. When omitted, the exercise is lab-wide — it's satisfied if the pattern appears in *any* asset's output.
- **`template`** — required, and always the last field. For `type: term`, this is **a regular expression matched against the student's terminal output** — not a shell command relay-grader executes. It's satisfied once the pattern appears anywhere in that asset's recent scrollback. Field order otherwise doesn't matter (the parser scans for the four labels by line prefix), and `template:` can span multiple lines — everything from that line to the closing fence is the pattern body verbatim, so multi-line regexes don't need YAML block-scalar syntax.

A task's markdown may contain zero, one, or several exercise blocks. Other fenced blocks (` ```bash `, etc.) are untouched — only the exact `exercise` info-string is matched.

See [`labs/README.md`](../labs/README.md) for the full lab YAML schema this fits into.

## Sync-time processing

`labs-sync.py` (invoked on every backend startup, and via `--verify` for local validation) does three things with exercise blocks:

1. **Extracts and numbers them.** Each exercise gets an ID `"<task#>.<exercise#>"` — the task's 1-indexed position in `content`, a dot, then the exercise's 1-indexed position within that task (resetting per task). The 3rd exercise in the 2nd task is `"2.3"`.
2. **Validates `asset` references.** If a block names an `asset`, it must match a real asset in the same lab's `environment.assets[]` — otherwise sync fails.
3. **Rewrites the stored content.** Before uploading to PocketBase, each exercise block's body is replaced with a placeholder containing only `id` and `description` — `type`, `asset`, and `template` never leave the source YAML or reach the frontend:

   ````markdown
   ```exercise
   id: 2.3
   description: Create /tmp/labfile owned by bob
   ```
   ````

   The frontend renders this placeholder as a badge (gray = not yet passed, green = passed) using only the `id` to look up its grade.

The full exercise list (`id`, `description`, `type`, `asset`, `template`) is stored separately as an `exercises` field on the `labs` PocketBase collection — never exposed through `labs_userview`, the collection the frontend actually queries.

## Provisioning: from PocketBase to a running grader

1. **attempt-controller** copies `labs.exercises` into the `LabEnvironment` custom resource's `spec.exercises`, the same way it copies `spec.assets`.
2. **labenv-operator** serializes `spec.exercises` into a `grader-tasks` ConfigMap, dropping `description` (grader has no use for it) and writing `{id, type, template, asset?}` as `tasks.json`.
3. The operator deploys `relay-grader` as a per-attempt sidecar, mounting that ConfigMap at `/etc/grader/tasks.json` and pointing `RELAY_TASKS_FILE` at it. It also wires a NetworkPolicy rule allowing the attempt's `relay-exec` pod to reach `relay-grader`'s internal port (`8081` by default) — and only that pod, only on that port.
4. **relay-exec** is given `RELAY_GRADER_ADDR=relay-grader:8081` so it knows where to forward terminal output. This is the only new configuration relay-exec needs; everything else about its terminal-proxying is unchanged.

## Runtime: how a keystroke becomes a passed exercise

**Streaming terminal output (relay-exec → relay-grader).** Every chunk of PTY output relay-exec sends to the browser is also, unconditionally and best-effort, sent to relay-grader: `{"asset":"<assetName>","data":"<raw chunk>"}\n`, one JSON object per line, over a plain TCP connection relay-exec keeps open and reconnects with backoff. This link is deliberately dumb on the relay-exec side — no line-splitting, no buffering beyond a small bounded queue — because **relay-exec's terminal sessions must never be affected by relay-grader being down, slow, or absent.** If the queue fills or the connection is unreachable, chunks are silently dropped; the browser-facing terminal is completely unaffected either way. Grading is optional infrastructure layered on top of a working terminal, never a dependency of one.

**Grading (relay-grader).** For each asset, relay-grader reassembles the incoming byte stream, strips ANSI escape codes (so colored prompts don't break regex matching), splits on newlines, and keeps the most recent 10 complete lines in a per-asset ring buffer. Whenever new lines arrive for an asset, every exercise that hasn't already passed gets its `template` regex re-run against the relevant buffer — that asset's buffer if the exercise is asset-scoped, every asset's buffer if it's lab-wide.

**Grades are sticky.** Once an exercise's regex matches, it's marked passed permanently for the life of the relay-grader process — it can never un-pass, even if the matching text later scrolls out of the 10-line window. A relay-grader restart (rare — one per attempt, no persistence) resets everything to unpassed; there's no cross-restart memory by design, matching the same "bootstrap, no state store" model the rest of relay uses.

**Delivering grades (relay-grader → frontend).** The existing PocketBase-authenticated WebSocket at `/relay/grade/<attemptID>/` sends the full `{"<exerciseId>": true|false, ...}` map as soon as a client connects, and again — a fresh full map, not a diff — every time any exercise's grade changes. The frontend's `useGraderConnection` composable already treats every incoming message as a wholesale replacement of its local grade state, so this "push on every change" behavior required no frontend changes to support.

## Design constraints worth knowing

- **No auth on the relay-exec ↔ relay-grader link.** The trust boundary is Kubernetes NetworkPolicy, not a token — only pods labeled `app: relay-exec` in the same attempt's namespace can reach relay-grader's internal port at all, which is the same pattern relay-exec already uses for its own egress to the Kubernetes API.
- **One relay-grader pod per attempt.** Grading state (buffers, grades, connected clients) is entirely in-memory and never shared across attempts or users — there's no cross-user blast radius from a slow or many-tabs client.
- **`type: "term"` is the only supported exercise type today.** The design leaves room for other types later, but only regex-against-terminal-scrollback exists now.
