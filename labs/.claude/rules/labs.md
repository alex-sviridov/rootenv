# Labs

Lab definitions live in `/labs/definitions/`. Backend syncs them into PocketBase on every startup via `scripts/labs-sync.py`. Full schema and exercise-authoring docs: [`labs/README.md`](../../README.md). Grading pipeline end to end: [`docs/lab-grade.md`](../../../docs/lab-grade.md).

## Directory Structure
Each subdirectory → a `folder` record. Each YAML file (except `index.yaml`) → a `lab` record linked to its parent folder via `parent` relation. Nesting is arbitrary.

```
labs/definitions/
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
Fields: `id` (underscore path without extension), `type=lab`, `title`, `description`, `content` (json), `environment` (json), `exercises` (json, never exposed via `labs_userview`), `parent` (id of parent folder)

## Lab YAML Structure
```yaml
meta:
  title: Human-readable lab name
  description: Short description

content:
  - title: Task Title
    content: |
      Markdown body — may embed ```exercise fenced blocks (see README).

environment:
  duration: 90
  assets:
    - name: server-0
      image: ubuntu
      cpu: 100m
      memory: 128Mi
      disk: 5Gi
      protocols:
        - exec
      setup: |
        # optional shell script run once at provision time
```

## Rules
- `meta` and `content` shown to users; `environment` and `exercises` never exposed to frontend
- Task order in `content` = sidebar order
- Each asset in `environment.assets` → one `servers` record per attempt
- Asset `name` is what relay URLs address (e.g. `/relay/exec/<attemptID>/<assetName>/`), not asset order
- IDs must be unique (enforced by path uniqueness)
- `platform` and `ssh_user` asset fields are obsolete — removed from all lab YAML, do not reintroduce (nothing downstream ever read them)

## Memory Maintenance
At the start of any labs work, read `labs/.claude/memory/MEMORY.md`.
Write immediately when a decision, invariant, or preference is discovered — not at session end:
- Architecture invariant → `labs/.claude/memory/memory-architecture.md`
- Implementation decision → `labs/.claude/memory/memory-decisions.md`
- Coding style or workflow preference → `labs/.claude/memory/memory-preferences.md`
Only write to this module's memory. Cross-module concerns go to `.claude/memory/` at the repo root.
