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
