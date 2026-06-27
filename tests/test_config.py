import pytest
import rbg


def test_env_overrides_file(tmp_path):
    conf = tmp_path / "rbg.conf"
    conf.write_text('RBG_HOST=fromfile\nRBG_CWD=/proj\n')
    cfg = rbg.load_config(env={"RBG_HOST": "fromenv"}, conf_path=str(conf))
    assert cfg.host == "fromenv"      # env wins
    assert cfg.cwd == "/proj"         # file fills the gap


def test_ssh_opts_split(tmp_path):
    conf = tmp_path / "rbg.conf"
    conf.write_text('RBG_HOST=h\nRBG_SSH=-p 2222 -i ~/k\n')
    cfg = rbg.load_config(env={}, conf_path=str(conf))
    assert cfg.ssh_opts == ["-p", "2222", "-i", "~/k"]


def test_missing_host_raises(tmp_path):
    conf = tmp_path / "nope.conf"
    with pytest.raises(rbg.ConfigError):
        rbg.load_config(env={}, conf_path=str(conf))


def test_quoted_values_and_comments(tmp_path):
    conf = tmp_path / "rbg.conf"
    conf.write_text('# comment\nRBG_HOST="quoted-host"\n\n')
    cfg = rbg.load_config(env={}, conf_path=str(conf))
    assert cfg.host == "quoted-host"
