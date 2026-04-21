---
description: Key architectural and implementation decisions made for the contmgr module
paths:
  - "contmgr/*"
---

# Contmgr Decisions

_Record decisions here as they are made — include what was decided, why, and what alternatives were rejected._

## Initial Design (plan.md)

- **Polling over event-driven:** PocketBase hooks can't push to external services reliably; polling every 5s is simple and sufficient at this scale.
- **Ed25519 keypairs:** Smaller keys, faster generation, modern standard. Generated in-memory — never touch disk.
- **AES-256-GCM for key storage:** Authenticated encryption; nonce prepended to ciphertext in single base64 blob for simplicity. Key derived via SHA-256 of the PocketBase-generated `secret` field.
- **Port allocation via `net.Listen(":0")`:** OS assigns a free port; accepted race window is tolerable for ephemeral internal containers.
- **Public key injection via docker exec:** Avoids building custom images; works with any image that has a shell.
- **`errgroup` for concurrency:** Errors per asset are logged but don't abort the poll cycle — one bad container shouldn't block others.
- **No HTTP server in contmgr:** Contmgr is a pure worker; observability via stdout logs only.
