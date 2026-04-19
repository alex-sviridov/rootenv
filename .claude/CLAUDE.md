# General Rules

## Code Style
- Simple and readable over clever; three similar lines beat a forced helper; no unnecessary comments

## Commit Policy
- Logical units only; format `<type>: <what>` (feat/fix/chore/refactor/docs)
- `master` for small changes; branch for multi-session work
- No squash/force-push; no broken code; tests must pass before commit

## Backend Work
Before writing any migration, hook, or API handler that touches collections, read `/home/alex/linuxlab/.claude/memory/memory-backend.md` for the current schema. Update it whenever collections change or the user says they changed the DB structure manually.

## Memory Maintenance
At the start of every session read `/home/alex/linuxlab/.claude/memory/MEMORY.md`.
Write immediately when learned — not at the end. Skip trivial tasks.
Project-wide memory lives at `/home/alex/linuxlab/.claude/memory/` — use it only for cross-module concerns (shared interfaces, project-wide constraints). Each module has its own memory at `<module>/.claude/memory/`; write module-specific decisions there, not here.
- Cross-module interface or constraint → `memory-architecture.md`
- Project-wide decision → `memory-decisions.md`
