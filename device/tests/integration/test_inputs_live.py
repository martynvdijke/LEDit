"""Live integration: device input events over the v2 protocol.

Requires the Go server binary; run with -m integration (see Taskfile test:integration).
"""

import asyncio
import json

import pytest

from .harness import (
    create_device,
    get_device_token_from_db,
    seed_settings,
    start_server,
    stop_server,
)

try:
    import websockets
except ImportError:  # pragma: no cover
    websockets = None

pytestmark = pytest.mark.integration


def _require_websockets():
    if websockets is None:
        pytest.skip("websockets package not installed")


async def _recv_until(ws, predicate, timeout=8.0):
    while True:
        raw = await asyncio.wait_for(ws.recv(), timeout=timeout)
        data = json.loads(raw)
        if predicate(data):
            return data


async def _run_input_flow(ws_url, token):
    async with websockets.connect(f"{ws_url}/ws/device/{token}?protocol=2") as ws:
        welcome = await _recv_until(ws, lambda d: d.get("type") == "welcome")
        assert welcome.get("protocol") == 2
        caps = welcome.get("capabilities", [])
        assert "inputs" in caps, caps

        await ws.send(json.dumps({"type": "input", "source": "nfc", "event": "tap", "value": "04a1b2c3"}))
        await ws.send(json.dumps({"type": "input", "source": "encoder", "event": "rotate", "value": 1}))
        await ws.send(json.dumps({"type": "input", "source": "button:next", "event": "press"}))

        # The stream must stay alive and keep rendering after accepted events.
        frame = await _recv_until(ws, lambda d: "image" in d)
        assert frame.get("image")


async def _run_v1_ignores_inputs(ws_url, token):
    async with websockets.connect(f"{ws_url}/ws/device/{token}") as ws:
        first = json.loads(await asyncio.wait_for(ws.recv(), timeout=8))
        # v1 connections never negotiate the welcome message.
        assert first.get("type") != "welcome"

        await ws.send(json.dumps({"type": "input", "source": "nfc", "event": "tap", "value": "04a1b2c3"}))
        frame = await _recv_until(ws, lambda d: "image" in d)
        assert frame.get("image")


def _boot(tmp_path, name):
    srv = start_server(tmp_path / "srv")
    seed_settings(srv["url"], 64, 64, timeout_val=1)
    create_device(srv["url"], name, 64, 64, 1)
    token = get_device_token_from_db(srv["data_dir"], 1)
    return srv, token


def test_input_events_accepted_over_v2(tmp_path):
    _require_websockets()
    srv, token = _boot(tmp_path, "InputLive")
    try:
        asyncio.run(_run_input_flow(srv["ws_url"], token))
    finally:
        stop_server(srv["proc"])


def test_v1_connections_ignore_input_events(tmp_path):
    _require_websockets()
    srv, token = _boot(tmp_path, "InputLiveV1")
    try:
        asyncio.run(_run_v1_ignores_inputs(srv["ws_url"], token))
    finally:
        stop_server(srv["proc"])
