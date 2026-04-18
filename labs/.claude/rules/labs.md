# Labs

Lab definitions live in `/labs/`. Backend syncs them into PocketBase on every startup.

## Directory Structure
Each subdirectory → a `folder` record. Each YAML file (except `index.yaml`) → a `lab` record linked to its parent folder via `parent` relation. Nesting is arbitrary.

```
labs/
  ex200/
    index.yaml         → folder metadata (title, description); not synced as a record itself
    rhcsa1.yaml        → lab id: ex200_rhcsa1, parent: ex200
  networking/
    advanced/
      bgp.yaml         → lab id: networking_advanced_bgp, parent: networking_advanced
```

`index.yaml` keys: `meta.title`, `meta.description`. If absent, folder title defaults to the directory name.

## Record Types

### Folder record (type: folder)
Fields: `id` (underscore path), `type=folder`, `title`, `description`, `parent` (id of parent folder, empty if top-level)

### Lab record (type: lab)
Fields: `id` (underscore path without extension), `type=lab`, `title`, `description`, `content` (json), `environment` (json), `parent` (id of parent folder)

## Lab YAML Structure
```yaml
meta:
  title: Human-readable lab name
  description: Short description

content:
  - title: Task Title
    content: |
      Markdown body.

environment:
  servers:
    - name: server-0
      # fields TBD — never exposed to frontend
```

## Rules
- `meta` and `content` shown to users; `environment` never exposed to frontend
- Task order in `content` = sidebar order
- Each server in `environment.servers` → one `servers` record per attempt
- Server index in relay URL (`/relay/0/`) matches order in `environment.servers`
- IDs must be unique (enforced by path uniqueness)

## Memory Maintenance
At the start of any labs work, read `/home/alex/linuxlab/labs/.claude/memory/MEMORY.md`.
Write immediately when a decision, invariant, or preference is discovered — not at session end:
- Architecture invariant → `/home/alex/linuxlab/labs/.claude/memory/memory-architecture.md`
- Implementation decision → `/home/alex/linuxlab/labs/.claude/memory/memory-decisions.md`
- Coding style or workflow preference → `/home/alex/linuxlab/labs/.claude/memory/memory-preferences.md`
Only write to this module's memory. Cross-module concerns go to `/home/alex/linuxlab/.claude/memory/`.
