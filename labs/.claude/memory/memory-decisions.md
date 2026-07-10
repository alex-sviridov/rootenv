---
description: Key decisions made about lab format, structure, or authoring conventions
paths:
  - "labs/*"
---

# Labs Decisions

_Record decisions here as they are made — include what was decided, why, and what alternatives were rejected._

## `platform`/`ssh_user` asset fields removed (2026-07-06)

Every lab YAML's `environment.assets[]` entries carried `platform: container` and (inconsistently) `ssh_user: lab`, but neither field was ever read by `labs-sync.py`, `attempt-controller`'s `Asset` struct, the `LabEnvironment` CRD, or `labenv-operator` — confirmed via repo-wide grep before removal. They were write-only authoring metadata from an earlier design. Removed from all 4 lab files that had them (`ex200/rhcsa1.yaml`, `ex200/rhcsa2.yaml`, `ex342/nfsdebug11.yaml`, `deeplinux/deepssh.yaml`); `labs-sync.py --verify` still passes 8/8 after removal. Do not reintroduce without first wiring a consumer.

## `deepssh.yaml` uses a legacy, unparsed `check:`/`host` grading format — needs migration

`labs/definitions/deeplinux/deepssh.yaml` has 9 `check:` blocks (list of `{host, description}`) as siblings of `content:` entries, instead of the ` ```exercise ` fenced-block format every other lab uses. `labs-sync.py`/`labs_sync_exercises.py` have no reference to a `check` key at all — this lab currently has **zero** gradeable exercises despite looking like it should. Flagged during the 2026-07-06 lab-grading docs pass; not fixed here since it's lab-content authoring work, not a docs or infra change. Whoever picks up `deepssh.yaml` next should migrate its `check:` blocks to ` ```exercise ` blocks.
