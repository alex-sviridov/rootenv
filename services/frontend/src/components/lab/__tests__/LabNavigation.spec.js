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
    const pill = wrapper.findAll('button')[0].find('[title]')
    expect(pill.attributes('title')).toBe('0/2 exercises passed')
  })

  it('shows 1/2 when one of two exercises passes', () => {
    const wrapper = mount(LabNavigation, {
      props: { tasks, selectedTask: 0, grades: { '1.1': true, '1.2': false } },
    })
    const pill = wrapper.findAll('button')[0].find('[title]')
    expect(pill.attributes('title')).toBe('1/2 exercises passed')
  })

  it('shows 2/2 when both exercises pass', () => {
    const wrapper = mount(LabNavigation, {
      props: { tasks, selectedTask: 0, grades: { '1.1': true, '1.2': true } },
    })
    const pill = wrapper.findAll('button')[0].find('[title]')
    expect(pill.attributes('title')).toBe('2/2 exercises passed')
  })

  it('shows no pill for a task with zero exercises', () => {
    const wrapper = mount(LabNavigation, { props: { tasks, selectedTask: 0, grades: {} } })
    const buttons = wrapper.findAll('button')
    expect(buttons[1].find('[title]').exists()).toBe(false)
  })

  it('defaults grades to {} when prop omitted', () => {
    const wrapper = mount(LabNavigation, { props: { tasks, selectedTask: 0 } })
    const pill = wrapper.findAll('button')[0].find('[title]')
    expect(pill.attributes('title')).toBe('0/2 exercises passed')
  })
})
