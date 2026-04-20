---
description: Coding style, tooling, and workflow preferences specific to the infrastructure module
paths:
  - "infra/*"
---

# Infra Preferences

_Record infra-specific conventions, patterns to follow or avoid, and tooling preferences here._

## Dev workflow
Always use `docker compose watch` (not `up`) for development. All services use `develop.watch` blocks — never bind-mount config files as volumes in compose-dev.yaml.
