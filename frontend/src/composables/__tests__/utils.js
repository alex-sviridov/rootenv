import { createApp, defineComponent, h } from 'vue'

// Mounts a composable inside a minimal Vue app so lifecycle hooks fire correctly.
// Returns the composable's return value and an unmount function.
export function withSetup(composableFn) {
  let result
  const app = createApp(defineComponent({
    setup() {
      result = composableFn()
      return () => h('div')
    },
  }))
  const root = document.createElement('div')
  app.mount(root)
  return { result, unmount: () => app.unmount() }
}
