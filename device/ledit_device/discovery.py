"""mDNS discovery advertisement and provisioning poll."""

from __future__ import annotations

import os
import secrets
import time
import logging
import urllib.request
import urllib.parse
import urllib.error
import json

from . import config as _config

logger = logging.getLogger("ledit_device")

# Overridable paths for tests.
MACHINE_ID_PATH = "/etc/machine-id"
DEVICE_ID_FILE = None  # None means use config dir; tests may override string
CONFIG_DIR_ENV = "LEDIT_CONFIG_DIR"

# nonce cached per boot
_nonce: str | None = None
_warned_zeroconf = False


def _config_dir() -> str:
    return os.getenv(CONFIG_DIR_ENV, os.path.expanduser("~/.config/ledit"))


def _device_id_file() -> str:
    if DEVICE_ID_FILE is not None:
        return DEVICE_ID_FILE
    return os.path.join(_config_dir(), "device_id")


def fingerprint(machine_id_path: str | None = None, device_id_file: str | None = None) -> str:
    """Stable device fingerprint.

    Tries /etc/machine-id; falls back to persisted random id.
    """
    path = machine_id_path if machine_id_path is not None else MACHINE_ID_PATH
    try:
        if os.path.exists(path):
            with open(path, "r") as f:
                val = f.read().strip()
                if val:
                    return val
    except Exception:
        pass
    # Fallback: persisted random id
    df = device_id_file if device_id_file is not None else _device_id_file()
    try:
        if os.path.exists(df):
            with open(df, "r") as f:
                val = f.read().strip()
                if val:
                    return val
    except Exception:
        pass
    # create new
    new_id = secrets.token_hex(16)
    try:
        os.makedirs(os.path.dirname(df) or ".", exist_ok=True)
        with open(df, "w") as f:
            f.write(new_id)
    except Exception:
        pass
    return new_id


def new_nonce() -> str:
    global _nonce
    if _nonce is None:
        _nonce = secrets.token_hex(16)
    return _nonce


def _reset_nonce_for_test():
    global _nonce
    _nonce = None


# -- advertiser -----------------------------------------------------------

class Advertiser:
    def __init__(self, fingerprint_value: str, nonce_value: str, model: str = "ledit-device", port: int = 8080):
        self.fingerprint_value = fingerprint_value
        self.nonce_value = nonce_value
        self.model = model
        self.port = port
        self._zeroconf = None
        self._info = None

    def start(self):
        global _warned_zeroconf
        try:
            from zeroconf import Zeroconf, ServiceInfo
        except Exception as e:
            if not _warned_zeroconf:
                logger.warning("zeroconf unavailable, discovery disabled: %s", e)
                _warned_zeroconf = True
            return None
        try:
            from ledit_device import __version__
        except Exception:
            __version__ = "0.0.0"
        props = {
            "id": fingerprint_value if (fingerprint_value := self.fingerprint_value) else "",
            "model": self.model,
            "version": __version__,
            "proto": "2",
            "nonce": self.nonce_value,
        }
        # zeroconf expects bytes
        props_b = {k.encode(): v.encode() if isinstance(v, str) else str(v).encode() for k, v in props.items()}
        # name must be unique; use fingerprint prefix
        name = "ledit-%s._ledit._tcp.local." % self.fingerprint_value[:8]
        zc = None
        try:
            info = ServiceInfo(
                "_ledit._tcp.local.",
                name,
                addresses=[],
                port=self.port,
                properties=props_b,
            )
            zc = Zeroconf()
            zc.register_service(info)
            self._zeroconf = zc
            self._info = info
            return self
        except Exception as e:
            if not _warned_zeroconf:
                logger.warning("mDNS advertise failed (multicast unavailable): %s", e)
                _warned_zeroconf = True
            try:
                if zc is not None:
                    zc.close()
            except Exception:
                pass
            return None

    def stop(self):
        if self._zeroconf and self._info:
            try:
                self._zeroconf.unregister_service(self._info)
            except Exception:
                pass
            try:
                self._zeroconf.close()
            except Exception:
                pass
            self._zeroconf = None
            self._info = None


def start_advertising(fingerprint_value=None, nonce_value=None, model="ledit-device", port=8080):
    fp = fingerprint_value or fingerprint()
    nc = nonce_value or new_nonce()
    adv = Advertiser(fp, nc, model=model, port=port)
    result = adv.start()
    if result is None:
        return None
    return adv


def stop_advertising(adv):
    if adv is None:
        return
    try:
        adv.stop()
    except Exception:
        pass


# -- provisioning ---------------------------------------------------------

def provision(server_url: str, fingerprint_value: str, nonce: str, interval: float = 2.0, timeout: float = 300) -> str | None:
    """Poll provisioning endpoint until token returned or timeout.

    Returns token string or None on timeout.
    """
    deadline = time.time() + timeout
    base = server_url.rstrip("/")
    # server_url may be ws://... convert to http
    if base.startswith("ws://"):
        base = "http://" + base[len("ws://"):]
    elif base.startswith("wss://"):
        base = "https://" + base[len("wss://"):]
    while time.time() < deadline:
        url = "%s/api/device/provision?fingerprint=%s&nonce=%s" % (
            base, urllib.parse.quote(fingerprint_value), urllib.parse.quote(nonce)
        )
        try:
            with urllib.request.urlopen(url, timeout=5) as resp:
                code = resp.status
                body = resp.read().decode(errors="replace")
                if code == 200:
                    try:
                        data = json.loads(body) if body else {}
                    except Exception:
                        data = {}
                    token = data.get("token") if isinstance(data, dict) else None
                    if token and isinstance(token, str) and token.strip():
                        tok = token.strip()
                        # persist
                        try:
                            _config.save_token(tok)
                        except Exception:
                            pass
                        return tok
                elif code == 204:
                    pass
                elif code == 429:
                    pass
        except urllib.error.HTTPError as e:
            # 204, 429, 404 etc - just wait
            pass
        except Exception:
            pass
        # wait interval but respect deadline
        remaining = deadline - time.time()
        if remaining <= 0:
            break
        time.sleep(min(interval, remaining))
    return None
