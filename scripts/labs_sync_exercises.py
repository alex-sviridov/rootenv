"""Extraction, validation, and rewriting of ```exercise blocks in lab task markdown."""

import re
import copy

_LABELS = ("description", "type", "asset")
_FENCE_RE = re.compile(r'```exercise\n(.*?)\n```', re.DOTALL)


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
