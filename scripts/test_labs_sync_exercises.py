import importlib.util

import pytest
import yaml as _yaml
from pathlib import Path

from labs_sync_exercises import parse_exercise_block, extract_exercises, validate_exercise_assets, rewrite_content_with_placeholders


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
