"""Configuration and small helpers read from environment variables."""

import os
import sys
import time


def log(level, msg):
    sys.stderr.write("[%s] %s\n" % (time.strftime("%H:%M:%S"), msg))


def env_int(name, default):
    try:
        return int(os.getenv(name, default))
    except (TypeError, ValueError):
        return default


def token():
    t = os.getenv("LEDIT_TOKEN", "").strip()
    if not t:
        sys.stderr.write("LEDIT_TOKEN is required (copy it from the admin Devices page)\n")
        sys.exit(1)
    return t


def server_url():
    return os.getenv("LEDIT_SERVER", "ws://localhost:8080").rstrip("/")
