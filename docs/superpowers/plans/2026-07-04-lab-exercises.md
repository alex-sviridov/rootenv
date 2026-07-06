# Lab Exercises Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let lab authors embed gradeable exercises inline in task markdown, and thread the extracted exercise data end-to-end from lab YAML through PocketBase, attempt-controller, and the `LabEnvironment` CRD into labenv-operator's `grader-tasks` ConfigMap — replacing today's hardcoded placeholder `tasks.json`.

**Architecture:** Exercises are authored as ` ```exercise ` fenced code blocks inside a task's markdown `content` string (`description`/`type`/`asset`/`template` as plain `key: value` lines). `scripts/labs-sync.py` extracts and validates these blocks at sync time, computes each exercise's id (`<task#>.<exercise#>`), rewrites `content` to a stripped placeholder (id + description only, for the public view), and writes a new `exercises` field to the `labs` PocketBase collection. `attempt-controller` copies `labs.exercises` into the `LabEnvironment` CRD's new `spec.exercises` field, mirroring how it already copies `environment.assets`. `labenv-operator` serializes `spec.exercises` into `grader-tasks`' `tasks.json`, dropping the `description` field (grader has no use for it). `grader.Task` gains an optional `asset` field that flows through unused (future work will filter grading by asset).

**Tech Stack:** Python 3 + PyYAML + pytest (labs-sync), Go + Ginkgo/envtest (labenv-operator), Go + `testing` stdlib (attempt-controller, relay/grader), PocketBase JS migrations.

## Global Constraints

