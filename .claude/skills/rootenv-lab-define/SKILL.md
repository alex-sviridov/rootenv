---
name: rootenv-lab-define
description: This skill helps to create lab definitions for the rootenv project. 
---

# Rootenv Lab Define

## Authoring Workflow

Before writing any YAML, go through four approval gates with the user. Never skip ahead — each gate catches misalignment cheaply.

### Gate 1 — Lab goal

State the single sentence that describes what a student will have accomplished by the end. Be concrete: "configure SSH certificate authentication using a custom CA" beats "learn about SSH". Get approval before continuing.

### Gate 2 — Skill list

List every discrete system administration skill or concept the lab will cover, one per bullet. Each bullet should be learnable in ~5–15 minutes. Aim for 4–7 skills per lab — fewer feels thin, more becomes exhausting. Get approval. This list becomes the direct input to Gate 3.

### Gate 3 — Task outlines

For each skill from Gate 2, write one sentence: task title + what the student will actually type or configure. Example: *"Generate a CA key pair — student runs `ssh-keygen` to create `/etc/ssh-ca/ca` and `ca.pub`."* Present all outlines together and get approval before writing content.

### Gate 4 — Full content

Write the complete YAML. Follow the content style rules below.

---

## Content Style

### Difficulty and pacing

- **Difficulty target:** student should succeed with the provided commands and brief thought, but not by copy-pasting blindly. Every task should require at least one decision or small adaptation.
- **Task size:** one coherent topic per task section, 2–4 exercises inside it. If exercises don't share state or build on each other, they belong in separate sections.
- **Progression:** order tasks so each one builds on the previous. Don't introduce a concept and then ask about a prerequisite two tasks later.
- **Challenge hooks:** end at least one task per lab with a variation that isn't shown in the example (e.g. "now do the same for user `dave`"). This keeps faster students engaged without blocking slower ones.

### Task body structure

A task section covers one coherent topic and takes 5–15 minutes. It contains multiple exercises — typically 2–4 — that build on each other within the same theme. Think of the section as a short lesson with theory woven throughout, not dumped at the top.

**Theory placement:** theory can appear anywhere it is needed — before the whole section, between exercises, or immediately before a specific exercise that needs it. Place it where the student will encounter it at the moment it matters. Use as much theory as the topic warrants; the constraint is relevance, not brevity.

**Exercise solution visibility:** not every exercise should show the solution. Calibrate per exercise:
- **Guided** — show the exact commands, student runs and observes. Use for unfamiliar syntax or dangerous operations.
- **Prompted** — show the relevant man page section, flag list, or concept; student constructs the command. Use once the pattern is established.
- **Open** — state the goal and expected outcome only; no commands shown. Use for the final exercise in a section, or when the skill was already practiced earlier.

Mix all three within a lab. Never use open exercises for a concept introduced for the first time.

**Connecting thread** — exercises within a section must share state. A file created in exercise 1 is used in exercise 2; a service configured in exercise 2 is tested in exercise 3. Never write standalone exercises that could be shuffled without consequence.

### Exercise blocks (auto-graded checks)

A task's `content` markdown can embed one or more graded exercises as fenced code blocks with the info-string `exercise`. Add one whenever a task step has a checkable outcome — commonly one per `**Task:**`/`**Exercise:**` prompt, sometimes more if a single prompt has multiple independently-checkable outcomes (e.g. "create the file and set its ownership" → one exercise per outcome). Purely conceptual prompts ("explain why...") get none.

````markdown
```exercise
description: Create /tmp/labfile owned by bob
type: term
asset: server-0
template:
chown\s+bob\s+/tmp/labfile
```
````

- **`description`** — required. Shown to the student as the exercise's label. State the checkable outcome, not the command (e.g. "Set `/tmp/labfile` to mode 750", not "run chmod 750").
- **`type`** — required; `term` is the only type relay-grader currently supports.
- **`asset`** — optional. Must match a `name` in this lab's `environment.assets[]` when present; the exercise is graded only against that asset's terminal output. Omit for single-node labs or when any asset satisfies it.
- **`template`** — required, always the last field. **A regular expression matched against the student's terminal scrollback, not a shell command** — satisfied once the pattern appears anywhere in that asset's recent output. Everything from the `template:` line to the closing fence is the pattern body verbatim (multi-line regexes are fine, no YAML block-scalar needed). Escape regex metacharacters in literal paths/values (`.`, `/` are usually left bare, but `[`, `(`, `+` etc. need escaping if literal). Use `\s+` between tokens so exercises tolerate flag/spacing variation. Field order otherwise doesn't matter.

