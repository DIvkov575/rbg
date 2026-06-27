#!/usr/bin/env python3
"""rbg — manage remote Claude --bg agents from the laptop."""
import json
import os
import shlex
import subprocess
import sys
from dataclasses import dataclass, field
