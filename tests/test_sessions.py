import rbg


def test_load_missing_returns_empty(tmp_path):
    assert rbg.load_sessions(path=str(tmp_path / "none.json")) == {}


def test_load_corrupt_returns_empty(tmp_path):
    p = tmp_path / "sessions.json"
    p.write_text("{not json")
    assert rbg.load_sessions(path=str(p)) == {}


def test_save_then_load_roundtrip(tmp_path):
    p = tmp_path / "sub" / "sessions.json"  # parent dir does not exist yet
    rbg.save_sessions({"alpha": "id-1"}, path=str(p))
    assert rbg.load_sessions(path=str(p)) == {"alpha": "id-1"}