`labs-sync.py` numbers each block `"<task#>.<exercise#>"` (1-indexed, exercise number resets per task), validates any `asset` reference against `environment.assets`, and rewrites each block down to just `id` and `description` before it reaches PocketBase — `type`/`asset`/`template` never reach the frontend.

**Writing tolerant regexes:**
- If a prompt implies multiple independent targets (e.g. "delete the `.bak` files in dir A and the old archives in dir B"), write one exercise block per target — don't collapse them into a single loose regex.
- For tasks that redirect output into a file (`> report.txt`), grade the command/pipeline containing the redirect, not the file's contents — the grader only ever sees terminal scrollback.
- Order-independent flags need an order-independent class, not a fixed sequence: `sort -\w*[rn]\w*[rn]\w*` matches both `-rn` and `-nr`; a literal `-\w*rn\w*` does not.
- To require two substrings anywhere in the output regardless of order or which side of a pipe they're on, use lookaheads — `(?=.*foo)(?=.*bar)` — rather than enumerating every ordering by hand.
- Before finalizing a non-trivial regex, test it in a scratch script against 2–3 hand-written alternate commands a student might plausibly type (different flag order, different tool chain achieving the same result). This catches operator-precedence and ordering bugs that are easy to miss by inspection.

### Markdown conventions

Use the full range of markdown to make theory readable:

- `## Headers` inside a task body to separate a theory block from the exercise sequence, or to name a major concept being introduced.
- Bullet and numbered lists for enumerating flags, permission bits, rules, or steps.
- Tables for structured reference material (e.g. permission bit meanings, signal numbers, mount options).
- Triple-backtick `bash` fences for all shell commands.
- Inline `code` for file paths, command names, flags, and values.
- Bold for `**Exercise:**` prompts and server role callouts (`**server-0**`, `**server-1**`).

Headers and lists are for theory and reference. Exercise prompts (`**Exercise:**`) remain plain bold lines — no heading level on them.

---

## Instructions

### 1. Determine placement

Labs live under `labs/definitions/`. Each subdirectory is a **group** (folder record). The YAML filename without extension becomes the lab slug.

```
labs/definitions/<group>/<slug>.yaml
```

Examples:
- `labs/definitions/ex200/rhcsa1.yaml` → id `ex200_rhcsa1`, group `ex200`
- `labs/definitions/networking/advanced/bgp.yaml` → id `networking_advanced_bgp`, group `networking_advanced`

If the group directory doesn't exist yet, also create `index.yaml` inside it with `meta.title` and `meta.description`.

### 2. Write the YAML

Every lab file has three top-level keys:

```yaml
meta:
  title: Human-readable lab name
  description: One-sentence description shown in the lab browser.

content:
  - title: Task Title
    content: |
      Markdown body — explanation, code blocks, bold **Task:** prompt.

environment:
  duration: <minutes>          # session timeout
  assets:
    - name: server-0
      image: <image>           # e.g. ubuntu, ubuntu-sshd
      platform: container
      ssh_user: lab
      cpu: 100m                # Kubernetes resource notation
      memory: 128Mi
      disk: 5Gi
      protocols:
        - exec
      setup: |                  # optional — shell script run during provisioning
        mkdir -p /etc/ssh-ca
        chown lab:lab /etc/ssh-ca
```

`setup` is an optional multi-line shell script executed on the server during provisioning, before the user gets access. Use it to pre-create directories, install packages, seed config files, or set up any state the lab scenario depends on. It runs as root.

### 3. Content task structure

Each item in `content` follows this pattern:

