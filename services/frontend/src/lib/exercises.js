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
