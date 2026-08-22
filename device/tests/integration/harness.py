"""Integration harness: ephemeral LEDit server for device tests."""
from __future__ import annotations

import json
import logging
import os
import signal
import socket
import subprocess
import sys
import time
import urllib.request
import urllib.error
import urllib.parse
from pathlib import Path

log = logging.getLogger("harness")
logging.basicConfig(level=logging.INFO, format="[harness] %(message)s")


def _find_free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _find_binary() -> str | None:
    candidates = [
        Path("/root/projects/LEDit/ledit"),
        Path("/tmp/ledit-test"),
        Path("./ledit"),
    ]
    for c in candidates:
        if c.exists() and c.is_file():
            return str(c.resolve())
    return None


def _build_binary(dest: str = "/tmp/ledit-test") -> str | None:
    """Build ./ledit if missing."""
    try:
        log.info("building ledit binary -> %s", dest)
        result = subprocess.run(
            ["go", "build", "-o", dest, "."],
            cwd="/root/projects/LEDit",
            capture_output=True,
            text=True,
            timeout=120,
        )
        if result.returncode != 0:
            log.warning("go build failed: %s %s", result.stdout, result.stderr)
            return None
        Path(dest).chmod(0o755)
        return dest
    except Exception as e:
        log.warning("build error: %s", e)
        return None


def stop_server(proc: subprocess.Popen | None):
    if proc is None:
        return
    if proc.poll() is not None:
        return
    try:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            log.warning("SIGTERM timeout, SIGKILL")
            proc.kill()
            proc.wait(timeout=5)
    except Exception as e:
        log.warning("stop_server error: %s", e)
        try:
            proc.kill()
        except Exception:
            pass


def start_server(tmp_path, timeout: float = 10):
    """
    Start LEDit server on ephemeral port.

    Returns dict {url, ws_url, proc, cleanup, data_dir, port, api_token}
    """
    tmp_path = Path(tmp_path)
    data_dir = tmp_path / "data"
    data_dir.mkdir(parents=True, exist_ok=True)

    binary = _find_binary()
    if binary is None:
        binary = _build_binary()
    if binary is None:
        raise RuntimeError("ledit binary not found and build failed")

    port = _find_free_port()
    env = os.environ.copy()
    env["LEDIT_AUTH_DISABLE"] = "true"
    # db uses LEDIT_DB_DIR
    env["LEDIT_DB_DIR"] = str(data_dir)
    env["LEDIT_DATA_DIR"] = str(data_dir)
    env["LEDIT_PORT"] = str(port)

    log.info("starting %s on port %s data_dir=%s", binary, port, data_dir)
    proc = subprocess.Popen(
        [binary],
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        cwd="/root/projects/LEDit",
    )

    url = f"http://127.0.0.1:{port}"
    ws_url = f"ws://127.0.0.1:{port}"

    # poll GET / until 200
    deadline = time.time() + timeout
    last_err = None
    while time.time() < deadline:
        if proc.poll() is not None:
            out = ""
            try:
                out = proc.stdout.read().decode(errors="replace") if proc.stdout else ""
            except Exception:
                pass
            raise RuntimeError(f"server exited early (code {proc.returncode}): {out[:2000]}")
        try:
            with urllib.request.urlopen(url + "/", timeout=1) as resp:
                if resp.status == 200:
                    break
        except Exception as e:
            last_err = e
        time.sleep(0.2)
    else:
        stop_server(proc)
        raise RuntimeError(f"server not ready within {timeout}s: {last_err}")

    log.info("server ready at %s", url)

    def cleanup():
        stop_server(proc)
        # tmp_path cleanup handled by pytest

    return {"url": url, "ws_url": ws_url, "proc": proc, "cleanup": cleanup, "data_dir": str(data_dir), "port": port}


