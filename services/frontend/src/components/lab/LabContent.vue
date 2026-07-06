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
      return `<div class="exercise-card relative flex items-center gap-3 w-full mb-4 pl-5 pr-4 py-3 rounded-lg border border-slate-700 bg-slate-800 overflow-hidden transition-colors" data-exercise-id="${escapeHtml(block.id)}">
        <span class="accent-bar absolute inset-y-0 left-0 w-1 bg-indigo-500"></span>
        <span class="status-icon relative flex items-center justify-center w-5 h-5 shrink-0">
          <span class="icon-circle absolute inset-0 rounded-full border-2 border-slate-500"></span>
          <svg class="icon-check w-5 h-5 text-white opacity-0 scale-50 transition-all" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M5 10.5L8.5 14L15 6.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>
        </span>
        <span class="flex flex-col gap-0.5 min-w-0">
          <span class="eyebrow text-xs font-semibold text-indigo-400 uppercase tracking-wide">Exercise</span>
          <span class="desc text-sm text-slate-300">${escapeHtml(block.description)}</span>
        </span>
      </div>`
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
    const passed = props.grades[id] === true

    el.classList.toggle('passed', passed)
    el.classList.toggle('bg-slate-800', !passed)
    el.classList.toggle('border-slate-700', !passed)
    el.classList.toggle('bg-green-500/10', passed)
    el.classList.toggle('border-green-500/40', passed)

    const circle = el.querySelector('.icon-circle')
    circle?.classList.toggle('border-slate-500', !passed)
    circle?.classList.toggle('border-transparent', passed)
    circle?.classList.toggle('bg-green-500', passed)

    const check = el.querySelector('.icon-check')
    check?.classList.toggle('opacity-0', !passed)
    check?.classList.toggle('scale-50', !passed)
    check?.classList.toggle('opacity-100', passed)
    check?.classList.toggle('scale-100', passed)

    const desc = el.querySelector('.desc')
    desc?.classList.toggle('text-slate-300', !passed)
    desc?.classList.toggle('text-slate-100', passed)

    const accentBar = el.querySelector('.accent-bar')
    accentBar?.classList.toggle('bg-indigo-500', !passed)
    accentBar?.classList.toggle('bg-green-500', passed)

    const eyebrow = el.querySelector('.eyebrow')
    eyebrow?.classList.toggle('text-indigo-400', !passed)
    eyebrow?.classList.toggle('text-green-400', passed)
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

</style>
