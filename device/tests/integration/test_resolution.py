"""Per-device resolution: PNG dimensions match declarations."""
import asyncio
import base64
import io
import json
from pathlib import Path

import pytest

try:
    from PIL import Image
except ImportError:
    Image = None

from ledit_device.client import Client
from ledit_device.display import FileDisplay
from .harness import start_server, stop_server, seed_settings, create_device, get_device_token_from_db



def _size_of_b64(b64):
    raw = base64.b64decode(b64)
    img = Image.open(io.BytesIO(raw))
    return img.size


async def _one_frame(ws_url, timeout=8):
    import websockets
    async with websockets.connect(ws_url) as ws:
        for _ in range(5):
            raw = await asyncio.wait_for(ws.recv(), timeout=timeout)
            data = json.loads(raw)
            if "image" in data:
                return data
    raise AssertionError("no image frame")


@pytest.mark.integration
def test_resolution_raw_and_file_display(tmp_path):
    if Image is None:
        pytest.skip("Pillow not installed")
    try:
        import websockets
    except ImportError:
        pytest.skip("websockets not installed")
    srv = None
    try:
        srv = start_server(tmp_path / "srv")
        url = srv["url"]
        ws_url = srv["ws_url"]
        seed_settings(url, width=64, height=64, timeout_val=1)

        d64 = create_device(url, name="D64", width=64, height=64, refresh_interval=1)
        dwide = create_device(url, name="DWide", width=128, height=32, refresh_interval=1)
        if not d64 or not dwide:
            pytest.skip("device creation failed")
        tok64 = get_device_token_from_db(srv["data_dir"], d64["id"]) or d64.get("token")
        tokWide = get_device_token_from_db(srv["data_dir"], dwide["id"]) or dwide.get("token")
        if not tok64 or not tokWide:
            pytest.skip("missing tokens")

        async def check_raw():
            data64 = await _one_frame(f"{ws_url}/ws/device/{tok64}")
            assert _size_of_b64(data64["image"]) == (64, 64), f"D64 got {_size_of_b64(data64['image'])}"
            dataWide = await _one_frame(f"{ws_url}/ws/device/{tokWide}")
            assert _size_of_b64(dataWide["image"]) == (128, 32), f"DWide got {_size_of_b64(dataWide['image'])}"
            return data64, dataWide

        asyncio.run(check_raw())

        # Resolution parity across consumers: raw + FileDisplay both 128x32 for DWide
        outdir = str(tmp_path / "frames_wide")
        display = FileDisplay(outdir, 128, 32)
        client = Client(display)

        async def check_file_display():
            import websockets
            async with websockets.connect(f"{ws_url}/ws/device/{tokWide}") as ws:
                raw = await asyncio.wait_for(ws.recv(), timeout=8)
                data = json.loads(raw)
                # ensure image
                while "image" not in data:
                    raw = await asyncio.wait_for(ws.recv(), timeout=8)
                    data = json.loads(raw)
                client.on_message(None, json.dumps(data))
                # wait for file
                for _ in range(20):
                    files = list(Path(outdir).glob("*.png"))
                    if files:
                        img = Image.open(files[0])
                        assert img.size == (128, 32)
                        # also decode raw was 128x32 already asserted
                        return
                    await asyncio.sleep(0.2)
                assert False, "FileDisplay did not write 128x32"

        asyncio.run(check_file_display())

    finally:
        if srv:
            stop_server(srv["proc"])