def seed_settings(url: str, width: int = 64, height: int = 64, timeout_val: float = 1.0, random: bool = False):
    """Seed GeneralSettings via POST /admin/settings form."""
    data = urllib.parse.urlencode({
        "width": str(width),
        "height": str(height),
        "timeout": str(timeout_val),
        "random": "on" if random else "",
        "eink_mode": "",
    }).encode()
    req = urllib.request.Request(url + "/admin/settings", data=data, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            return resp.status
    except urllib.error.HTTPError as e:
        return e.code
    except Exception as e:
        log.warning("seed_settings error: %s", e)
        raise


def create_api_token(url: str) -> str | None:
    """Create API token via POST /admin/api-tokens; returns secret or None."""
    data = urllib.parse.urlencode({"name": "integration-test"}).encode()
    req = urllib.request.Request(url + "/admin/api-tokens", data=data, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            body = resp.read().decode()
            j = json.loads(body)
            return j.get("secret")
    except Exception as e:
        log.warning("create_api_token failed: %s", e)
        return None


def create_device(url: str, name: str = "test-device", width: int = 64, height: int = 64, refresh_interval: int = 1) -> dict | None:
    """Create device via POST /admin/devices/new; returns {id, token} if parseable."""
    data = urllib.parse.urlencode({
        "name": name,
        "ip": "127.0.0.1",
        "port": "6270",
        "width": str(width),
        "height": str(height),
        "refresh_interval": str(refresh_interval),
        "enabled": "on",
    }).encode()
    req = urllib.request.Request(url + "/admin/devices/new", data=data, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")
    try:
        with urllib.request.urlopen(req, timeout=5):
            pass
    except Exception as e:
        # redirect raises HTTPError 302 in some handlers; ignore
        if isinstance(e, urllib.error.HTTPError) and e.code in (302, 303):
            pass
        else:
            log.warning("create_device post error: %s", e)
    # fetch device list to find id/token
    return get_device_by_name(url, name)


def get_device_by_name(url: str, name: str) -> dict | None:
    """Fetch device id/token by querying /admin/devices page or ent via API fallback."""
    # Try to list via DB: query ent directly not possible; scrape /admin/devices HTML
    try:
        with urllib.request.urlopen(url + "/admin/devices", timeout=5) as resp:
            html = resp.read().decode(errors="replace")
            # Find device name and extract id from /admin/devices/<id>/edit link
            import re
            # find all device ids
            ids = re.findall(r'/admin/devices/(\d+)/edit', html)
            # crude: if html contains name, return first id; try to also extract token
            tokens = re.findall(r'[0-9a-fA-F]{16,}', html)
            # token is per device displayed; use DB query via preview page if needed
            if ids:
                dev_id = int(ids[-1])
                # try to get token by querying DB file via sqlite? fallback: try to fetch device preview page
                token = None
                # tokens found may include device tokens
                if tokens:
                    token = tokens[-1]
                return {"id": dev_id, "token": token}
    except Exception as e:
        log.warning("get_device_by_name error: %s", e)
    return None


def get_device_token_from_db(data_dir: str, device_id: int) -> str | None:
    """Read token directly from sqlite db."""
    import sqlite3
    db_path = os.path.join(data_dir, "ledit.db")
    if not os.path.exists(db_path):
        return None
    try:
        con = sqlite3.connect(db_path)
        cur = con.cursor()
        cur.execute("SELECT token FROM device_settings WHERE id=?", (device_id,))
        row = cur.fetchone()
        con.close()
        if row:
            return row[0]
    except Exception as e:
        log.warning("get_device_token_from_db: %s", e)
    return None


def api_post(url: str, path: str, token: str | None = None, json_body: dict | None = None):
    """POST to API with optional bearer token."""
    full = url + path
    body = json.dumps(json_body or {}).encode() if json_body is not None else b"{}"
    req = urllib.request.Request(full, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            return resp.status, resp.read().decode()
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace")
    except Exception as e:
        return 0, str(e)
