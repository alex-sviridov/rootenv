# Labs

Lab definitions live in `/labs/`. Backend syncs them into PocketBase on every startup.

## Directory Structure
Subdirectories = groups; YAML filename (no extension) = slug.
```
labs/
  rhcsa/
    rhcsa-lab1.yaml    → slug: rhcsa-lab1, group: rhcsa
  networking/
    intro.yaml         → slug: intro, group: networking
```

## YAML Structure
```yaml
meta:
  name: Human-readable lab name
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
- Slugs must be unique across all groups

## Memory Maintenance
Keep `.claude/memory/` up to date: `memory-decisions.md`, `memory-architecture.md`, `memory-preferences.md`.
