# Design: Exercises in Lab Definitions

**Date:** 2026-07-04
**Branch:** feat/relay-grader
**Status:** Approved

## Problem

Lab task markdown currently has no way to express a gradeable exercise. `relay-grader` (deployed per `LabEnvironment` per the `ensure-relay-grader` design) reads `tasks.json` from a ConfigMap, but that ConfigMap content is a hardcoded placeholder (`graderTasksPlaceholder` in `services/labenv-operator/internal/controller/grader.go`) — no real task data flows from lab authoring into it.

This design adds an authoring syntax for exercises embedded in task markdown, extracts and validates them at sync time, and threads the extracted data through PocketBase → attempt-controller → the `LabEnvironment` CRD → labenv-operator's `grader-tasks` ConfigMap, replacing the placeholder with real per-lab task data.

**Out of scope:**
- Frontend rendering of the exercise placeholder as an interactive (gray/green) badge — this design only produces markdown containing a parseable placeholder; actually rendering it in Vue is future work.
- Frontend-to-grader WebSocket connection (subscribing to grade results, updating badges live) — separate future design.
- Grader filtering/using the `asset` field to scope checks to a specific terminal — the field flows through end-to-end but `grader.Backend` does not act on it yet.

## 1. Authoring Syntax

Exercises are authored inline inside a task's `content` markdown, as a fenced code block with a custom info-string:

````markdown
```exercise
description: Create /tmp/labfile owned by bob
type: term
asset: server-0
template:
test -O /tmp/labfile
test -G /tmp/labfile
```
````

