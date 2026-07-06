"""Extraction, validation, and rewriting of ```exercise blocks in lab task markdown."""

import re

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
