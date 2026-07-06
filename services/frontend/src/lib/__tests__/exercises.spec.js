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