1. **Explanation paragraph** — concise prose describing the concept.
2. **Code block** — concrete commands with inline `# comments` for context.
3. **Bold Task line** — `**Task:** <imperative instruction the user must complete.>`
4. **Exercise block(s)** — one ` ```exercise ` block per checkable outcome the Task line implies. See "Exercise blocks (auto-graded checks)" above for the format.

Use `**server-0**` / `**server-1**` callouts when a multi-node lab has role-specific steps.

### 4. Environment rules

- `meta` and `content` are shown to users; `environment` is **never** exposed to the frontend.
- `duration` is in minutes.
- Asset `name` values must be `server-0`, `server-1`, … (zero-indexed). Relay URL index (`/relay/0/`) matches the order in `assets`.
- Use `image: ubuntu` for single-node labs. Use `image: ubuntu-sshd` when inter-node SSH is needed.
- CPU in millicores (`100m`–`500m`), memory in Mi, disk in Gi.
- Add a server only when the scenario genuinely needs multiple nodes.

### 5. index.yaml (group metadata)

Create one per group directory. Only two keys:

```yaml
meta:
  title: Group Display Name
  description: Short description of the group shown in the lab browser.
```

`index.yaml` is never synced as a lab record — it only sets folder metadata.

### 6. IDs and uniqueness

IDs are derived from the file path (underscores replace slashes, extension dropped). They must be unique — enforced by path uniqueness. Never manually assign an `id` field.

---

## Examples

### Single-node lab (ex200/rhcsa1.yaml)

```yaml
meta:
  title: File Permissions and Ownership
  description: Practice managing Linux file permissions, ownership, and special bits essential for the RHCSA exam.

content:
  - title: Inspect Current Permissions
    content: |
      Use `ls -l` to examine file permissions and ownership.

      ```bash
      ls -l /etc/passwd
      ls -ld /tmp
      ```

      **Task:** List the permissions of `/etc/shadow` and explain why it has those permissions.

      ```exercise
      description: List the permissions of /etc/shadow
      type: term
      asset: server-0
      template:
      ls\s+-l[a-z]*\s+/etc/shadow
      ```

  - title: Set Permissions with chmod
    content: |
      Use `chmod` with symbolic or octal notation to set permissions.

      ```bash
      chmod 644 /tmp/labfile
      chmod u+x script.sh
      ```

      **Task:** Set `/tmp/labfile` to `750` (owner: rwx, group: r-x, others: ---).

      ```exercise
      description: Create /tmp/labfile
      type: term
      asset: server-0
      template:
      touch\s+/tmp/labfile
      ```

      ```exercise
      description: Set /tmp/labfile permissions to 750
      type: term
      asset: server-0
      template:
      chmod\s+750\s+/tmp/labfile
      ```

environment:
  duration: 90
  assets:
    - name: server-0
      image: ubuntu
      platform: container
      ssh_user: lab
      cpu: 100m
      memory: 128Mi
      disk: 5Gi
      protocols:
        - exec
```

### Multi-node lab (ex342/nfsdebug11.yaml)

```yaml
meta:
  title: NFS Troubleshooting
  description: Diagnose and fix common NFS export and mount issues across a server and client node.

content:
  - title: Verify NFS Services
    content: |
      Before debugging mounts, confirm NFS services are running on **server-0**.

      ```bash
      systemctl status nfs-server
      systemctl enable --now nfs-server
      ```

      On **server-1** (client), verify the NFS utilities are available:

      ```bash
      rpm -q nfs-utils
      rpcinfo -p server-0
      ```

      **Task:** Ensure `nfs-server` is active on server-0 and confirm server-0's RPC portmapper is reachable from server-1.

      ```exercise
      description: Enable and start nfs-server on server-0
      type: term
      asset: server-0
      template:
      systemctl\s+enable\s+--now\s+nfs-server
      ```

      ```exercise
      description: Confirm server-0's RPC portmapper from server-1
      type: term
      asset: server-1
      template:
      rpcinfo\s+-p\s+server-0
      ```

environment:
  duration: 30
  assets:
    - name: server-0
      image: ubuntu-sshd
      platform: container
      ssh_user: lab
      cpu: 200m
      memory: 128Mi
      disk: 5Gi
      protocols:
        - exec
    - name: server-1
      image: ubuntu-sshd
      platform: container
      ssh_user: lab
      cpu: 200m
      memory: 128Mi
      disk: 5Gi
      protocols:
        - exec
```

### Group index (ex200/index.yaml)

```yaml
meta:
  title: RHCSA
  description: Practice core RHCSA skills, including file permissions, user management, and system services.
```
