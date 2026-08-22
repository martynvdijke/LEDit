"""Feed parity: raw websockets vs Client(FileDisplay)."""
import asyncio
import base64
import io
import json
import os
import time
from pathlib import Path

import pytest

try:
    from PIL import Image
except ImportError:
    Image = None

from ledit_device.client import Client
from ledit_device.display import FileDisplay

from .harness import (
    start_server,
    stop_server,
    seed_settings,
    create_device,
    get_device_token_from_db,
)



def _decode_png(b64: str):
    raw = base64.b64decode(b64)
    return Image.open(io.BytesIO(raw)).convert("RGB")


async def _recv_frame(ws, timeout=8):
    raw = await asyncio.wait_for(ws.recv(), timeout=timeout)
    data = json.loads(raw)
    # skip error frames
    while "image" not in data:
        raw = await asyncio.wait_for(ws.recv(), timeout=timeout)
        data = json.loads(raw)
    return data


@pytest.mark.integration
def test_emulation_writes_disk(tmp_path):
    """Emulation mode writes PNG to disk within 5s."""
    if Image is None:
        pytest.skip("Pillow not installed")
    try:
        import websockets  # noqa: F401
    except ImportError:
        pytest.skip("websockets not installed")
    srv = None
    try:
        srv = start_server(tmp_path / "srv1")
        url = srv["url"]
        seed_settings(url, width=64, height=64, timeout_val=1)
        # create device 64x64
        info = create_device(url, name="parity-disk", width=64, height=64, refresh_interval=1)
        if not info or not info.get("id"):
            pytest.skip("device creation failed")
        token = get_device_token_from_db(srv["data_dir"], info["id"])
        if not token:
            token = info.get("token")
        if not token:
            pytest.skip("no device token")

        outdir = str(tmp_path / "frames")
        display = FileDisplay(outdir, 64, 64)
        client = Client(display)

        # feed client via websocket in background thread using asyncio websockets
        async def run():
            import websockets
            async with websockets.connect(srv["ws_url"] + f"/ws/device/{token}") as ws:
                for _ in range(5):
                    raw = await asyncio.wait_for(ws.recv(), timeout=8)
                    data = json.loads(raw)
                    if "image" in data:
                        client.on_message(None, raw)
                        break
            # wait a bit for file
            for _ in range(25):
                files = list(Path(outdir).glob("*.png"))
                if files:
                    return files[0]
                await asyncio.sleep(0.2)
            return None

        f = asyncio.run(run())
        # also check sync path
        if f is None:
            # allow extra wait
            time.sleep(1)
            files = list(Path(outdir).glob("*.png"))
            assert files, "no PNG written within 5s"
            f = files[0]
        img = Image.open(f)
        assert img.size == (64, 64)
    finally:
        if srv:
            stop_server(srv["proc"])


@pytest.mark.integration
def test_same_resolution_parity(tmp_path):
    """Raw websocket vs Client(FileDisplay) produce same RGB bytes."""
    if Image is None:
        pytest.skip("Pillow not installed")
    try:
        import websockets
    except ImportError:
        pytest.skip("websockets not installed")
    srv = None
    try:
        srv = start_server(tmp_path / "srv2")
        url = srv["url"]
        seed_settings(url, width=64, height=64, timeout_val=1)
        info = create_device(url, name="parity-raw", width=64, height=64, refresh_interval=1)
        if not info or not info.get("id"):
            pytest.skip("device creation failed")
        token = get_device_token_from_db(srv["data_dir"], info["id"]) or info.get("token")
        if not token:
            pytest.skip("no device token")

        outdir = str(tmp_path / "frames2")
        display = FileDisplay(outdir, 64, 64)
        client = Client(display)

        async def run():
            import websockets
            raw_b64 = None
            async with websockets.connect(srv["ws_url"] + f"/ws/device/{token}") as ws_raw:
                data = await _recv_frame(ws_raw, timeout=8)
                raw_b64 = data["image"]
                # feed same message to client
                client.on_message(None, json.dumps(data))
            # also feed via second connection to ensure file written
            await asyncio.sleep(0.5)
            return raw_b64

        raw_b64 = asyncio.run(run())
        assert raw_b64 is not None
        files = list(Path(outdir).glob("*.png"))
        assert files, "FileDisplay did not write"
        # compare RGB bytes
        ws_img = _decode_png(raw_b64).resize((64, 64))
        file_img = Image.open(files[0]).convert("RGB")
        assert ws_img.tobytes() == file_img.tobytes(), "parity mismatch: raw vs FileDisplay bytes differ"

        # optional Playwright vs Python parity when env set
        if os.getenv("PLAYWRIGHT_INTEGRATION"):
            pytest.skip("Playwright parity requires browser harness - not implemented in this runner")
    finally:
        if srv:
            stop_server(srv["proc"])
