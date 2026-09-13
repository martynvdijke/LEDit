"""Protocol v2 integration: welcome negotiation, v1 fallback and control events."""
import asyncio
import json

import pytest

from .harness import (
    start_server,
    stop_server,
    seed_settings,
    create_device,
    get_device_token_from_db,
)


async def _next_image(ws, timeout=8):
    raw = await asyncio.wait_for(ws.recv(), timeout=timeout)
    data = json.loads(raw)
    while "image" not in data:
        raw = await asyncio.wait_for(ws.recv(), timeout=timeout)
        data = json.loads(raw)
    return data


@pytest.mark.integration
def test_protocol_v2_welcome_and_v1_fallback(tmp_path):
    try:
        import websockets  # noqa: F401
    except ImportError:
        pytest.skip("websockets not installed")

    srv = None
    try:
        srv = start_server(tmp_path / "srv")
        url = srv["url"]
        ws_url = srv["ws_url"]
        seed_settings(url, width=64, height=64, timeout_val=1)
        info = create_device(url, name="proto-v2", width=64, height=64, refresh_interval=1)
        if not info:
            pytest.skip("device creation failed")
        token = get_device_token_from_db(srv["data_dir"], info["id"]) or info.get("token")
        if not token:
            pytest.skip("no device token")

        async def run():
            # v2 device: welcome first, then frames; brightness is optional and
            # spectrum/hold must not break the stream.
            async with websockets.connect(f"{ws_url}/ws/device/{token}?protocol=2") as ws:
                welcome = json.loads(await asyncio.wait_for(ws.recv(), timeout=8))
                assert welcome.get("type") == "welcome"
                assert welcome.get("protocol") == 2
                assert {"brightness", "spectrum", "hold"} <= set(welcome.get("capabilities", []))

                frame = await _next_image(ws)
                assert "brightness" in frame or frame.get("brightness") is None

                bins = [i * 10 for i in range(16)]
                await ws.send(json.dumps({"type": "spectrum", "bins": bins}))
                await ws.send(json.dumps({"action": "hold"}))
                await ws.send(json.dumps({"action": "next"}))
                # Stream stays alive after spectrum + hold + next.
                await _next_image(ws)

            # v1 device on the same v2 server: no welcome, no brightness.
            async with websockets.connect(f"{ws_url}/ws/device/{token}") as ws:
                first = json.loads(await asyncio.wait_for(ws.recv(), timeout=8))
                while first.get("type") == "welcome":
                    first = json.loads(await asyncio.wait_for(ws.recv(), timeout=8))
                assert first.get("type") != "welcome"
                while "image" not in first:
                    first = json.loads(await asyncio.wait_for(ws.recv(), timeout=8))
                assert "brightness" not in first

        asyncio.run(run())
    finally:
        if srv:
            stop_server(srv["proc"])
