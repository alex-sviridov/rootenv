# Frontend grader wiring design

**Date:** 2026-07-06
**Branch:** feat/relay-grader
**Scope:** `services/frontend`

## Problem

`relay-grader` is deployed with an ingress route (`/relay/grade/<attemptId>/`) and reports, on connect, a JSON grade map (`{"<taskId>": false, ...}`) for every exercise loaded from `tasks.json`. Lab task markdown already contains rewritten `` ```exercise `` placeholder blocks (`id` + `description` only, written by `labs-sync.py`) but the frontend renders them as inert code blocks today — no connection to the grader, no visual distinction between passed/unpassed exercises.

This design wires the frontend to relay-grader and renders each exercise as a badge (gray = not graded/failed, green = passed), plus an aggregate pass count per task in the sidebar.

**Out of scope:**
- Any grading logic changes on relay-grader — it keeps reporting `false` for everything; that's a separate future change.
- Live re-grade-on-demand protocol messages (grader design already excludes this).
- Any labenv-operator/backend changes — relay-grader is already deployed with ingress per the current branch state.

## Architecture

```
useLabSession (per lab route)
  │
  │  watch(attempt.current_state)
  │  → 'provisioned': useGraderConnection(attemptId).connect()
  │  → decommissioned/unmount: close()
  ▼
useGraderConnection
  │  wss://<host>/relay/grade/<attemptId>/
  │  auth: pb_auth cookie + first-message token (same as useExecRelayConnection)
  │  onmessage: JSON.parse → grades.value = { [exerciseId]: boolean }
  │  onclose/onerror: log only, grades frozen at last value
  ▼
grades (reactive ref, exposed from useLabSession)
  │
  ├──► LabContent.vue    — per-exercise badge inline in rendered markdown
  └──► LabNavigation.vue — aggregate pass count per task in sidebar
