import io
import rbg


def test_render_string_content():
    line = '{"type": "user", "message": {"role": "user", "content": "hello"}}'
    assert rbg.render_line(line) == "user: hello"


def test_render_text_block_list():
    line = (
        '{"type": "assistant", "message": {"role": "assistant",'
        ' "content": [{"type": "text", "text": "hi there"}]}}'
    )
    assert rbg.render_line(line) == "assistant: hi there"


def test_render_tool_use_block():
    line = (
        '{"message": {"role": "assistant",'
        ' "content": [{"type": "tool_use", "name": "Bash"}]}}'
    )
    assert rbg.render_line(line) == "assistant: [tool: Bash]"


def test_render_skips_blank_unparseable_and_empty():
    assert rbg.render_line("") is None
    assert rbg.render_line("   ") is None
    assert rbg.render_line("{bad json") is None
    assert rbg.render_line('{"type": "system", "message": {"content": []}}') is None


def test_render_tolerates_unknown_keys():
    line = '{"type": "assistant", "weird": 1, "message": {"role": "assistant", "content": "ok"}}'
    assert rbg.render_line(line) == "assistant: ok"


def test_render_stream_prints_only_renderable():
    lines = [
        '{"message": {"role": "user", "content": "q"}}',
        "garbage",
        '{"message": {"role": "assistant", "content": "a"}}',
    ]
    buf = io.StringIO()
    rbg.render_stream(lines, out=buf)
    assert buf.getvalue() == "user: q\nassistant: a\n"
