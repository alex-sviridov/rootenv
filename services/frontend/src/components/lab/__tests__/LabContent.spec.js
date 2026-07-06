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