```

## 1. `useGraderConnection` composable

New file: `src/composables/useGraderConnection.js`, modeled on `useExecRelayConnection.js`:

- `useGraderConnection(attemptId)` returns `{ grades }` — `grades` is a `ref({})`.
- `connect()`: builds URL `` `${proto}://${location.host}/relay/grade/${attemptId}/` ``, sets `pb_auth` cookie, opens WS, sends `pb.authStore.token` as first message on `onopen` (matches relay-exec's auth pattern; relay-grader auth headers are injected by the operator, but the client still discards/sends a first message per the documented handler contract).
- `onmessage`: `grades.value = JSON.parse(e.data)` — replaces the whole map (bootstrap sends one message; if the grader ever sends more, each replaces wholesale).
- `onclose` / `onerror`: `console.error`/`console.warn` only. `grades` is left at its last value — no error UI, no reconnect loop (matches relay-exec's connect-once behavior).
- `close()`: closes the socket with code 1000 if open/connecting, safe to call multiple times.
- No terminal, no resize/control-byte framing — this is JSON text messages only.

## 2. Lifecycle wiring in `useLabSession`

- `useLabSession` creates `const graderConnection = useGraderConnection(attemptId)` — `attemptId` is already a computed in scope.
- Extend the existing `watch(() => attemptsStore.lastAttempt?.id, ...)` block (or add a sibling `watch(() => attemptsStore.lastAttempt?.current_state, ...)`) so that:
  - `current_state === 'provisioned'` → `graderConnection.connect()` (only if not already connected for this attempt).
  - `current_state === 'decommissioned'` or attempt changes/clears → `graderConnection.close()`.
- `onUnmounted`: also call `graderConnection.close()`.
- Return `grades: graderConnection.grades` from `useLabSession`, alongside existing returned state.
- `LabView.vue` (or wherever `LabContent`/`LabNavigation` are mounted, i.e. `LabView.vue` per current structure) passes `grades` down as a prop to both components.

## 3. Shared parsing utility

New file: `src/lib/exercises.js`:

```js
export function parseExerciseBlocks(markdown) {
  // scans for ```exercise ... ``` fenced blocks, extracts id/description
  // lines (plain "key: value" scan, mirrors labs-sync.py's rewritten
  // placeholder format), returns [{ id, description }, ...] in order.
}
```

- Used by both `LabContent.vue` (badge rendering) and `LabNavigation.vue` (aggregate counts) so the placeholder format is parsed in exactly one place.
- Malformed blocks (missing `id` or `description`) are skipped, not errored — rendering must not break on a bad block.

## 4. Badge rendering in `LabContent.vue`

- Register a custom `marked` renderer instance (module-level, created once) overriding `renderer.code(code, infostring)`: when `infostring === 'exercise'`, parse the block body via the same line-scan as `parseExerciseBlocks` (single-block variant) and emit:
  ```html
  <div class="exercise-badge" data-exercise-id="2.3">
    <span class="dot" />
    <span class="desc">Create /tmp/labfile owned by bob</span>
  </div>
  ```
  Other fenced blocks fall through to `marked`'s default code rendering (unchanged).
- The `html` computed becomes:
  ```js
  const html = computed(() => {
    if (!task) return ''
    const dirty = marked.parse(task.content, { renderer })
    return DOMPurify.sanitize(dirty, { ADD_ATTR: ['data-exercise-id'] })
  })
  ```
  (DOMPurify strips unknown data-* attributes by default in some configs — explicitly allow `data-exercise-id`.)
- Grade coloring is applied via a second computed/watch, not baked into the renderer (the renderer only knows the id, not live grade state, and re-running `marked.parse` on every grade tick is wasteful): after `html` is set via `v-html`, a `watch(grades, ..., { immediate: true })` walks `el.querySelectorAll('[data-exercise-id]')` and toggles a `.passed` class per element based on `grades.value[id] === true`. This requires a template ref on the container div.
- CSS (scoped, added to existing `<style scoped>` block): `.exercise-badge` — flex row, rounded pill, gray dot/background by default; `.exercise-badge.passed` — green dot/background. Reuses the same green (`#4ade80`/`bg-green-400`) and slate grays already used in `labStates.js` for visual consistency.

## 5. Aggregate indicator in `LabNavigation.vue`

- New prop: `grades: { type: Object, default: () => ({}) }`.
- For each task, compute exercise ids via `parseExerciseBlocks(task.content)`, then:
  ```js
  const passed = ids.filter(id => grades[id] === true).length
  const total = ids.length
  ```
- Render a small pill next to the task title only when `total > 0`: `{{ passed }}/{{ total }}`, colored green when `passed === total`, slate otherwise — same visual language as the `serverStateConfig`/`serverStatusConfig` pattern (colored text, no new config map needed since it's just two states: complete vs. not).

## What is NOT changed

- `relay-grader` itself — no grading logic, no protocol changes.
- `labs_userview` / PocketBase — `exercises` field still not exposed; frontend only ever sees `content` (with `id`+`description` placeholders).
- `useExecRelayConnection.js` — untouched, grader connection is fully separate.
- No reconnect/retry logic for the grader socket — matches existing relay-exec simplicity.

## Testing

- `useGraderConnection.spec.js`: mirrors `useExecRelayConnection.spec.js` — mock `WebSocket`, assert connection URL, `pb_auth` cookie set, first-message token send, `grades` populated from a mock `onmessage`, `close()` behavior, and that `onclose`/`onerror` don't throw or set any error state.
- `exercises.spec.js`: `parseExerciseBlocks` fixtures — single block, multiple blocks across content, malformed block (missing `id` or `description`) skipped, no blocks returns `[]`.
- `LabContent.spec.js` (new): given a task with exercise blocks and a `grades` prop, asserts badge markup renders with correct `passed`/gray class per id, and that grade changes (prop update) re-toggle classes without re-parsing markdown unnecessarily.
- `LabNavigation.spec.js` (new): given tasks with varying exercise counts and a `grades` map, asserts the pill shows correct `passed/total` and color state; tasks with no exercises show no pill.
