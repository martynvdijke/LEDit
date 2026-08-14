"""Configuration and small helpers read from environment variables."""

import logging
import os
import sys

# All device log output goes through this logger. When OTel telemetry is
# enabled (see telemetry.py) an OTLP handler with trace-context correlation
# is attached; the stderr handler below keeps console output unchanged.
_logger = logging.getLogger("ledit_device")
_logger.setLevel(logging.INFO)
if not _logger.handlers:
    _handler = logging.StreamHandler(sys.stderr)
    _handler.setFormatter(
        logging.Formatter("[%(asctime)s] %(message)s", datefmt="%H:%M:%S")
    )
    _logger.addHandler(_handler)


def log(level, msg):
    getattr(_logger, level, _logger.info)(msg)


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
