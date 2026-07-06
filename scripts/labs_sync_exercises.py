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