- Exercise `type` must currently be `"term"` — no other value is valid (matches `grader.LoadTasks`'s existing constraint).
- `type` is always written explicitly by lab authors; there is no default.
- `asset`, when present in an exercise block, must match a `name` in that lab's `environment.assets`; validation happens once, in `labs-sync.py` — nowhere else re-validates it.
- `labs_userview`'s field list (`id, title, description, content, parent, type`) is unchanged — `exercises` must never be added to it.
- The rewritten `content` stored on `labs` records must never contain `type`, `asset`, or `template` — only `id` and `description` survive into the placeholder.
- `tasks.json` (the grader-tasks ConfigMap payload) must never contain `description` — it's frontend-only.
- Field names copied into the `LabEnvironment` CRD's `spec.exercises` must stay in sync with `labenv-operator`'s `Exercise` Go type (same convention already used for `spec.assets`/`Asset`).

---

## File Structure

| File | Responsibility |
|---|---|
| `services/backend/pb_migrations/1783203214_updated_labs.js` | (already exists, untracked) adds `exercises` json field to `labs` collection |
| `scripts/labs_sync_exercises.py` | New module: parse ` ```exercise ` blocks out of task markdown, validate, compute ids, rewrite content |
| `scripts/test_labs_sync_exercises.py` | pytest tests for the above |
| `scripts/labs-sync.py` | Modified: `validate_lab` calls exercise validation; `upsert_lab` calls extraction/rewrite and sends `exercises` field |
| `services/relay/grader/tasks.go` | Modified: `Task` gains `Asset string` field |
| `services/relay/grader/tasks_test.go` | Modified: covers `asset` present/absent/round-trip |
| `services/attempt-controller/internal/pocketbase/pbclient.go` | Modified: `AttemptRecord.Expand.Lab` gains `Exercises` field; `ToAttempt()` unmarshals it |
| `services/attempt-controller/internal/downstream/reconcile.go` | Modified: new `Exercise` struct; `Attempt.Exercises`; `toLabEnvironment` maps it into `spec.exercises` |
| `services/attempt-controller/internal/downstream/reconcile_test.go` | Modified: covers exercises mapping (empty, populated, asset omitted) |
| `services/labenv-operator/api/v1alpha1/labenvironment_types.go` | Modified: `LabEnvironmentSpec` gains `Exercises []Exercise`; new `Exercise` type |
| `services/labenv-operator/internal/controller/grader.go` | Modified: `ensureGraderTasksConfigMap` serializes `env.Spec.Exercises` instead of the placeholder constant |
| `services/labenv-operator/internal/controller/labenvironment_controller_test.go` | Modified: replaces placeholder assertions with exercises-derived assertions; covers empty case |

---

### Task 1: Commit the existing PocketBase migration

**Files:**
- Verify/commit: `services/backend/pb_migrations/1783203214_updated_labs.js` (already present, untracked)
- Modify: `.claude/memory/memory-backend.md`

**Interfaces:**
- Produces: `labs` collection has a new field `exercises` (type `json`, not required, not presentable, not system, no maxSize limit).

- [ ] **Step 1: Read the existing migration file and confirm its shape**

Run: `cat services/backend/pb_migrations/1783203214_updated_labs.js`

Expected: a `migrate()` call that does `app.findCollectionByNameOrId("pbc_2691397795")`, then `collection.fields.addAt(8, new Field({... "name": "exercises", "type": "json", "required": false ...}))`, with a down-migration that calls `collection.fields.removeById(...)`.

If the file does not match this shape, stop and re-derive it manually via the PocketBase admin UI (add a `json` field named `exercises`, not required, to the `labs` collection) before continuing.

Note: this migration has **already been applied** to the running PocketBase instance in this environment (confirmed by the user) — skip starting PocketBase or re-verifying the migration applies; go straight to Step 2.

- [ ] **Step 2: Update `.claude/memory/memory-backend.md`**

Add a row to the `labs (base)` table in `.claude/memory/memory-backend.md`:

```markdown
| exercises | json | array of `{id, description, type, asset?, template}`, written by labs-sync.py only; never exposed via `labs_userview` |
```

- [ ] **Step 3: Commit**

```bash
git add services/backend/pb_migrations/1783203214_updated_labs.js .claude/memory/memory-backend.md
git commit -m "feat(backend): add exercises json field to labs collection"
```

---

### Task 2: `labs_sync_exercises.py` — parse a single exercise block

**Files:**
- Create: `scripts/labs_sync_exercises.py`
- Test: `scripts/test_labs_sync_exercises.py`

**Interfaces:**
- Produces: `parse_exercise_block(body: str) -> dict` — takes the text between the ` ```exercise ` fence markers (not including the fence lines themselves) and returns `{"description": str, "type": str, "asset": str | None, "template": str}`. Raises `ValueError` with a descriptive message if `description` or `type` is missing, or if there's no `template:` line.

- [ ] **Step 1: Write the failing tests**

```python
# scripts/test_labs_sync_exercises.py
import pytest
from labs_sync_exercises import parse_exercise_block


def test_parse_exercise_block_basic():
    body = (
        "description: Create /tmp/labfile owned by bob\n"
        "type: term\n"
        "template:\n"
        "test -O /tmp/labfile\n"
    )
    result = parse_exercise_block(body)
    assert result == {
        "description": "Create /tmp/labfile owned by bob",
        "type": "term",
        "asset": None,
        "template": "test -O /tmp/labfile",
    }


def test_parse_exercise_block_multiline_template():
    body = (
        "description: Check both owner and group\n"
        "type: term\n"
        "template:\n"
        "test -O /tmp/labfile\n"
        "test -G /tmp/labfile\n"
    )
    result = parse_exercise_block(body)
    assert result["template"] == "test -O /tmp/labfile\ntest -G /tmp/labfile"


def test_parse_exercise_block_with_asset():
    body = (
        "description: d\n"
        "type: term\n"
        "asset: server-0\n"
        "template:\n"
        "echo hi\n"
    )
    result = parse_exercise_block(body)
    assert result["asset"] == "server-0"


def test_parse_exercise_block_field_order_flexible():
    body = (
        "type: term\n"
        "description: d\n"
        "template:\n"
        "echo hi\n"
    )
    result = parse_exercise_block(body)
    assert result["description"] == "d"
    assert result["type"] == "term"


def test_parse_exercise_block_missing_description():
    body = "type: term\ntemplate:\necho hi\n"
    with pytest.raises(ValueError, match="description"):
        parse_exercise_block(body)


def test_parse_exercise_block_missing_type():
    body = "description: d\ntemplate:\necho hi\n"
    with pytest.raises(ValueError, match="type"):
        parse_exercise_block(body)


def test_parse_exercise_block_missing_template():
    body = "description: d\ntype: term\n"
    with pytest.raises(ValueError, match="template"):
        parse_exercise_block(body)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd scripts && python3 -m pytest test_labs_sync_exercises.py -v`
Expected: FAIL with `ModuleNotFoundError: No module named 'labs_sync_exercises'`

- [ ] **Step 3: Write the implementation**

```python
# scripts/labs_sync_exercises.py
"""Extraction, validation, and rewriting of ```exercise blocks in lab task markdown."""

_LABELS = ("description", "type", "asset")


def parse_exercise_block(body: str) -> dict:
    """Parse the body of a ```exercise fenced block (everything between the
    fence lines) into {description, type, asset, template}.

    Fields are matched by their `label:` prefix, not by line position.
    Everything after the `template:` line becomes the template body verbatim.
    """
    lines = body.splitlines()
    fields = {"description": None, "type": None, "asset": None}
    template_lines = None

    i = 0
    while i < len(lines):
        line = lines[i]
        matched = False
        for label in _LABELS:
            prefix = label + ":"
            if line.startswith(prefix):
                fields[label] = line[len(prefix):].strip()
                matched = True
                break
        if not matched and line.startswith("template:"):
            template_lines = lines[i + 1:]
            break
        i += 1

    if not fields["description"]:
        raise ValueError("exercise block missing 'description'")
    if not fields["type"]:
        raise ValueError("exercise block missing 'type'")
    if template_lines is None:
        raise ValueError("exercise block missing 'template'")

    return {
        "description": fields["description"],
        "type": fields["type"],
        "asset": fields["asset"] or None,
        "template": "\n".join(template_lines).rstrip("\n"),
    }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd scripts && python3 -m pytest test_labs_sync_exercises.py -v`
Expected: PASS (6 tests)

- [ ] **Step 5: Commit**

```bash
git add scripts/labs_sync_exercises.py scripts/test_labs_sync_exercises.py
git commit -m "feat(labs-sync): parse exercise fenced blocks into structured fields"
```

---

### Task 3: `labs_sync_exercises.py` — extract all exercises from a lab, compute ids

**Files:**
- Modify: `scripts/labs_sync_exercises.py`
- Test: `scripts/test_labs_sync_exercises.py`

**Interfaces:**
- Consumes: `parse_exercise_block(body: str) -> dict` from Task 2.
- Produces: `extract_exercises(content: list[dict]) -> list[dict]` — takes a lab's `content` list (each item a `{"title": str, "content": str}` task dict) and returns a flat list of `{"id": str, "description": str, "type": str, "asset": str | None, "template": str}` across all tasks, in order. Raises `ValueError` (with task/exercise position in the message) if any block fails to parse.

- [ ] **Step 1: Write the failing tests**

```python
# add to scripts/test_labs_sync_exercises.py
from labs_sync_exercises import extract_exercises


def _task(content_md):
    return {"title": "t", "content": content_md}


def test_extract_exercises_single_task_single_exercise():
    content = [_task(
        "Some intro text.\n\n"
        "```exercise\n"
        "description: d1\n"
        "type: term\n"
        "template:\n"
        "echo hi\n"
        "```\n"
    )]
    result = extract_exercises(content)
    assert result == [{
        "id": "1.1", "description": "d1", "type": "term",
        "asset": None, "template": "echo hi",
    }]


def test_extract_exercises_numbering_across_tasks_and_multiple_per_task():
    content = [
        _task(
            "```exercise\ndescription: d1\ntype: term\ntemplate:\necho a\n```\n"
            "middle text\n"
            "```exercise\ndescription: d2\ntype: term\ntemplate:\necho b\n```\n"
        ),
        _task(
            "```exercise\ndescription: d3\ntype: term\ntemplate:\necho c\n```\n"
        ),
    ]
    result = extract_exercises(content)
    ids = [e["id"] for e in result]
    assert ids == ["1.1", "1.2", "2.1"]


def test_extract_exercises_no_exercises_in_task():
    content = [_task("Just prose, no exercise blocks.\n")]
    assert extract_exercises(content) == []


def test_extract_exercises_ignores_other_fenced_blocks():
    content = [_task(
        "```bash\necho not an exercise\n```\n"
        "```exercise\ndescription: d\ntype: term\ntemplate:\necho hi\n```\n"
    )]
    result = extract_exercises(content)
    assert len(result) == 1
    assert result[0]["id"] == "1.1"


