"""Live library <-> server integration.

Unlike the other integration tests, which speak the raw protocol with the
asyncio ``websockets`` client and call ``Client.on_message`` by hand, these
drive the *real* shipped code path: ``build_ws_url`` + ``websocket.WebSocketApp``
+ ``Client`` callbacks, and the ``python -m ledit_device`` entry point end to end.
"""
import os
import subprocess
import sys
import threading
import time
from pathlib import Path

import pytest

from ledit_device.client import Client, build_ws_url
from ledit_device.display import Display

from .harness import (
    start_server,
    stop_server,
    seed_settings,
    create_device,
    get_device_token_from_db,
)


class RecordingDisplay(Display):
    """In-memory display: records frames and brightness hints."""

    def __init__(self, width=64, height=64):
        self._w = width
        self._h = height
        self.frames = 0
        self.brightness = []

    @property
    def width(self):
        return self._w

    @property
    def height(self):
        return self._h

    def show(self, image):
        self.frames += 1

    def set_brightness(self, level):
        self.brightness.append(level)


def _wait(pred, timeout=8.0, interval=0.05):
    end = time.time() + timeout
    while time.time() < end:
        if pred():
            return True
        time.sleep(interval)
    return False


def _start_wsapp(url, client):
    """Run the library's real transport (websocket-client) in a thread."""
    import websocket

    ws = websocket.WebSocketApp(
        url,
        on_message=client.on_message,
        on_error=client.on_error,
        on_close=client.on_close,
    )
    thread = threading.Thread(
        target=ws.run_forever, kwargs={"ping_interval": 30}, daemon=True
    )
    thread.start()
    return ws


def _device(srv, name):
    info = create_device(srv["url"], name=name, width=64, height=64, refresh_interval=1)
    if not info:
        pytest.skip("device creation failed")
    token = get_device_token_from_db(srv["data_dir"], info["id"]) or info.get("token")
    if not token:
        pytest.skip("no device token")
    return token


@pytest.mark.integration
def test_client_negotiates_v2_and_applies_brightness(tmp_path):
    """The real Client<->server handshake: welcome, v2 caps, frames, brightness."""
    try:
        import websocket  # noqa: F401
    except ImportError:
        pytest.skip("websocket-client not installed")

    srv = None
    try:
        srv = start_server(tmp_path / "srv")
        seed_settings(srv["url"], width=64, height=64, timeout_val=1)
        token = _device(srv, "live-v2")

        display = RecordingDisplay(64, 64)
        client = Client(display)
        url = build_ws_url(srv["ws_url"], token)
        assert "protocol=2" in url

        ws = _start_wsapp(url, client)
        try:
            assert _wait(lambda: client._protocol == 2), "client never negotiated v2"
            assert {"brightness", "spectrum", "hold"} <= client._capabilities
            assert _wait(lambda: display.frames > 0), "no frame rendered to display"
            # Brightness defaults to 100 (disabled); the first v2 frame carries it.
            assert _wait(lambda: display.brightness), "brightness hint never applied"
            assert display.brightness[0] == 100
        finally:
            ws.close()
            client.close()
    finally:
        if srv:
            stop_server(srv["proc"])


@pytest.mark.integration
def test_client_v1_fallback_has_no_v2_features(tmp_path):
    """A connect without ?protocol=2 gets v1: no welcome, no brightness."""
    try:
        import websocket  # noqa: F401
    except ImportError:
        pytest.skip("websocket-client not installed")

    srv = None
    try:
        srv = start_server(tmp_path / "srv")
        seed_settings(srv["url"], width=64, height=64, timeout_val=1)
        token = _device(srv, "live-v1")

        display = RecordingDisplay(64, 64)
        client = Client(display)
        # Deliberately bypass build_ws_url's protocol=2 opt-in.
        url = "%s/ws/device/%s" % (srv["ws_url"], token)

        ws = _start_wsapp(url, client)
        try:
            assert _wait(lambda: display.frames > 0), "v1 client rendered no frame"
            assert client._protocol == 1
            assert client._capabilities == set()
            assert display.brightness == []
        finally:
            ws.close()
            client.close()
    finally:
        if srv:
            stop_server(srv["proc"])


@pytest.mark.integration
def test_cli_end_to_end_writes_frames(tmp_path):
    """`python -m ledit_device` renders server frames via FileDisplay."""
    srv = None
    proc = None
    try:
        srv = start_server(tmp_path / "srv")
        seed_settings(srv["url"], width=64, height=64, timeout_val=1)
        token = _device(srv, "cli-e2e")

        outdir = tmp_path / "cli-frames"
        env = os.environ.copy()
        env.update(
            {
                "LEDIT_SERVER": srv["ws_url"],
                "LEDIT_TOKEN": token,
                "LEDIT_PREVIEW_DIR": str(outdir),
                "LEDIT_COLS": "64",
                "LEDIT_ROWS": "64",
            }
        )
        proc = subprocess.Popen(
            [sys.executable, "-m", "ledit_device"],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            cwd=str(Path(__file__).resolve().parents[3]),
        )

        deadline = time.time() + 20
        frames = []
        while time.time() < deadline:
            if proc.poll() is not None:
                out = proc.stdout.read().decode(errors="replace") if proc.stdout else ""
                pytest.fail(f"CLI exited early ({proc.returncode}): {out[:1500]}")
            frames = list(outdir.glob("*.png")) if outdir.exists() else []
            if frames:
                break
            time.sleep(0.25)
        assert frames, "CLI wrote no frame within 20s"
    finally:
        if proc is not None and proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
        if srv:
            stop_server(srv["proc"])
