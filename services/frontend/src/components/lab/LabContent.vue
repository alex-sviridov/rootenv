<script setup>
import { computed, ref, watch, onMounted } from 'vue'
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

function applyGrades() {
  if (!contentEl.value) return
  contentEl.value.querySelectorAll('[data-exercise-id]').forEach((el) => {
    const id = el.getAttribute('data-exercise-id')
    el.classList.toggle('passed', props.grades[id] === true)
  })
}

onMounted(applyGrades)

watch([html, () => props.grades], applyGrades, { deep: true, flush: 'post' })
</script>

<template>
  <div class="w-full h-full overflow-y-auto p-8">
    <template v-if="task">
      <h1 class="text-xl font-semibold text-white mb-6">{{ task.title }}</h1>
      <div ref="contentEl" class="prose" v-html="html" />
    </template>
    <p v-else class="text-sm text-slate-500">Select a task.</p>
  </div>
</template>

<style scoped>
.prose :deep(h1),
.prose :deep(h2),
.prose :deep(h3) {
  color: #f1f5f9;
  font-weight: 600;
  margin-top: 1.5em;
  margin-bottom: 0.5em;
}
.prose :deep(h1) { font-size: 1.25rem; }
.prose :deep(h2) { font-size: 1.125rem; }
.prose :deep(h3) { font-size: 1rem; }

.prose :deep(p) {
  color: #94a3b8;
  line-height: 1.75;
  margin-bottom: 1em;
}

.prose :deep(ul),
.prose :deep(ol) {
  color: #94a3b8;
  padding-left: 1.5em;
  margin-bottom: 1em;
}
.prose :deep(li) { margin-bottom: 0.25em; }
.prose :deep(ul) { list-style-type: disc; }
.prose :deep(ol) { list-style-type: decimal; }

.prose :deep(code) {
  background: #1e293b;
  color: #e2e8f0;
  padding: 0.15em 0.4em;
  border-radius: 0.25rem;
  font-size: 0.875em;
  font-family: ui-monospace, monospace;
}

.prose :deep(pre) {
  background: #0f172a;
  border: 1px solid #1e293b;
  border-radius: 0.5rem;
  padding: 1rem;
  overflow-x: auto;
  margin-bottom: 1em;
}
.prose :deep(pre code) {
  background: none;
  padding: 0;
  font-size: 0.875rem;
  color: #e2e8f0;
}

.prose :deep(a) { color: #818cf8; }
.prose :deep(a:hover) { color: #a5b4fc; }

.prose :deep(blockquote) {
  border-left: 3px solid #334155;
  padding-left: 1em;
  color: #64748b;
  margin-bottom: 1em;
}

.prose :deep(hr) {
  border-color: #1e293b;
  margin: 1.5em 0;
}

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
</style>