def test_extract_exercises_invalid_block_raises_with_position():
    content = [_task("```exercise\ntype: term\ntemplate:\necho hi\n```\n")]
    with pytest.raises(ValueError, match="task 1"):
        extract_exercises(content)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd scripts && python3 -m pytest test_labs_sync_exercises.py -v -k extract_exercises`
Expected: FAIL with `ImportError: cannot import name 'extract_exercises'`

- [ ] **Step 3: Write the implementation**

```python
# add to scripts/labs_sync_exercises.py
import re

_FENCE_RE = re.compile(r'```exercise\n(.*?)\n```', re.DOTALL)


def extract_exercises(content: list[dict]) -> list[dict]:
    """Extract all ```exercise blocks across a lab's content (task) list,
    computing ids as "<task#>.<exercise#>" (both 1-indexed, exercise number
    resets per task).
    """
    exercises = []
    for task_num, task in enumerate(content, start=1):
        body_text = task.get("content", "") or ""
        for exercise_num, match in enumerate(_FENCE_RE.finditer(body_text), start=1):
            try:
                parsed = parse_exercise_block(match.group(1))
            except ValueError as e:
                raise ValueError(f"task {task_num}, exercise {exercise_num}: {e}") from e
            parsed["id"] = f"{task_num}.{exercise_num}"
            exercises.append(parsed)
    return exercises
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd scripts && python3 -m pytest test_labs_sync_exercises.py -v`
Expected: PASS (11 tests total)

- [ ] **Step 5: Commit**

```bash
git add scripts/labs_sync_exercises.py scripts/test_labs_sync_exercises.py
git commit -m "feat(labs-sync): extract and number exercises across a lab's tasks"
```

---

### Task 4: `labs_sync_exercises.py` — validate asset references, rewrite content

**Files:**
- Modify: `scripts/labs_sync_exercises.py`
- Test: `scripts/test_labs_sync_exercises.py`

**Interfaces:**
- Consumes: `extract_exercises(content) -> list[dict]` from Task 3.
- Produces:
  - `validate_exercise_assets(exercises: list[dict], asset_names: set[str]) -> list[str]` — returns a list of human-readable error strings (empty list if all valid); does not raise.
  - `rewrite_content_with_placeholders(content: list[dict]) -> list[dict]`  — returns a new `content` list (same shape as input) with every ` ```exercise ` block's body replaced by `id: <id>\ndescription: <description>` (using the same ids `extract_exercises` would compute). Original `content` list is not mutated.

- [ ] **Step 1: Write the failing tests**

```python
# add to scripts/test_labs_sync_exercises.py
from labs_sync_exercises import validate_exercise_assets, rewrite_content_with_placeholders


def test_validate_exercise_assets_all_valid():
    exercises = [{"id": "1.1", "asset": "server-0"}, {"id": "1.2", "asset": None}]
    errors = validate_exercise_assets(exercises, {"server-0", "server-1"})
    assert errors == []


def test_validate_exercise_assets_unknown_asset():
    exercises = [{"id": "1.1", "asset": "server-9"}]
    errors = validate_exercise_assets(exercises, {"server-0"})
    assert len(errors) == 1
    assert "1.1" in errors[0]
    assert "server-9" in errors[0]


def test_rewrite_content_with_placeholders_strips_type_and_template():
    content = [_task(
        "intro\n\n"
        "```exercise\n"
        "description: Create /tmp/labfile\n"
        "type: term\n"
        "template:\n"
        "test -O /tmp/labfile\n"
        "```\n"
        "outro\n"
    )]
    rewritten = rewrite_content_with_placeholders(content)
    assert len(rewritten) == 1
    body = rewritten[0]["content"]
    assert "type:" not in body
    assert "template:" not in body
    assert "test -O /tmp/labfile" not in body
    assert "```exercise\nid: 1.1\ndescription: Create /tmp/labfile\n```" in body
    assert "intro" in body and "outro" in body


def test_rewrite_content_with_placeholders_does_not_mutate_input():
    original_body = "```exercise\ndescription: d\ntype: term\ntemplate:\necho hi\n```\n"
    content = [_task(original_body)]
    rewrite_content_with_placeholders(content)
    assert content[0]["content"] == original_body
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd scripts && python3 -m pytest test_labs_sync_exercises.py -v -k "validate_exercise_assets or rewrite_content"`
Expected: FAIL with `ImportError`

- [ ] **Step 3: Write the implementation**

```python
# add to scripts/labs_sync_exercises.py
import copy


def validate_exercise_assets(exercises: list[dict], asset_names: set[str]) -> list[str]:
    """Check each exercise's `asset` (if set) against the lab's known asset
    names. Returns a list of error strings; empty if everything is valid.
    """
    errors = []
    for ex in exercises:
        asset = ex.get("asset")
        if asset and asset not in asset_names:
            errors.append(f"exercise {ex['id']}: asset '{asset}' not found in environment.assets")
    return errors


def rewrite_content_with_placeholders(content: list[dict]) -> list[dict]:
    """Return a copy of `content` with every exercise block's body replaced
    by a stripped placeholder containing only id and description.
    """
    rewritten = copy.deepcopy(content)
    for task_num, task in enumerate(rewritten, start=1):
        body_text = task.get("content", "") or ""
        counter = [0]

        def _replace_for_task(match, task_num=task_num, counter=counter):
            counter[0] += 1
            parsed = parse_exercise_block(match.group(1))
            exercise_id = f"{task_num}.{counter[0]}"
            return f"```exercise\nid: {exercise_id}\ndescription: {parsed['description']}\n```"

        task["content"] = _FENCE_RE.sub(_replace_for_task, body_text)
    return rewritten
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd scripts && python3 -m pytest test_labs_sync_exercises.py -v`
Expected: PASS (15 tests total)

- [ ] **Step 5: Commit**

```bash
git add scripts/labs_sync_exercises.py scripts/test_labs_sync_exercises.py
git commit -m "feat(labs-sync): validate exercise asset refs and rewrite content placeholders"
```

---

### Task 5: Wire exercise extraction into `labs-sync.py`'s validate/upsert flow

**Files:**
- Modify: `scripts/labs-sync.py`
- Test: `scripts/test_labs_sync_exercises.py` (add integration-style tests calling into `labs-sync.py`'s functions directly)

**Interfaces:**
- Consumes: `extract_exercises`, `validate_exercise_assets`, `rewrite_content_with_placeholders` from `labs_sync_exercises` (Tasks 2-4).
- Produces: `validate_lab(path)` now also reports exercise validation errors; `upsert_lab(token, path)` now sends a rewritten `content` and a new `exercises` field.

- [ ] **Step 1: Write the failing tests**

Since `validate_lab`/`upsert_lab` read from disk and `upsert_lab` calls the network, add tests that write a temp lab file and call the pure parts directly (existing test file has no fixtures for this, so add minimal ones):

```python
# add to scripts/test_labs_sync_exercises.py
import importlib.util
import yaml as _yaml
from pathlib import Path


