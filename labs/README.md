# Labs

Lab definitions for LinuxLab. Everything under `definitions/` is synced into PocketBase on every backend startup via `scripts/labs-sync.py`.

Validate without syncing:

```sh
python3 scripts/labs-sync.py --verify labs/definitions
```

## Directory structure

Each subdirectory under `definitions/` becomes a `folder` record; each YAML file in it (except `index.yaml`) becomes a `lab` record linked to that folder. Nesting is arbitrary.

```
labs/definitions/
  ex200/
    index.yaml       → folder metadata for "ex200"
    rhcsa1.yaml       → lab id: ex200_rhcsa1, parent: ex200
    rhcsa2.yaml       → lab id: ex200_rhcsa2, parent: ex200
  networking/
    advanced/
      bgp.yaml        → lab id: networking_advanced_bgp, parent: networking_advanced
```

- **Lab id** = the underscore-joined path from `definitions/`, without the `.yaml` extension.
- **`index.yaml`** — optional per folder. Keys: `meta.title`, `meta.description`. If absent, the folder's title defaults to the directory name. Not synced as its own record.
- IDs must be unique — enforced by path uniqueness, since two files can't share a path.

## Lab YAML schema

```yaml
meta:
  title: Human-readable lab name       # required
  description: Short description        # optional

content:                                # required, non-empty list; order = sidebar order
  - title: Task Title                   # required
    content: |                          # required — markdown body, may include ```exercise blocks (see below)
      Markdown body.

environment:
  duration: 90                          # minutes; how long an attempt's VMs live before auto-decommission
  assets:                               # required, non-empty list
    - name: server-0                    # required, unique within the lab
      image: ubuntu                     # required — container image name
      cpu: 100m                         # required
      memory: 128Mi                     # required
      disk: 5Gi                         # required
      protocols:                        # required — relay protocols this asset exposes
        - exec
      setup: |                          # optional — shell script run once at provision time
        useradd -m -s /bin/bash lab
```

Rules:
- `meta` and `content` are shown to users; `environment` is never exposed to the frontend (see `labs_userview` in the backend).
- Task order in `content` is the sidebar order.
- Each entry in `environment.assets` becomes one `servers` record per attempt; the asset's `name` is what the frontend/relay uses to address that server (e.g. in `/relay/exec/<attemptID>/<assetName>/`).
- `protocols` currently only has one real value in use: `exec` (proxied via relay-exec's `kubectl exec`).
- `setup` is a shell script executed once when the asset's container starts — use it for anything that needs to exist before the student's first command (users, keys, seeded files).

## Exercises (auto-grading)

Any task's `content` markdown can embed exercises as fenced code blocks with the info-string `exercise`:

````markdown
```exercise
description: Create /tmp/labfile owned by bob
type: term
asset: server-0
template:
chown\s+bob\s+/tmp/labfile
```
````

- **`description`** — required, shown to the student as the exercise's label.
- **`type`** — required; `"term"` is the only type relay-grader currently supports.
- **`asset`** — optional. If present, must match a `name` in this lab's `environment.assets[]`; the exercise is graded only against that asset's terminal output. Omitted means lab-wide — satisfied if the pattern shows up on *any* asset.
- **`template`** — required, always the last field. **This is a regular expression matched against the student's terminal output, not a shell command** — it's satisfied once the pattern appears anywhere in that asset's recent scrollback. Everything from the `template:` line to the closing fence is the pattern body verbatim, so multi-line regexes are fine without YAML block-scalar syntax. Field order otherwise doesn't matter.

A task may have zero, one, or several exercise blocks. `labs-sync.py` numbers each one `"<task#>.<exercise#>"` (1-indexed, exercise number resets per task), validates any `asset` reference against `environment.assets`, and — only at sync time, for storage — rewrites each block down to just `id` and `description` before it reaches PocketBase, so `type`/`asset`/`template` never reach the frontend.

See [`docs/lab-grade.md`](../docs/lab-grade.md) for how exercises get graded end to end, from sync through to the live regex matching against a student's terminal.

## Images

`images/` holds Dockerfiles for the container images labs reference by name in `environment.assets[].image` (see [`infra.md`](../docs/infra.md) for how these get built).
