#!/usr/bin/env python3
"""rbg — manage remote Claude --bg agents from the laptop."""
import json
import os
import shlex
import subprocess
import sys
from dataclasses import dataclass, field


class ConfigError(Exception):
    pass


@dataclass
class Config:
    host: str
    cwd: str = ""
    ssh_opts: list = field(default_factory=list)


def _read_conf_file(path):
    vals = {}
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, _, value = line.partition("=")
                vals[key.strip()] = value.strip().strip('"').strip("'")
    except FileNotFoundError:
        pass
    return vals


def load_config(env=None, conf_path=None):
    env = os.environ if env is None else env
    conf_path = conf_path or os.path.expanduser("~/.rbg.conf")
    file_vals = _read_conf_file(conf_path)

    def get(key):
        return env.get(key) if env.get(key) is not None else file_vals.get(key, "")

    host = get("RBG_HOST")
    if not host:
        raise ConfigError("RBG_HOST not set (export it or put it in ~/.rbg.conf)")
    return Config(host=host, cwd=get("RBG_CWD"), ssh_opts=shlex.split(get("RBG_SSH")))