Rules:
- Plain `key: value` lines — no YAML parsing of the block body.
- `description` and `type` are required. `type` must currently be `"term"` (matches `grader.LoadTasks`'s existing constraint) — always written explicitly, no default.
- `asset` is optional. When present, it must match a `name` in that lab's `environment.assets`. When omitted, the grader does not filter by terminal (checks apply lab-wide).
- Field order is not fixed — the parser scans for the `description:`, `type:`, `asset:`, `template:` labels rather than assuming position.
- `template:` is always the last field. Everything after that line, up to the closing fence, is the template body verbatim — this is how multi-line templates (multi-command shell scripts) are supported without needing YAML block-scalar syntax.
- A task's markdown may contain zero, one, or several exercise blocks.
- Because it's a standard fenced code block, it renders as inert code if ever displayed unprocessed.

## 2. `labs-sync.py`: Extraction, Validation, ID Computation, Content Rewrite

Extends the existing per-lab validation pass (`validate_lab`) and upsert (`upsert_lab`) in `scripts/labs-sync.py`.

For each lab YAML, after existing structural validation passes:

1. **Scan**: for each `content[i]` task, find all fenced blocks whose info-string is exactly `exercise` (i.e. a line matching `` ```exercise `` opening the fence, closed by a `` ``` `` line) in that task's `content` markdown string, in order of appearance. Other fenced blocks (e.g. `` ```bash ``) are left untouched.
2. **Parse**: for each block, extract `description`, `type`, `asset` (if present) by scanning lines for their labels; everything after the `template:` line (to the closing fence) becomes the `template` body.
3. **Validate** (failures behave like existing `validate_lab` errors — printed, causing `--verify`/sync to exit 1):
   - `description` missing → error.
   - `type` missing, or not equal to `"term"` → error.
   - `asset` present but not found in that lab's `environment.assets[].name` → error.
4. **Compute id**: task number = 1-indexed position of the task in `content` (`content[0]` → task 1). Exercise number = 1-indexed position of the block within that task's markdown, resetting per task. E.g. the 3rd exercise block in the 2nd task → id `"2.3"`.
5. **Build `exercises` list**: flat list across the whole lab: `[{id, description, type, asset?, template}, ...]`.
6. **Rewrite `content` for storage**: replace each ` ```exercise ` block in the markdown with a stripped placeholder block containing only `id` and `description`:
   ````markdown
   ```exercise
   id: 2.3
   description: Create /tmp/labfile owned by bob
   ```
   ````
   This rewritten `content` — not the original — is what gets stored on the `labs` record. `type`, `asset`, and `template` never appear in it.
7. **Upsert**: `upsert_lab` sends both the rewritten `content` and the new `exercises` field to PocketBase.

## 3. PocketBase Schema Change

New field on the `labs` collection:

| Field | Type | Notes |
|---|---|---|
| `exercises` | json | array of `{id, description, type, asset?, template}`, written by labs-sync.py only |

- `labs_userview` is **not** changed — it continues to select the same fixed fields (`id, title, description, content, parent, type`). `exercises` is never exposed through it, so `type`/`template`/`asset` never reach the public view. Only the rewritten `content` (with stripped placeholders, description-only) is visible there.
- `exercises` is readable only via the base `labs` collection, whose `viewRule` (`@request.auth.svc_role = 'attempt-controller'`) already restricts it appropriately — no new PocketBase rule needed.

## 4. attempt-controller: Copy Exercises into the CRD

Mirrors the existing `environment` → `spec.assets` pattern in `internal/downstream/reconcile.go`.

- `AttemptRecord.Expand.Lab` (in `internal/pocketbase/pbclient.go`) gains an `Exercises json.RawMessage` field, sibling to the existing `Environment` field.
- New `downstream.Exercise` struct: `{ID, Description, Type, Asset, Template string}` with json tags matching the PocketBase field names (`asset` uses `omitempty` since it's optional).
- `AttemptRecord.ToAttempt()` unmarshals `Expand.Lab.Exercises` into `Attempt.Exercises []Exercise`, the same way `Environment.Assets` is unmarshaled today.
- `toLabEnvironment()` maps each `Exercise` into a `map[string]any` (`id`, `description`, `type`, `asset`, `template`) and sets `spec["exercises"]` on the unstructured CRD, alongside the existing `spec["assets"]`.
- The existing comment noting `EnvironmentSpec`/`Asset` must stay in sync with labenv-operator's types is extended to cover `Exercise` too.

## 5. labenv-operator: `spec.exercises` → `tasks.json`

- `LabEnvironmentSpec` (`api/v1alpha1/labenvironment_types.go`) gains:
  ```go
  Exercises []Exercise `json:"exercises,omitempty"`
  ```
  New `Exercise` type: `{ID, Description, Type, Asset, Template string}` (Asset/Description `omitempty`).
- `ensureGraderTasksConfigMap` (`internal/controller/grader.go`) drops the `graderTasksPlaceholder` constant entirely. It instead marshals `env.Spec.Exercises` into the shape `grader.LoadTasks` expects: `[{id, type, template, asset?}, ...]`.
  - **`description` is deliberately excluded from `tasks.json`** — it's a frontend-only field with no use in the grader.
- If `env.Spec.Exercises` is empty/nil, `tasks.json` is `[]` — a valid empty task list. `grader.Backend` already handles an empty task set (sends `{}` on connect), so no grader change is needed for this case.

## 6. relay/grader: Add `asset` Field

- `grader.Task` (`services/relay/grader/tasks.go`) gains:
  ```go
  Asset string `json:"asset,omitempty"`
  ```
- `LoadTasks` does **not** validate `asset` — it's an opaque pass-through. Validation already happened once, upstream, in labs-sync.py; the grader has no runtime knowledge of `environment.assets` to validate against.
- No behavior change in `grader.Backend` — filtering checks by asset is future work. This design only makes the field flow through end-to-end unused.

## What Is Not Changed

- `labs_userview` PocketBase view definition (field list unchanged).
- Frontend markdown rendering / Vue components — the rewritten placeholder is valid markdown (renders as an inert code block) but no Vue-side parsing of it is added here.
- `grader.Backend` grading logic — `asset` is threaded through but not acted upon.
- Lab YAML top-level structure (`meta`, `content`, `environment`) — exercises are embedded in existing `content` strings, not a new YAML key.

## Testing

- **labs-sync.py**: new pytest test file (e.g. `scripts/test_labs_sync.py`) covering: exercise extraction from a task's markdown, id numbering across multiple tasks/multiple exercises per task, multi-line template body parsing, missing-description/missing-type/invalid-type validation failures, asset-not-found validation failure, and the content-rewrite output (placeholder contains only `id`+`description`).
- **attempt-controller**: extend existing `reconcile.go` tests for `toLabEnvironment` to cover exercises mapping — empty list, populated list, `asset` present vs. omitted.
- **labenv-operator**: extend `ensureGraderTasksConfigMap` Ginkgo tests in `labenvironment_controller_test.go` — replace placeholder-based assertions with exercises-derived `tasks.json` assertions; cover the empty-exercises case (`[]`).
- **grader**: extend `tasks_test.go` for the new optional `asset` field — present, absent, JSON round-trip via `LoadTasks`.
