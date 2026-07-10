# Frontend Grader Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Connect the frontend to the already-deployed `relay-grader` WebSocket and render each lab exercise as a gray/green badge, with an aggregate pass count per task in the sidebar.

**Architecture:** A new `useGraderConnection` composable (mirroring the existing `useExecRelayConnection` pattern) opens one WebSocket per lab attempt once it reaches `provisioned` state, receiving a `{exerciseId: boolean}` grade map. A shared `parseExerciseBlocks` utility extracts exercise `{id, description}` pairs from task markdown (the same placeholder format `labs-sync.py` writes). `LabContent.vue` renders each `` ```exercise `` block as a badge via a custom `marked` renderer, colored by the live grade map; `LabNavigation.vue` shows a `passed/total` pill per task using the same parsed ids.

**Tech Stack:** Vue 3 (Composition API), `marked` v18, `DOMPurify`, `vitest` + `@vue/test-utils`, native `WebSocket`.

## Global Constraints

- No reconnect/retry logic for the grader socket — connect once, freeze `grades` at last known value on close/error (no error UI), matching `useExecRelayConnection`'s simplicity.
- `relay-grader` itself is unchanged — no grading logic, no protocol changes.
- `labs_userview` / PocketBase unchanged — the frontend only ever sees `task.content` markdown with `id`+`description` placeholders, never the `exercises` field.
- Tailwind utility classes only; custom CSS only when Tailwind can't express it (existing `LabContent.vue` `<style scoped>` block already uses raw CSS for `:deep()` selectors — follow that existing exception, don't introduce new patterns).
- Test-first: write the failing test before implementation for every task.

---

## File Structure

- **Create** `src/lib/exercises.js` — `parseExerciseBlocks(markdown)`, shared by `LabContent.vue` and `LabNavigation.vue`.
- **Create** `src/lib/__tests__/exercises.spec.js` — tests for the above.
- **Create** `src/composables/useGraderConnection.js` — WebSocket client, returns `{ grades, connect, close }`.
- **Create** `src/composables/__tests__/useGraderConnection.spec.js` — tests for the above, mirroring `useExecRelayConnection.spec.js`.
- **Modify** `src/composables/useLabSession.js` — wire `useGraderConnection` lifecycle to attempt provisioning state; expose `grades`.
- **Modify** `src/views/LabView.vue` — pass `grades` down to `LabContent` and `LabNavigation`.
- **Modify** `src/components/lab/LabContent.vue` — custom marked renderer for `` ```exercise `` blocks + grade-driven class toggling.
- **Modify** `src/components/lab/LabNavigation.vue` — `passed/total` pill per task.
- **Create** `src/components/lab/__tests__/LabContent.spec.js` (new test file — no existing tests for this component).
- **Create** `src/components/lab/__tests__/LabNavigation.spec.js` (new test file — no existing tests for this component).

---

### Task 1: `parseExerciseBlocks` utility

**Files:**
- Create: `src/lib/exercises.js`
- Test: `src/lib/__tests__/exercises.spec.js`

**Interfaces:**
- Produces: `parseExerciseBlocks(markdown: string) => Array<{ id: string, description: string }>` — used by Task 4 (`LabContent.vue`) and Task 5 (`LabNavigation.vue`).

The placeholder format written by `labs-sync.py` (per `docs/superpowers/specs/2026-07-04-lab-exercises-design.md`) is:

````
```exercise
id: 2.3
description: Create /tmp/labfile owned by bob
```
````

- [ ] **Step 1: Write the failing test**

