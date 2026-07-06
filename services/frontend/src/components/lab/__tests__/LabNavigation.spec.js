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