def _load_labs_sync():
    """Load scripts/labs-sync.py as a module (its filename isn't a valid
    Python identifier, so it can't be imported normally)."""
    spec = importlib.util.spec_from_file_location("labs_sync", Path(__file__).parent / "labs-sync.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_labs_sync_validate_lab_reports_exercise_errors(tmp_path):
    labs_sync = _load_labs_sync()

    labs_dir = tmp_path / "labs"
    labs_dir.mkdir()
    lab_file = labs_dir / "mylab.yaml"
    lab_file.write_text(_yaml.dump({
        "meta": {"title": "My Lab"},
        "content": [{"title": "Task 1", "content": "```exercise\ntype: term\ntemplate:\necho hi\n```\n"}],
        "environment": {"assets": [{"name": "server-0"}]},
    }))

    labs_sync.LABS_DIR = labs_dir
    ok = labs_sync.validate_lab(lab_file)
    assert ok is False


def test_labs_sync_validate_lab_reports_unknown_asset(tmp_path):
    labs_sync = _load_labs_sync()

    labs_dir = tmp_path / "labs"
    labs_dir.mkdir()
    lab_file = labs_dir / "mylab.yaml"
    lab_file.write_text(_yaml.dump({
        "meta": {"title": "My Lab"},
        "content": [{"title": "Task 1", "content": "```exercise\ndescription: d\ntype: term\nasset: server-9\ntemplate:\necho hi\n```\n"}],
        "environment": {"assets": [{"name": "server-0"}]},
    }))

    labs_sync.LABS_DIR = labs_dir
    ok = labs_sync.validate_lab(lab_file)
    assert ok is False


def test_labs_sync_validate_lab_passes_with_valid_exercise(tmp_path):
    labs_sync = _load_labs_sync()

    labs_dir = tmp_path / "labs"
    labs_dir.mkdir()
    lab_file = labs_dir / "mylab.yaml"
    lab_file.write_text(_yaml.dump({
        "meta": {"title": "My Lab"},
        "content": [{"title": "Task 1", "content": "```exercise\ndescription: d\ntype: term\nasset: server-0\ntemplate:\necho hi\n```\n"}],
        "environment": {"assets": [{"name": "server-0"}]},
    }))

    labs_sync.LABS_DIR = labs_dir
    ok = labs_sync.validate_lab(lab_file)
    assert ok is True
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd scripts && python3 -m pytest test_labs_sync_exercises.py -v -k labs_sync_validate`
Expected: FAIL — `validate_lab` currently returns `True` for the missing-type case (no exercise validation exists yet), so `test_labs_sync_validate_lab_reports_exercise_errors` and `..._reports_unknown_asset` fail their `assert ok is False`.

- [ ] **Step 3: Modify `validate_lab` in `scripts/labs-sync.py`**

In `scripts/labs-sync.py`, add the import at the top:

```python
from labs_sync_exercises import extract_exercises, validate_exercise_assets
```

In `validate_lab`, inside the `else:` branch after the existing `assets` validation block (after the `for i, a in enumerate(assets):` loop, still inside `if not errors:` — actually inside the outer `else` of `if not isinstance(doc, dict)`), add exercise validation right after `content` is validated as a list and before/after the `environment`/`assets` block:

```python
                content = doc.get("content")
                if not content:
                    errors.append("missing content")
                elif not isinstance(content, list):
                    errors.append("content must be a list")
                else:
                    for i, task in enumerate(content):
                        if not task.get("title"):
                            errors.append(f"content[{i}] missing title")
                        if not task.get("content"):
                            errors.append(f"content[{i}] missing content")
                env = doc.get("environment", {})
                assets = env.get("assets", []) if isinstance(env, dict) else []
                if not assets:
                    errors.append("environment.assets is empty")
                else:
                    names = [a.get("name") for a in assets if isinstance(a, dict)]
                    seen = set()
                    for n in names:
                        if n in seen:
                            errors.append(f"environment.assets has duplicate name '{n}'")
                        seen.add(n)
                    for i, a in enumerate(assets):
                        if not isinstance(a, dict):
                            continue
                        setup = a.get("setup")
                        if setup is not None and not isinstance(setup, str):
                            errors.append(f"environment.assets[{i}].setup must be a string")
                if isinstance(content, list):
                    try:
                        exercises = extract_exercises(content)
                        asset_names = {a.get("name") for a in assets if isinstance(a, dict)}
                        errors.extend(validate_exercise_assets(exercises, asset_names))
                    except ValueError as e:
                        errors.append(f"exercise parse error: {e}")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd scripts && python3 -m pytest test_labs_sync_exercises.py -v`
Expected: PASS (all tests, including the 3 new `labs_sync_validate_lab_*` ones)

- [ ] **Step 5: Modify `upsert_lab` in `scripts/labs-sync.py`**

Add to the import line from Step 3:

```python
from labs_sync_exercises import extract_exercises, validate_exercise_assets, rewrite_content_with_placeholders
```

Change `upsert_lab`:

```python
def upsert_lab(token, path: Path):
    lid = lab_id(path)
    rel = path.relative_to(LABS_DIR)
    parts = rel.with_suffix("").parts
    parent = "_".join(parts[:-1])
    slug = parts[-1]

    with open(path) as f:
        doc = yaml.safe_load(f)

    content = doc.get("content", [])
    exercises = extract_exercises(content)
    rewritten_content = rewrite_content_with_placeholders(content)

    record = {
        "title": doc["meta"]["title"],
        "description": doc["meta"].get("description", ""),
        "content": rewritten_content,
        "environment": doc.get("environment", {}),
        "exercises": exercises,
        "type": "lab",
        "parent": parent,
        "slug": slug,
    }

    upsert_record(token, "labs", lid, record)
```

- [ ] **Step 6: Run the full test suite once more**

Run: `cd scripts && python3 -m pytest test_labs_sync_exercises.py -v`
Expected: PASS (all tests)

- [ ] **Step 7: Manually verify against a real lab file with `--verify`**

Run: `python3 scripts/labs-sync.py --verify labs/definitions` (from repo root, adjust path if `labs/definitions` isn't the sync root used elsewhere — check existing usage in CI/README first)
Expected: all labs still report `ok` (none of the existing lab YAMLs use exercise blocks yet, so behavior is unchanged for them).

- [ ] **Step 8: Commit**

```bash
git add scripts/labs-sync.py scripts/test_labs_sync_exercises.py
git commit -m "feat(labs-sync): wire exercise extraction into validate/upsert flow"
```

---

### Task 6: Add `asset` field to `grader.Task`

**Files:**
- Modify: `services/relay/grader/tasks.go`
- Modify: `services/relay/grader/tasks_test.go`

**Interfaces:**
- Produces: `grader.Task` struct gains `Asset string \`json:"asset,omitempty"\``. `LoadTasks` behavior unchanged otherwise (no validation added for `asset`).

- [ ] **Step 1: Write the failing test**

Add to `services/relay/grader/tasks_test.go`:

```go
func TestLoadTasks_asset_field_present(t *testing.T) {
	path := writeTasksFile(t, `[{"id": "task1", "type": "term", "template": "echo hi", "asset": "server-0"}]`)
	tasks, err := grader.LoadTasks(path)
	if err != nil {
		t.Fatalf("LoadTasks failed: %v", err)
	}
	if tasks[0].Asset != "server-0" {
		t.Errorf("Asset = %q, want %q", tasks[0].Asset, "server-0")
	}
}

func TestLoadTasks_asset_field_absent(t *testing.T) {
	path := writeTasksFile(t, `[{"id": "task1", "type": "term", "template": "echo hi"}]`)
	tasks, err := grader.LoadTasks(path)
	if err != nil {
		t.Fatalf("LoadTasks failed: %v", err)
	}
	if tasks[0].Asset != "" {
		t.Errorf("Asset = %q, want empty", tasks[0].Asset)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/relay && go test ./grader/... -run TestLoadTasks_asset -v`
Expected: FAIL with `tasks[0].Asset undefined (type grader.Task has no field or method Asset)`

- [ ] **Step 3: Add the field**

In `services/relay/grader/tasks.go`, modify the `Task` struct:

```go
// Task is one gradeable item loaded from tasks.json.
type Task struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Template string `json:"template"`
	Asset    string `json:"asset,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/relay && go test ./grader/... -v`
Expected: PASS (all grader tests, including the 2 new ones)

- [ ] **Step 5: Commit**

```bash
git add services/relay/grader/tasks.go services/relay/grader/tasks_test.go
git commit -m "feat(relay/grader): add optional asset field to Task"
```

---

### Task 7: Add `Exercises` to attempt-controller's domain types and CRD mapping

**Files:**
- Modify: `services/attempt-controller/internal/downstream/reconcile.go`
- Modify: `services/attempt-controller/internal/pocketbase/pbclient.go`
- Modify: `services/attempt-controller/internal/downstream/reconcile_test.go`

**Interfaces:**
- Consumes: nothing new from other tasks (this is Go-only, independent of Python/CRD-yaml changes).
- Produces:
  - `downstream.Exercise` struct: `{ID, Description, Type, Asset, Template string}` with json tags `id`, `description`, `type`, `asset`, `template`.
  - `downstream.Attempt.Exercises []Exercise` field.
  - `toLabEnvironment` sets `spec["exercises"]` on the unstructured CRD object as `[]any` of `map[string]any{"id":..., "description":..., "type":..., "asset":..., "template":...}`.
  - `AttemptRecord.Expand.Lab.Exercises json.RawMessage` field; `ToAttempt()` unmarshals it into `Attempt.Exercises`.

- [ ] **Step 1: Write the failing tests**

Add to `services/attempt-controller/internal/downstream/reconcile_test.go`:

```go
func TestToLabEnvironmentIncludesExercises(t *testing.T) {
	r, _, _ := newReconcilerWithFake()
	a := Attempt{
		ID:     "a1",
		UserID: "u1",
		LabID:  "rhcsa-lab1",
		Exercises: []Exercise{
			{ID: "1.1", Description: "Create a file", Type: "term", Asset: "server-0", Template: "test -f /tmp/x"},
			{ID: "1.2", Description: "No asset filter", Type: "term", Template: "echo hi"},
		},
	}

	obj := r.toLabEnvironment(a)

	spec := obj.Object["spec"].(map[string]any)
	exercises := spec["exercises"].([]any)
	if len(exercises) != 2 {
		t.Fatalf("len(exercises) = %d, want 2", len(exercises))
	}
	first := exercises[0].(map[string]any)
	if first["id"] != "1.1" || first["description"] != "Create a file" || first["type"] != "term" ||
		first["asset"] != "server-0" || first["template"] != "test -f /tmp/x" {
		t.Errorf("unexpected exercises[0]: %+v", first)
	}
	second := exercises[1].(map[string]any)
	if second["asset"] != "" {
		t.Errorf("exercises[1].asset = %q, want empty", second["asset"])
	}
}

func TestToLabEnvironmentEmptyExercisesIsEmptySlice(t *testing.T) {
	r, _, _ := newReconcilerWithFake()
	obj := r.toLabEnvironment(Attempt{ID: "a1"})
	spec := obj.Object["spec"].(map[string]any)
	exercises := spec["exercises"].([]any)
	if len(exercises) != 0 {
		t.Errorf("exercises = %v, want empty", exercises)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd services/attempt-controller && go test ./internal/downstream/... -run TestToLabEnvironment -v`
Expected: FAIL with `unknown field Exercises in struct literal of type Attempt` and `unknown field Asset in struct literal of type Exercise` (Exercise type doesn't exist yet).

- [ ] **Step 3: Add the `Exercise` type and `Attempt.Exercises` field**

In `services/attempt-controller/internal/downstream/reconcile.go`, add after the `Asset` struct:

```go
// Exercise is a gradeable item embedded in a lab's task markdown, extracted
// by scripts/labs_sync_exercises.py at sync time.
type Exercise struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Asset       string `json:"asset,omitempty"`
	Template    string `json:"template"`
}
```

Modify the `Attempt` struct:

```go
type Attempt struct {
	ID                 string
	UserID             string
	UserName           string
	LabID              string
	DesiredState       string
	DecommissionReason string
	Environment        EnvironmentSpec
	Exercises          []Exercise
}
```

- [ ] **Step 4: Modify `toLabEnvironment` to include exercises**

In `services/attempt-controller/internal/downstream/reconcile.go`, modify `toLabEnvironment`:

```go
// toLabEnvironment translates an Attempt into a LabEnvironment custom resource
// (lab.rootenv.io/v1alpha1). Field names must be kept in sync with
// LabEnvironmentSpec/Asset/Exercise in services/labenv-operator/api/v1alpha1/labenvironment_types.go.
func (r *Reconciler) toLabEnvironment(a Attempt) *unstructured.Unstructured {
	assets := make([]any, 0, len(a.Environment.Assets))
	for _, assetItem := range a.Environment.Assets {
		protocols := assetItem.RelayProtocols
		if protocols == nil {
			protocols = []string{}
		}
		assets = append(assets, map[string]any{
			"name":      assetItem.Name,
			"image":     assetItem.Image,
			"cpu":       assetItem.CPU,
			"memory":    assetItem.Memory,
			"disk":      assetItem.Disk,
			"setup":     assetItem.Setup,
			"protocols": protocols,
		})
	}
	exercises := make([]any, 0, len(a.Exercises))
	for _, ex := range a.Exercises {
		exercises = append(exercises, map[string]any{
			"id":          ex.ID,
			"description": ex.Description,
			"type":        ex.Type,
			"asset":       ex.Asset,
			"template":    ex.Template,
		})
	}
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "lab.rootenv.io/v1alpha1",
			"kind":       "LabEnvironment",
			"metadata": map[string]any{
				"name": a.ID,
			},
			"spec": map[string]any{
				"ownerId":   a.UserID,
				"ownerName": a.UserName,
				"labId":     a.LabID,
				"ttl":       a.Environment.Duration,
				"assets":    assets,
				"exercises": exercises,
			},
		},
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd services/attempt-controller && go test ./internal/downstream/... -v`
Expected: PASS (all downstream tests, including the 2 new ones)

- [ ] **Step 6: Wire `Exercises` through `pbclient.go`**

In `services/attempt-controller/internal/pocketbase/pbclient.go`, modify `AttemptRecord`:

```go
type AttemptRecord struct {
	ID           string `json:"id"`
	UserId       string `json:"user"`
	UserName     string `json:"userName"`
	Lab          string `json:"lab"`
	LabName      string `json:"lab_name"`
	CurrentState string `json:"current_state"`
	DesiredState string `json:"desired_state"`
	ExpiresAt          string `json:"expires_at"`
	DecommissionReason string `json:"-"` // not from PocketBase; set by the controller
	Expand             struct {
		Lab struct {
			Environment json.RawMessage `json:"environment"`
			Exercises   json.RawMessage `json:"exercises"`
		} `json:"lab"`
	} `json:"expand"`
}
```

Modify `ToAttempt()`:

```go
// ToAttempt converts the PocketBase JSON record into the controller's domain
// type. Environment and Exercises JSON are parsed here; if parsing fails the
// returned error describes a malformed or missing field.
func (r AttemptRecord) ToAttempt() (downstream.Attempt, error) {
	var env downstream.EnvironmentSpec
	if len(r.Expand.Lab.Environment) > 0 {
		if err := json.Unmarshal(r.Expand.Lab.Environment, &env); err != nil {
			return downstream.Attempt{}, fmt.Errorf("attempt %s: parse environment: %w", r.ID, err)
		}
	}
	var exercises []downstream.Exercise
	if len(r.Expand.Lab.Exercises) > 0 {
		if err := json.Unmarshal(r.Expand.Lab.Exercises, &exercises); err != nil {
			return downstream.Attempt{}, fmt.Errorf("attempt %s: parse exercises: %w", r.ID, err)
		}
	}
	return downstream.Attempt{
		ID:           r.ID,
		UserID:       r.UserId,
		UserName:     r.UserName,
		LabID:        r.Lab,
		DesiredState: r.DesiredState,
		Environment:  env,
		Exercises:    exercises,
	}, nil
}
```

- [ ] **Step 7: Write a test for `ToAttempt` parsing exercises**

Check `services/attempt-controller/internal/pocketbase/pbclient_test.go` for the existing pattern used to test `ToAttempt`, then add a parallel case. First read the file to match style:

Run: `grep -n "ToAttempt" services/attempt-controller/internal/pocketbase/pbclient_test.go`

Add a test following whatever pattern is already used there for constructing an `AttemptRecord` with `Expand.Lab.Environment` set, but for `Exercises`:

```go
func TestToAttemptParsesExercises(t *testing.T) {
	rec := AttemptRecord{
		ID: "a1",
		Expand: struct {
			Lab struct {
				Environment json.RawMessage `json:"environment"`
				Exercises   json.RawMessage `json:"exercises"`
			} `json:"lab"`
		}{},
	}
	rec.Expand.Lab.Exercises = json.RawMessage(`[{"id":"1.1","description":"d","type":"term","template":"echo hi"}]`)

	a, err := rec.ToAttempt()
	if err != nil {
		t.Fatalf("ToAttempt failed: %v", err)
	}
	if len(a.Exercises) != 1 || a.Exercises[0].ID != "1.1" {
		t.Errorf("Exercises = %+v", a.Exercises)
	}
}
```

- [ ] **Step 8: Run all attempt-controller tests**

Run: `cd services/attempt-controller && go test ./... -v`
Expected: PASS (all tests, including new ones)

- [ ] **Step 9: Commit**

```bash
git add services/attempt-controller/internal/downstream/reconcile.go \
        services/attempt-controller/internal/downstream/reconcile_test.go \
        services/attempt-controller/internal/pocketbase/pbclient.go \
        services/attempt-controller/internal/pocketbase/pbclient_test.go
git commit -m "feat(attempt-controller): copy lab exercises into LabEnvironment CRD spec"
```

---

### Task 8: Add `Exercises` to labenv-operator's CRD types

**Files:**
- Modify: `services/labenv-operator/api/v1alpha1/labenvironment_types.go`
- Regenerate: `services/labenv-operator/config/crd/bases/lab.rootenv.io_labenvironments.yaml` (via `make manifests`)
- Regenerate: `services/labenv-operator/api/v1alpha1/zz_generated.deepcopy.go` (via `make generate`, if this file exists — check first)

**Interfaces:**
- Produces: `labv1alpha1.Exercise` struct: `{ID, Description, Type, Asset, Template string}` with json tags `id`, `description`, `type`, `asset,omitempty`, `template`. `LabEnvironmentSpec.Exercises []Exercise` with json tag `exercises,omitempty`.

- [ ] **Step 1: Check whether `zz_generated.deepcopy.go` exists**

Run: `ls services/labenv-operator/api/v1alpha1/ | grep deepcopy`

Note the result — Step 4 below only applies if this file exists.

- [ ] **Step 2: Modify `labenvironment_types.go`**

In `services/labenv-operator/api/v1alpha1/labenvironment_types.go`, add the `Exercise` type after the `Asset` type (after line 66):

```go
// Exercise is a gradeable item embedded in a lab's task markdown, copied
// verbatim from the labs PocketBase record's `exercises` field via
// attempt-controller.
type Exercise struct {
	// ID identifies this exercise, computed as "<task#>.<exercise#>" at lab-sync time.
	ID string `json:"id"`
	// Description is shown to the user; never used by the grader itself.
	Description string `json:"description"`
	// Type must currently be "term" — the only type relay-grader supports.
	Type string `json:"type"`
	// Asset optionally scopes this exercise's check to one asset's terminal.
	// Empty means the grader does not filter by terminal.
	Asset string `json:"asset,omitempty"`
	// Template is the shell check the grader runs to determine completion.
	Template string `json:"template"`
}
```

Modify `LabEnvironmentSpec` to add the field after `Assets`:

```go
// LabEnvironmentSpec defines the desired state of LabEnvironment
type LabEnvironmentSpec struct {
	// OwnerId is the ID of the user who owns this lab environment.
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9](?:[-A-Za-z0-9_.]*[A-Za-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	OwnerId string `json:"ownerId"`
	// OwnerName is the name of the user who owns this lab environment (for display purposes only).
	OwnerName string `json:"ownerName,omitempty"`
	// LabId identifies which lab definition to provision.
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9](?:[-A-Za-z0-9_.]*[A-Za-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	LabId string `json:"labId"`
	// TTL (minutes) is the time-to-live for this lab environment, after which it should be automatically deleted.
	// +kubebuilder:default=60
	TTL int32 `json:"ttl,omitempty"`
	// Assets is a list of assets that should be provisioned as part of this lab environment.
	Assets []Asset `json:"assets"`
	// Exercises is a list of gradeable exercises extracted from the lab's task markdown.
	Exercises []Exercise `json:"exercises,omitempty"`
}
```

- [ ] **Step 3: Regenerate CRD manifests**

Run: `cd services/labenv-operator && make manifests`
Expected: exits 0, modifies `config/crd/bases/lab.rootenv.io_labenvironments.yaml` to include the new `exercises` array property under `spec.properties`.

- [ ] **Step 4: Regenerate deepcopy code (only if `zz_generated.deepcopy.go` exists per Step 1)**

Run: `cd services/labenv-operator && make generate`
Expected: exits 0, modifies `zz_generated.deepcopy.go` to add `DeepCopy`/`DeepCopyInto` handling for the new `Exercise` type and `Exercises` slice field.

- [ ] **Step 5: Build to confirm everything compiles**

Run: `cd services/labenv-operator && go build ./...`
Expected: exits 0, no errors.

- [ ] **Step 6: Commit**

```bash
git add services/labenv-operator/api/v1alpha1/labenvironment_types.go \
        services/labenv-operator/config/crd/bases/lab.rootenv.io_labenvironments.yaml
git add services/labenv-operator/api/v1alpha1/zz_generated.deepcopy.go 2>/dev/null || true
git commit -m "feat(labenv-operator): add Exercises field to LabEnvironmentSpec"
```

---

### Task 9: Serialize `spec.exercises` into `grader-tasks` ConfigMap

**Files:**
- Modify: `services/labenv-operator/internal/controller/grader.go`
- Modify: `services/labenv-operator/internal/controller/labenvironment_controller_test.go`

**Interfaces:**
- Consumes: `labv1alpha1.LabEnvironmentSpec.Exercises []Exercise` (Task 8), `grader.Task{ID, Type, Template, Asset}` shape (Task 6, for reference — labenv-operator builds its own equivalent map, it does not import the `grader` Go package).
- Produces: `ensureGraderTasksConfigMap` takes `env *labv1alpha1.LabEnvironment` as a new parameter and writes `tasks.json` derived from `env.Spec.Exercises` (fields `id`, `type`, `template`, `asset` — `description` excluded).

- [ ] **Step 1: Write the failing test — replace the placeholder assertion**

In `services/labenv-operator/internal/controller/labenvironment_controller_test.go`, modify the existing `It("creates all relay-grader resources", ...)` test (around line 223-289): change the `env` construction to include `Exercises`, and change the ConfigMap assertion.

Find this block (around line 230-237):

```go
			env := &labv1alpha1.LabEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: envName},
				Spec: labv1alpha1.LabEnvironmentSpec{
					OwnerId: "usr-test",
					LabId:   "test-lab",
					Assets:  []labv1alpha1.Asset{{Name: "main", Image: "busybox"}},
				},
			}
```

Replace with:

```go
			env := &labv1alpha1.LabEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: envName},
				Spec: labv1alpha1.LabEnvironmentSpec{
					OwnerId: "usr-test",
					LabId:   "test-lab",
					Assets:  []labv1alpha1.Asset{{Name: "main", Image: "busybox"}},
					Exercises: []labv1alpha1.Exercise{
						{ID: "1.1", Description: "Create a file", Type: "term", Asset: "main", Template: "test -f /tmp/x"},
						{ID: "1.2", Description: "No asset filter", Type: "term", Template: "echo hi"},
					},
				},
			}
```

Find this block (around line 242-246):

```go
			By("ConfigMap grader-tasks exists with placeholder tasks.json")
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "grader-tasks"}, &cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey("tasks.json"))
			Expect(cm.Data["tasks.json"]).To(ContainSubstring("task1"))
```

Replace with:

```go
			By("ConfigMap grader-tasks exists with tasks.json derived from spec.exercises")
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "grader-tasks"}, &cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey("tasks.json"))
			var tasks []map[string]any
			Expect(json.Unmarshal([]byte(cm.Data["tasks.json"]), &tasks)).To(Succeed())
			Expect(tasks).To(HaveLen(2))
			Expect(tasks[0]["id"]).To(Equal("1.1"))
			Expect(tasks[0]["type"]).To(Equal("term"))
			Expect(tasks[0]["template"]).To(Equal("test -f /tmp/x"))
			Expect(tasks[0]["asset"]).To(Equal("main"))
			Expect(tasks[0]).NotTo(HaveKey("description"))
			Expect(tasks[1]).NotTo(HaveKey("asset"))
```

Add a new test after this `It` block (before the closing of the `Describe("ensureRelayGrader", ...)` block) for the empty-exercises case:

```go
	It("writes an empty tasks.json array when spec.exercises is empty", func() {
		envName := "grader-empty-exercises-test"
		nsName := "rootenv-lab-" + envName
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		env := &labv1alpha1.LabEnvironment{
			ObjectMeta: metav1.ObjectMeta{Name: envName},
			Spec: labv1alpha1.LabEnvironmentSpec{
				OwnerId: "usr-test",
				LabId:   "test-lab",
				Assets:  []labv1alpha1.Asset{{Name: "main", Image: "busybox"}},
			},
		}

		r := &LabEnvironmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		Expect(r.ensureRelayGrader(ctx, env, nsName)).To(Succeed())

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "grader-tasks"}, &cm)).To(Succeed())
		Expect(cm.Data["tasks.json"]).To(Equal("[]"))
	})
```

Add `"encoding/json"` to the imports of `labenvironment_controller_test.go` if not already present — check first:

Run: `grep -n '"encoding/json"' services/labenv-operator/internal/controller/labenvironment_controller_test.go`

If absent, add it to the import block.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd services/labenv-operator && make test` (this wires up `KUBEBUILDER_ASSETS` via `setup-envtest` before running `go test ./...`, required for the `Describe("ensureRelayGrader", ...)` Ginkgo suite in `suite_test.go` which uses a real envtest API server)

Expected: FAIL — `ensureGraderTasksConfigMap` still writes the hardcoded placeholder, so the new assertions (`tasks[0]["id"] == "1.1"`, empty-array case) fail. The build itself still succeeds at this point (we haven't touched `grader.go` yet) — the failure is a Ginkgo assertion failure, not a compile error.

- [ ] **Step 3: Modify `ensureGraderTasksConfigMap` in `grader.go`**

In `services/labenv-operator/internal/controller/grader.go`, remove the `graderTasksPlaceholder` constant (line 23) and add `"encoding/json"` to imports. Modify the function:

```go
func (r *LabEnvironmentReconciler) ensureGraderTasksConfigMap(ctx context.Context, env *labv1alpha1.LabEnvironment, nsName string) error {
	var existing corev1.ConfigMap
	err := r.Get(ctx, client.ObjectKey{Namespace: nsName, Name: "grader-tasks"}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	type graderTask struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Template string `json:"template"`
		Asset    string `json:"asset,omitempty"`
	}
	tasks := make([]graderTask, 0, len(env.Spec.Exercises))
	for _, ex := range env.Spec.Exercises {
		tasks = append(tasks, graderTask{
			ID:       ex.ID,
			Type:     ex.Type,
			Template: ex.Template,
			Asset:    ex.Asset,
		})
	}
	tasksJSON, err := json.Marshal(tasks)
	if err != nil {
		return fmt.Errorf("marshal grader tasks: %w", err)
	}

	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grader-tasks",
			Namespace: nsName,
		},
		Data: map[string]string{
			"tasks.json": string(tasksJSON),
		},
	}
	return client.IgnoreAlreadyExists(r.Create(ctx, &cm))
}
```

Update the call site in `ensureRelayGrader` (around line 68):

```go
	if err := r.ensureGraderTasksConfigMap(ctx, env, nsName); err != nil {
		return err
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd services/labenv-operator && make test`
Expected: PASS — all `ensureRelayGrader` tests, including the 2 modified/new ones, and all other packages.

- [ ] **Step 6: Commit**

```bash
git add services/labenv-operator/internal/controller/grader.go \
        services/labenv-operator/internal/controller/labenvironment_controller_test.go
git commit -m "feat(labenv-operator): derive grader-tasks ConfigMap from spec.exercises"
```

---

### Task 10: End-to-end manual verification

**Files:** none (verification only)

- [ ] **Step 1: Add an exercise block to a real lab file for manual testing**

Temporarily edit `labs/definitions/ex200/rhcsa1.yaml`'s first task (`Inspect Current Permissions`) to add, at the end of its `content` string:

```yaml
      ```exercise
      description: List the permissions of /etc/shadow
      type: term
      asset: server-0
      template:
      ls -l /etc/shadow
      ```
```

- [ ] **Step 2: Run labs-sync.py --verify against it**

Run: `python3 scripts/labs-sync.py --verify labs/definitions`
Expected: `ok` for `ex200_rhcsa1`, no errors.

- [ ] **Step 3: Run a real sync against a local PocketBase instance**

Start PocketBase locally per the project's existing dev workflow, then run: `python3 scripts/labs-sync.py labs/definitions` (with `POCKETBASE_URL`/admin credentials set per `scripts/.env`).
Expected: `updated ex200_rhcsa1` (or `created` on first run).

- [ ] **Step 4: Confirm via PocketBase admin UI or API**

Fetch the `labs` record for `ex200_rhcsa1` directly (not via `labs_userview`) and confirm:
- `exercises` contains one entry: `{"id": "1.1", "description": "List the permissions of /etc/shadow", "type": "term", "asset": "server-0", "template": "ls -l /etc/shadow"}`
- `content` for the first task contains a stripped placeholder (`id: 1.1`, `description: ...`) with no `type`/`asset`/`template`.

Fetch the same lab via `labs_userview` (public endpoint) and confirm the response has no `exercises` field at all, and `content`'s exercise block shows only `id`/`description`.

- [ ] **Step 5: Revert the temporary test edit to the lab file**

```bash
git checkout labs/definitions/ex200/rhcsa1.yaml
```

(No commit — this task is verification-only, and the lab file edit was never meant to be a permanent example content change unless the user wants to keep it as a demo. Ask before keeping it.)
