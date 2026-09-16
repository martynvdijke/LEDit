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


def env_bool(name, default=False):
    """Parse a boolean-ish env var (1/true/yes/on, case-insensitive)."""
    val = os.getenv(name)
    if val is None:
        return default
    return val.strip().lower() in ("1", "true", "yes", "on")


def spectrum_enabled():
    """Opt-in audio spectrum tap (default off)."""
    return env_bool("LEDIT_SPECTRUM", False)


def _config_dir():
    return os.getenv("LEDIT_CONFIG_DIR", os.path.expanduser("~/.config/ledit"))


def _token_file():
    return os.path.join(_config_dir(), "device_token")


def save_token(value: str):
    path = _token_file()
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w") as f:
        f.write(value.strip())
    try:
        os.chmod(path, 0o600)
    except Exception:
        pass


def token():
    t = os.getenv("LEDIT_TOKEN", "").strip()
    if t:
        return t
    # fallback to persisted token file
    tf = _token_file()
    try:
        if os.path.exists(tf):
            with open(tf, "r") as f:
                v = f.read().strip()
                if v:
                    return v
    except Exception:
        pass
    sys.stderr.write("LEDIT_TOKEN is required (copy it from the admin Devices page)\n")
    sys.exit(1)


def token_optional():
    """Return token if available (env or file), else None without exiting."""
    t = os.getenv("LEDIT_TOKEN", "").strip()
    if t:
        return t
    tf = _token_file()
    try:
        if os.path.exists(tf):
            with open(tf, "r") as f:
                v = f.read().strip()
                if v:
                    return v
    except Exception:
        pass
    return None


def server_url():
    return os.getenv("LEDIT_SERVER", "ws://localhost:8080").rstrip("/")


def update_interval():
    return env_int("LEDIT_UPDATE_INTERVAL", 3600)


def update_channel():
    return os.getenv("LEDIT_UPDATE_CHANNEL", "").strip()
