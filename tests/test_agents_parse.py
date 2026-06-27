import rbg


def test_parse_bare_array():
    agents = rbg.parse_agents('[{"name": "a", "sessionId": "x"}]')
    assert agents == [{"name": "a", "sessionId": "x"}]


def test_parse_agents_wrapped_object():
    agents = rbg.parse_agents('{"agents": [{"name": "a", "id": "x"}]}')
    assert agents == [{"name": "a", "id": "x"}]


def test_parse_empty_or_garbage():
    assert rbg.parse_agents("") == []
    assert rbg.parse_agents("not json") == []


def test_find_agent_id_prefers_known_keys():
    agents = [
        {"name": "alpha", "session_id": "sid-alpha"},
        {"name": "beta", "id": "sid-beta"},
    ]
    assert rbg.find_agent_id(agents, "alpha") == "sid-alpha"
    assert rbg.find_agent_id(agents, "beta") == "sid-beta"


def test_find_agent_id_missing_returns_none():
    assert rbg.find_agent_id([{"name": "alpha", "id": "x"}], "ghost") is None
