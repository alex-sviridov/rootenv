---
description: Authoring style and conventions preferred when writing lab content
paths:
  - "labs/*"
---

# Labs Preferences

_Record preferred authoring conventions, markdown style, task structure patterns, etc._

## Exercise regex tolerance: split by outcome, tolerate alternate approaches

When adding ` ```exercise ` blocks, confirmed conventions (2026-07-10, `baselinux/datastream.yaml`):
- One exercise block per independently-checkable outcome, even when a single `**Упражнение:**` prompt names 2+ targets (e.g. delete `.bak` in dir A + delete old archives in dir B → two blocks). Don't collapse multi-target prompts into one loose regex.
- For file-output tasks (student redirects into a report file), grade the shell command/pipeline containing the redirect, not the file contents — the grader only sees terminal scrollback.
- Regex should tolerate reasonable alternate approaches: flag order (`sort -rn` vs `sort -nr` → use `-\w*[rn]\w*[rn]\w*`), quote style (`'`/`"`), and equivalent tool substitutions (`grep -c X` vs `grep X | wc -l`). Always test candidate regexes against 2-3 hand-written alternate student commands before finalizing — order-dependent character classes (`[rn]` vs literal `rn`) are the most common bug.
- **Never use lookahead/lookbehind (`(?=...)`, `(?!...)`, `(?<=...)`) or backreferences in a `template`.** relay-grader compiles templates with Go's `regexp` package (RE2 syntax — see `services/relay/grader/backend.go`), which does not support them at all; the compile fails silently (`invalid task template regex, task will never pass`) and the exercise becomes permanently unpassable with no visible error to the student. (Corrects earlier advice in this file that recommended lookaheads — that advice was wrong and had already shipped broken exercises in `advancedlinux/sshindepth.yaml` before this was caught, 2026-07-10.)
- For "both X and Y must appear, any order" checks, use explicit order alternation instead: `(X[\s\S]*Y|Y[\s\S]*X)`. For 3+ order-independent tokens this gets combinatorially ugly fast — prefer keying off a token whose position is grammatically fixed regardless of the ambiguous part (e.g. for a SQL `JOIN` where table order can flip, anchor on `SELECT`/`SUM` before the `FROM`/`JOIN` clause and `GROUP BY` after it, and don't require the two table names to appear in exercise-code order — see `baselinux/datastream.yaml`'s SQLite task) rather than trying to cover every permutation.
- `[\s\S]*` is the RE2-safe way to say "any characters including newlines" inline; the grader already prepends `(?s)` to the whole template so a bare `.*` also spans lines, but `[\s\S]*` reads clearer when order-alternating across multiple commands.
- `scripts/labs-sync.py` (and `--verify`) now actually compile every `template` through `scripts/regexcheck` (a tiny Go helper, since only `go`'s own `regexp` package can confirm RE2-validity) and fail the lab if any don't compile — requires `go` on `PATH`. Run `python3 scripts/labs-sync.py --verify labs/definitions` after writing any exercise, not just for asset-name typos.