```js
// src/lib/__tests__/exercises.spec.js
import { describe, it, expect } from 'vitest'
import { parseExerciseBlocks } from '../exercises'

describe('parseExerciseBlocks', () => {
  it('parses a single exercise block', () => {
    const md = [
      'Some intro text.',
      '',
      '```exercise',
      'id: 1.1',
      'description: Create /tmp/labfile owned by bob',
      '```',
      '',
      'More text.',
    ].join('\n')

    expect(parseExerciseBlocks(md)).toEqual([
      { id: '1.1', description: 'Create /tmp/labfile owned by bob' },
    ])
  })

  it('parses multiple exercise blocks in order', () => {
    const md = [
      '```exercise',
      'id: 1.1',
      'description: First',
      '```',
      'text between',
      '```exercise',
      'id: 1.2',
      'description: Second',
      '```',
    ].join('\n')

    expect(parseExerciseBlocks(md)).toEqual([
      { id: '1.1', description: 'First' },
      { id: '1.2', description: 'Second' },
    ])
  })

  it('ignores non-exercise fenced blocks', () => {
    const md = [
      '```bash',
      'echo hello',
      '```',
      '```exercise',
      'id: 2.1',
      'description: Only this one',
      '```',
    ].join('\n')

    expect(parseExerciseBlocks(md)).toEqual([
      { id: '2.1', description: 'Only this one' },
    ])
  })

  it('skips a block missing id', () => {
    const md = ['```exercise', 'description: No id here', '```'].join('\n')
    expect(parseExerciseBlocks(md)).toEqual([])
  })

  it('skips a block missing description', () => {
    const md = ['```exercise', 'id: 3.1', '```'].join('\n')
    expect(parseExerciseBlocks(md)).toEqual([])
  })

  it('returns an empty array when there are no exercise blocks', () => {
    expect(parseExerciseBlocks('Just plain text, no fences.')).toEqual([])
  })

  it('returns an empty array for empty input', () => {
    expect(parseExerciseBlocks('')).toEqual([])
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/frontend && npx vitest run src/lib/__tests__/exercises.spec.js`
Expected: FAIL — `Cannot find module '../exercises'` (or similar module-not-found error).

- [ ] **Step 3: Write minimal implementation**

```js
// src/lib/exercises.js
const FENCE_RE = /```exercise\n([\s\S]*?)```/g

export function parseExerciseBlocks(markdown) {
  const blocks = []
  let match
  FENCE_RE.lastIndex = 0
  while ((match = FENCE_RE.exec(markdown)) !== null) {
    const body = match[1]
    const idMatch = body.match(/^id:\s*(.+)$/m)
    const descMatch = body.match(/^description:\s*(.+)$/m)
    if (!idMatch || !descMatch) continue
    blocks.push({ id: idMatch[1].trim(), description: descMatch[1].trim() })
  }
  return blocks
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/frontend && npx vitest run src/lib/__tests__/exercises.spec.js`
Expected: PASS (7 tests)

- [ ] **Step 5: Commit**

```bash
cd services/frontend
git add src/lib/exercises.js src/lib/__tests__/exercises.spec.js
git commit -m "feat(frontend): add parseExerciseBlocks utility for exercise placeholders"
```

---

### Task 2: `useGraderConnection` composable

**Files:**
- Create: `src/composables/useGraderConnection.js`
- Test: `src/composables/__tests__/useGraderConnection.spec.js`

**Interfaces:**
- Consumes: `pb.authStore.token` from `@/lib/pb` (same as `useExecRelayConnection.js`).
- Produces: `useGraderConnection(attemptId: string) => { grades: Ref<Record<string, boolean>>, connect: () => void, close: () => void }` — used by Task 3 (`useLabSession.js`).

This composable does **not** use `onMounted`/`onUnmounted` internally (unlike `useExecRelayConnection`) — `connect()`/`close()` are called explicitly by `useLabSession` based on attempt provisioning state, since the grader connection's lifecycle is tied to attempt state, not component mount.

- [ ] **Step 1: Write the failing test**

```js
// src/composables/__tests__/useGraderConnection.spec.js
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

const { mockToken } = vi.hoisted(() => ({ mockToken: 'test-token' }))

vi.mock('@/lib/pb', () => ({
  pb: { authStore: { get token() { return mockToken } } },
}))

import { useGraderConnection } from '../useGraderConnection'

class MockWebSocket {
  constructor(url) {
    this.url = url
    this.readyState = WebSocket.CONNECTING
    this.sent = []
    MockWebSocket.lastInstance = this
  }
  send(data) { this.sent.push(data) }
  close(code, reason) { this._closedWith = { code, reason } }
}
MockWebSocket.CONNECTING = 0
MockWebSocket.OPEN = 1
MockWebSocket.CLOSING = 2
MockWebSocket.CLOSED = 3

beforeEach(() => {
  MockWebSocket.lastInstance = null
  vi.stubGlobal('WebSocket', MockWebSocket)
  vi.stubGlobal('location', { protocol: 'http:', host: 'localhost:8080' })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useGraderConnection', () => {
  it('opens WebSocket at /relay/grade/<attemptId>/ on connect()', () => {
    const { connect } = useGraderConnection('atm_123')
    connect()

    expect(MockWebSocket.lastInstance.url).toBe('ws://localhost:8080/relay/grade/atm_123/')
  })

  it('opens wss when protocol is https', () => {
    vi.stubGlobal('location', { protocol: 'https:', host: 'example.com' })
    const { connect } = useGraderConnection('atm_123')
    connect()

    expect(MockWebSocket.lastInstance.url).toBe('wss://example.com/relay/grade/atm_123/')
  })

  it('sets pb_auth cookie before connecting', () => {
    let setCookieValue = null
    const originalDescriptor = Object.getOwnPropertyDescriptor(document, 'cookie')
    Object.defineProperty(document, 'cookie', {
      set(value) { setCookieValue = value },
      configurable: true,
    })

    try {
      const { connect } = useGraderConnection('atm_123')
      connect()
      expect(setCookieValue).toContain('pb_auth=test-token')
    } finally {
      if (originalDescriptor) Object.defineProperty(document, 'cookie', originalDescriptor)
    }
  })

  it('sends the token as the first message on open', () => {
    const { connect } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onopen()

    expect(MockWebSocket.lastInstance.sent).toEqual([mockToken])
  })

  it('populates grades from a JSON message', () => {
    const { connect, grades } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': true, '1.2': false }) })

    expect(grades.value).toEqual({ '1.1': true, '1.2': false })
  })

  it('replaces the whole grades map on each message', () => {
    const { connect, grades } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': false }) })
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': true, '2.1': true }) })

    expect(grades.value).toEqual({ '1.1': true, '2.1': true })
  })

  it('does not throw and leaves grades unchanged on close', () => {
    const { connect, grades } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': true }) })
    expect(() => MockWebSocket.lastInstance.onclose({ code: 1000, reason: '' })).not.toThrow()
    expect(grades.value).toEqual({ '1.1': true })
  })

  it('does not throw and leaves grades unchanged on error', () => {
    const { connect, grades } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': true }) })
    expect(() => MockWebSocket.lastInstance.onerror()).not.toThrow()
    expect(grades.value).toEqual({ '1.1': true })
  })

  it('close() closes the socket with code 1000 when open', () => {
    const { connect, close } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    close()

    expect(MockWebSocket.lastInstance._closedWith).toEqual({ code: 1000, reason: 'session ended' })
  })

  it('close() is a no-op if never connected', () => {
    const { close } = useGraderConnection('atm_123')
    expect(() => close()).not.toThrow()
  })

  it('close() is safe to call twice', () => {
    const { connect, close } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    close()
    expect(() => close()).not.toThrow()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/frontend && npx vitest run src/composables/__tests__/useGraderConnection.spec.js`
Expected: FAIL — `Cannot find module '../useGraderConnection'`

- [ ] **Step 3: Write minimal implementation**

```js
// src/composables/useGraderConnection.js
import { ref } from 'vue'
import { pb } from '@/lib/pb'

export function useGraderConnection(attemptId) {
  const grades = ref({})
  let ws = null

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/relay/grade/${attemptId}/`
    document.cookie = `pb_auth=${pb.authStore.token}; SameSite=Strict; Secure; path=/`
    ws = new WebSocket(url)

    ws.onopen = () => {
      ws.send(pb.authStore.token)
    }

    ws.onmessage = (e) => {
      grades.value = JSON.parse(e.data)
    }

    ws.onclose = () => {
      // grades frozen at last known value; no error UI
    }

    ws.onerror = () => {
      // grades frozen at last known value; no error UI
    }
  }

  function close() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      ws.close(1000, 'session ended')
    }
    ws = null
  }

  return { grades, connect, close }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/frontend && npx vitest run src/composables/__tests__/useGraderConnection.spec.js`
Expected: PASS (11 tests)

- [ ] **Step 5: Commit**

```bash
cd services/frontend
git add src/composables/useGraderConnection.js src/composables/__tests__/useGraderConnection.spec.js
git commit -m "feat(frontend): add useGraderConnection composable for relay-grader WebSocket"
```

---

### Task 3: Wire grader lifecycle into `useLabSession`

**Files:**
- Modify: `src/composables/useLabSession.js`

**Interfaces:**
- Consumes: `useGraderConnection(attemptId)` from Task 2 → `{ grades, connect, close }`.
- Produces: `useLabSession()` return value gains a `grades` field (`Ref<Record<string, boolean>>`) — used by Task 6 (`LabView.vue`).

There is no existing test file for `useLabSession.js` (verify with `find src/composables/__tests__ -iname "*labsession*"` — none found during research). This task does not introduce one; it's thin wiring over an already-tested composable (Task 2) and an already-tested store (`useAttemptsStore`). Manual verification is via Task 6/7's component tests plus the `run` skill at the end of this plan.

- [ ] **Step 1: Read current file to confirm exact context**

Run: `cd services/frontend && cat -n src/composables/useLabSession.js`

Confirm the current shape matches:

```js
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { useBreadcrumbsStore } from '@/stores/breadcrumbs'
import { useAttemptsStore } from '@/stores/attempts'
import { fetchLab } from '@/api/labs'
import { useTerminalTabs } from '@/composables/useTerminalTabs'

export function useLabSession() {
  const route = useRoute()
  const breadcrumbs = useBreadcrumbsStore()
  const attemptsStore = useAttemptsStore()

  const lab = ref(null)
  const selectedTask = ref(0)
  const error = ref(null)

  const { tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab, resetTabs } = useTerminalTabs()

  const attemptId = computed(() => attemptsStore.lastAttempt?.id ?? null)

  const currentTask = computed(() => lab.value?.content?.[selectedTask.value] ?? null)

  watch(() => attemptsStore.lastAttempt?.id, async (id) => {
    await attemptsStore.stopWatching()
    resetTabs()
    const attempt = attemptsStore.lastAttempt
    if (id && attempt?.current_state !== 'decommissioned') {
      await attemptsStore.startWatching(id)
    }
  })

  // ... initLab, onMounted, onUnmounted, return
}
```

- [ ] **Step 2: Add the grader wiring**

Modify `src/composables/useLabSession.js`:

```js
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { useBreadcrumbsStore } from '@/stores/breadcrumbs'
import { useAttemptsStore } from '@/stores/attempts'
import { fetchLab } from '@/api/labs'
import { useTerminalTabs } from '@/composables/useTerminalTabs'
import { useGraderConnection } from '@/composables/useGraderConnection'

export function useLabSession() {
  const route = useRoute()
  const breadcrumbs = useBreadcrumbsStore()
  const attemptsStore = useAttemptsStore()

  const lab = ref(null)
  const selectedTask = ref(0)
  const error = ref(null)

  const { tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab, resetTabs } = useTerminalTabs()

  const attemptId = computed(() => attemptsStore.lastAttempt?.id ?? null)

  const currentTask = computed(() => lab.value?.content?.[selectedTask.value] ?? null)

  let graderConnection = null
  let graderConnectedForId = null
  const grades = ref({})

  watch(() => attemptsStore.lastAttempt?.id, async (id) => {
    await attemptsStore.stopWatching()
    resetTabs()
    const attempt = attemptsStore.lastAttempt
    if (id && attempt?.current_state !== 'decommissioned') {
      await attemptsStore.startWatching(id)
    }
  })

  watch(() => attemptsStore.lastAttempt?.current_state, (state) => {
    const id = attemptsStore.lastAttempt?.id
    if (state === 'provisioned' && id && graderConnectedForId !== id) {
      graderConnection?.close()
      graderConnection = useGraderConnection(id)
      grades.value = graderConnection.grades.value
      watch(graderConnection.grades, (g) => { grades.value = g })
      graderConnection.connect()
      graderConnectedForId = id
    } else if (state !== 'provisioned' && graderConnection) {
      graderConnection.close()
      graderConnection = null
      graderConnectedForId = null
      grades.value = {}
    }
  })

  async function initLab(group, slug) {
    lab.value = null
    error.value = null
    selectedTask.value = 0
    resetTabs()
    try {
      lab.value = await fetchLab(`${group}_${slug}`)

      breadcrumbs.set([
        { label: 'LinuxLab', to: '/' },
        lab.value.group_title ? { label: lab.value.group_title, to: `/labs/${group}` } : null,
        { label: lab.value.title },
      ].filter(Boolean))

      await attemptsStore.stopWatching()
      await Promise.all([
        attemptsStore.loadLastAttempt(lab.value.id),
        attemptsStore.loadActiveAttempt(),
      ])
    } catch {
      error.value = 'Lab not found.'
    }
  }

  onMounted(() => {
    const { group, slug } = route.params
    if (group && slug) initLab(group, slug)
  })

  onUnmounted(async () => {
    await attemptsStore.stopWatching()
    graderConnection?.close()
  })

  return {
    lab, selectedTask, currentTask, error,
    tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab,
    attemptId, grades,
  }
}
```

**Why the nested `watch(graderConnection.grades, ...)` instead of returning `graderConnection.grades` directly:** each `useGraderConnection(id)` call creates a fresh `ref`. If we returned that ref directly from `useLabSession`, re-provisioning the same lab (new attempt id) would swap which ref template consumers hold, breaking Vue's reactivity for existing template bindings. Keeping one stable `grades` ref in `useLabSession` and copying values into it avoids that.

- [ ] **Step 3: Manually verify no syntax errors**

Run: `cd services/frontend && npx vite build --mode development 2>&1 | tail -20`
Expected: build succeeds (no syntax/import errors). This is not a full correctness check — full verification happens via Task 6/7 tests and the end-to-end check in the final task.

- [ ] **Step 4: Commit**

```bash
cd services/frontend
git add src/composables/useLabSession.js
git commit -m "feat(frontend): open grader connection when attempt becomes provisioned"
```

---

### Task 4: Exercise badge rendering in `LabContent.vue`

**Files:**
- Modify: `src/components/lab/LabContent.vue`
- Create: `src/components/lab/__tests__/LabContent.spec.js`

**Interfaces:**
- Consumes: `parseExerciseBlocks(markdown) => Array<{id, description}>` from Task 1 — the renderer calls it on the single block's raw text (`` ```exercise\n${text}``` ``) to get one `{id, description}` pair.
- Consumes new prop: `grades: Object` (default `{}`).

Current file (for exact context):

```vue
<script setup>
import { computed } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'

const { task } = defineProps({
  task: { type: Object, default: null },
})

const html = computed(() => {
  if (!task) return ''
  const dirty = marked.parse(task.content)
  return DOMPurify.sanitize(dirty)
})
</script>
```

- [ ] **Step 1: Write the failing test**

```js
// src/components/lab/__tests__/LabContent.spec.js
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import LabContent from '../LabContent.vue'

const taskWithExercise = {
  title: 'Task 1',
  content: [
    'Do the thing.',
    '',
    '```exercise',
    'id: 1.1',
    'description: Create /tmp/labfile owned by bob',
    '```',
  ].join('\n'),
}

describe('LabContent exercise badges', () => {
  it('renders a badge with the exercise description', () => {
    const wrapper = mount(LabContent, { props: { task: taskWithExercise, grades: {} } })
    const badge = wrapper.find('[data-exercise-id="1.1"]')

    expect(badge.exists()).toBe(true)
    expect(badge.text()).toContain('Create /tmp/labfile owned by bob')
  })

  it('renders the badge as not-passed (gray) when grades has no entry', () => {
    const wrapper = mount(LabContent, { props: { task: taskWithExercise, grades: {} } })
    const badge = wrapper.find('[data-exercise-id="1.1"]')

    expect(badge.classes()).not.toContain('passed')
  })

  it('renders the badge as not-passed when grades has false for that id', () => {
    const wrapper = mount(LabContent, { props: { task: taskWithExercise, grades: { '1.1': false } } })
    const badge = wrapper.find('[data-exercise-id="1.1"]')

    expect(badge.classes()).not.toContain('passed')
  })

  it('renders the badge as passed (green) when grades has true for that id', () => {
    const wrapper = mount(LabContent, { props: { task: taskWithExercise, grades: { '1.1': true } } })
    const badge = wrapper.find('[data-exercise-id="1.1"]')

    expect(badge.classes()).toContain('passed')
  })

  it('updates badge class reactively when grades prop changes', async () => {
    const wrapper = mount(LabContent, { props: { task: taskWithExercise, grades: {} } })
    expect(wrapper.find('[data-exercise-id="1.1"]').classes()).not.toContain('passed')

    await wrapper.setProps({ grades: { '1.1': true } })
    expect(wrapper.find('[data-exercise-id="1.1"]').classes()).toContain('passed')
  })

  it('renders nothing exercise-related when task has no exercise blocks', () => {
    const wrapper = mount(LabContent, {
      props: { task: { title: 'Plain', content: 'Just text.' }, grades: {} },
    })
    expect(wrapper.find('.exercise-badge').exists()).toBe(false)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/frontend && npx vitest run src/components/lab/__tests__/LabContent.spec.js`
Expected: FAIL — `grades` prop doesn't exist / badge not found (component doesn't render `[data-exercise-id]` yet).

- [ ] **Step 3: Implement the custom renderer and grade-driven classes**

Modify `src/components/lab/LabContent.vue` script section:

```vue
<script setup>
import { computed, ref, watch } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { parseExerciseBlocks } from '@/lib/exercises'

const props = defineProps({
  task: { type: Object, default: null },
  grades: { type: Object, default: () => ({}) },
})

function escapeHtml(str) {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

marked.use({
  renderer: {
    code({ text, lang }) {
      if (lang !== 'exercise') return false
      const [block] = parseExerciseBlocks('```exercise\n' + text + '\n```')
      if (!block) return false
      return `<div class="exercise-badge" data-exercise-id="${escapeHtml(block.id)}"><span class="dot" /><span class="desc">${escapeHtml(block.description)}</span></div>`
    },
  },
})

const contentEl = ref(null)

const html = computed(() => {
  if (!props.task) return ''
  const dirty = marked.parse(props.task.content)
  return DOMPurify.sanitize(dirty, { ADD_ATTR: ['data-exercise-id'] })
})

watch(
  [html, () => props.grades],
  () => {
    if (!contentEl.value) return
    contentEl.value.querySelectorAll('[data-exercise-id]').forEach((el) => {
      const id = el.getAttribute('data-exercise-id')
      el.classList.toggle('passed', props.grades[id] === true)
    })
  },
  { immediate: true, deep: true, flush: 'post' },
)
</script>
```

Modify the template to attach the ref:

```vue
<template>
  <div class="w-full h-full overflow-y-auto p-8">
    <template v-if="task">
      <h1 class="text-xl font-semibold text-white mb-6">{{ task.title }}</h1>
      <div ref="contentEl" class="prose" v-html="html" />
    </template>
    <p v-else class="text-sm text-slate-500">Select a task.</p>
  </div>
</template>
```

Add badge styles to the existing `<style scoped>` block (append, don't replace existing rules):

```css
.prose :deep(.exercise-badge) {
  display: inline-flex;
  align-items: center;
  gap: 0.5em;
  padding: 0.35em 0.75em;
  border-radius: 999px;
  background: #1e293b;
  margin-bottom: 1em;
}
.prose :deep(.exercise-badge .dot) {
  width: 0.5em;
  height: 0.5em;
  border-radius: 50%;
  background: #64748b;
  flex-shrink: 0;
}
.prose :deep(.exercise-badge .desc) {
  color: #cbd5e1;
  font-size: 0.875rem;
}
.prose :deep(.exercise-badge.passed) {
  background: rgba(74, 222, 128, 0.15);
}
.prose :deep(.exercise-badge.passed .dot) {
  background: #4ade80;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/frontend && npx vitest run src/components/lab/__tests__/LabContent.spec.js`
Expected: PASS (6 tests)

- [ ] **Step 5: Run the full frontend test suite to check for regressions**

Run: `cd services/frontend && npx vitest run`
Expected: all existing tests still pass (the `marked.use()` call is additive — non-`exercise` fenced blocks return `false` and fall through to default rendering, so existing markdown rendering is unaffected).

- [ ] **Step 6: Commit**

```bash
cd services/frontend
git add src/components/lab/LabContent.vue src/components/lab/__tests__/LabContent.spec.js
git commit -m "feat(frontend): render exercise placeholders as gray/green badges in LabContent"
```

---

### Task 5: Aggregate pass count in `LabNavigation.vue`

**Files:**
- Modify: `src/components/lab/LabNavigation.vue`
- Create: `src/components/lab/__tests__/LabNavigation.spec.js`

**Interfaces:**
- Consumes: `parseExerciseBlocks` from Task 1.
- Consumes new prop: `grades: Object` (default `{}`).

Current file (for exact context):

```vue
<script setup>
defineProps({
  tasks: { type: Array, required: true },
  selectedTask: { type: Number, required: true },
})

const emit = defineEmits(['selectTask'])
</script>
```

- [ ] **Step 1: Write the failing test**

```js
// src/components/lab/__tests__/LabNavigation.spec.js
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import LabNavigation from '../LabNavigation.vue'

const tasks = [
  {
    title: 'Task with two exercises',
    content: [
      '```exercise', 'id: 1.1', 'description: First', '```',
      '```exercise', 'id: 1.2', 'description: Second', '```',
    ].join('\n'),
  },
  {
    title: 'Task with no exercises',
    content: 'Just reading material.',
  },
]

describe('LabNavigation exercise pill', () => {
  it('shows 0/2 when no exercises are graded', () => {
    const wrapper = mount(LabNavigation, { props: { tasks, selectedTask: 0, grades: {} } })
    expect(wrapper.text()).toContain('0/2')
  })

  it('shows 1/2 when one of two exercises passes', () => {
    const wrapper = mount(LabNavigation, {
      props: { tasks, selectedTask: 0, grades: { '1.1': true, '1.2': false } },
    })
    expect(wrapper.text()).toContain('1/2')
  })

  it('shows 2/2 when both exercises pass', () => {
    const wrapper = mount(LabNavigation, {
      props: { tasks, selectedTask: 0, grades: { '1.1': true, '1.2': true } },
    })
    expect(wrapper.text()).toContain('2/2')
  })

  it('shows no pill for a task with zero exercises', () => {
    const wrapper = mount(LabNavigation, { props: { tasks, selectedTask: 0, grades: {} } })
    const buttons = wrapper.findAll('button')
    expect(buttons[1].text()).not.toMatch(/\d\/\d/)
  })

  it('defaults grades to {} when prop omitted', () => {
    const wrapper = mount(LabNavigation, { props: { tasks, selectedTask: 0 } })
    expect(wrapper.text()).toContain('0/2')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/frontend && npx vitest run src/components/lab/__tests__/LabNavigation.spec.js`
Expected: FAIL — pill text `0/2` not found (component doesn't compute or render it yet).

- [ ] **Step 3: Implement**

Modify `src/components/lab/LabNavigation.vue`:

```vue
<script setup>
import { parseExerciseBlocks } from '@/lib/exercises'

const props = defineProps({
  tasks: { type: Array, required: true },
  selectedTask: { type: Number, required: true },
  grades: { type: Object, default: () => ({}) },
})

const emit = defineEmits(['selectTask'])

function exerciseSummary(task) {
  const ids = parseExerciseBlocks(task.content ?? '').map((b) => b.id)
  if (ids.length === 0) return null
  const passed = ids.filter((id) => props.grades[id] === true).length
  return { passed, total: ids.length }
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <div class="px-4 py-3 border-b border-slate-800">
      <p class="text-xs font-semibold text-slate-500 uppercase tracking-widest">Tasks</p>
    </div>
    <div>
      <button
        v-for="(task, i) in tasks"
        :key="i"
        class="w-full flex items-center gap-2 text-left px-4 py-2.5 text-sm border-l-2 transition-colors"
        :class="i === selectedTask
          ? 'border-indigo-500 bg-slate-800 text-white'
          : 'border-transparent text-slate-400 hover:text-white hover:bg-slate-800/50'"
        @click="emit('selectTask', i)"
      >
        <span class="flex-1 truncate">{{ task.title }}</span>
        <span
          v-if="exerciseSummary(task)"
          class="text-xs font-medium shrink-0"
          :class="exerciseSummary(task).passed === exerciseSummary(task).total
            ? 'text-green-400'
            : 'text-slate-500'"
        >{{ exerciseSummary(task).passed }}/{{ exerciseSummary(task).total }}</span>
      </button>
    </div>
  </div>
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/frontend && npx vitest run src/components/lab/__tests__/LabNavigation.spec.js`
Expected: PASS (5 tests)

- [ ] **Step 5: Run the full frontend test suite to check for regressions**

Run: `cd services/frontend && npx vitest run`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
cd services/frontend
git add src/components/lab/LabNavigation.vue src/components/lab/__tests__/LabNavigation.spec.js
git commit -m "feat(frontend): show exercise pass count pill per task in LabNavigation"
```

---

### Task 6: Pass `grades` through `LabView.vue`

**Files:**
- Modify: `src/views/LabView.vue`

**Interfaces:**
- Consumes: `grades` from `useLabSession()` (Task 3).
- Consumes: `grades` prop on `LabContent` (Task 4) and `LabNavigation` (Task 5).

Current relevant lines (from `useLabSession` destructure and template):

```js
const {
  lab, selectedTask, currentTask, error,
  tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab,
  attemptId,
} = useLabSession()
```

```vue
<LabNavigation :tasks="labTasks" :selected-task="selectedTask" @select-task="selectedTask = $event" />
...
<LabContent :task="currentTask" />
```

- [ ] **Step 1: No test needed for this task**

This is prop-threading only — Task 4 and Task 5's component tests already verify the `grades` prop is consumed correctly by each child in isolation. `useLabSession`'s wiring is covered by Task 3. Wire it and verify with a build + the end-to-end check in the final task.

- [ ] **Step 2: Update the destructure**

```js
const {
  lab, selectedTask, currentTask, error,
  tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab,
  attemptId, grades,
} = useLabSession()
```

- [ ] **Step 3: Pass `grades` to both children**

```vue
<LabNavigation :tasks="labTasks" :selected-task="selectedTask" :grades="grades" @select-task="selectedTask = $event" />
```

```vue
<LabContent :task="currentTask" :grades="grades" />
```

- [ ] **Step 4: Verify build succeeds**

Run: `cd services/frontend && npx vite build --mode development 2>&1 | tail -20`
Expected: build succeeds, no missing-prop warnings in output.

- [ ] **Step 5: Run the full frontend test suite**

Run: `cd services/frontend && npx vitest run`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
cd services/frontend
git add src/views/LabView.vue
git commit -m "feat(frontend): thread grader grades from useLabSession into LabView children"
```

---

### Task 7: End-to-end verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full test suite one more time**

Run: `cd services/frontend && npx vitest run`
Expected: all tests pass (existing + new from Tasks 1, 2, 4, 5).

- [ ] **Step 2: Invoke the `run` skill / start the dev environment and manually verify**

Use the `run` skill (or the project's existing dev workflow, e.g. `skaffold dev` per `skaffold.yaml`) to start the full stack. Provision a lab that has exercise blocks in its markdown (e.g. `labs/ex200/rhcsa1` per the recent `feat(labs): add exercises to ex200/rhcsa1 for end-to-end testing` commit). Confirm in the browser:

1. Exercise badges render inline in the task content, initially gray.
2. The sidebar task list shows a `0/N` pill for tasks with exercises.
3. Opening the browser devtools Network tab shows a WS connection to `/relay/grade/<attemptId>/` opening once the attempt reaches `provisioned` state.
4. No console errors on connect/disconnect (navigating away from the lab, or decommissioning the attempt, closes the socket cleanly).

Since `relay-grader` always reports `grade: false` today (per the bootstrap design's explicit non-goal), badges will stay gray in this manual check — that's expected. The end-to-end check confirms wiring, not grading correctness.

- [ ] **Step 3: Report findings**

If all checks in Step 2 pass, the feature is complete. If any check fails, treat it as a bug in one of Tasks 1–6 — use `superpowers:systematic-debugging` rather than patching around it here.
