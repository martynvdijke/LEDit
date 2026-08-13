#!/usr/bin/env python3
"""LEDit device client.

A small, single-file client that streams frames from a LEDit server over
WebSocket and renders them onto an RGB LED matrix (HUB75 panels driven by the
official ``rpi-rgb-led-matrix`` library).

The device connects OUT to the server, so no inbound port or static IP is
required. Each device authenticates with its own token, shown on the admin
"Devices" page.

Configuration is read from environment variables:

    LEDIT_SERVER             ws:// URL of the server (default ws://localhost:8080)
    LEDIT_TOKEN              device token (required)
    LEDIT_COLS               panel width  (default 64)
    LEDIT_ROWS               panel height (default 64)
    LEDIT_CHAIN              chained panels (default 1)
    LEDIT_PARALLEL           parallel chains (default 1)
    LEDIT_HARDWARE_MAPPING   rpi-rgb-led-matrix mapping (default "regular")
    LEDIT_BRIGHTNESS         0-100 (default 80)
    LEDIT_GPIO_SLOWDOWN      set >1 on fast boards/Pi4 (default 1)
    LEDIT_PREVIEW_DIR        if set, save frames as PNGs here instead of driving
                             hardware (useful for testing without a panel)

Usage:

    LEDIT_SERVER=ws://ledit.local:8080 LEDIT_TOKEN=abc123 python3 ledit_client.py
"""

import base64
import io
import json
import os
import sys
import time

try:
    from PIL import Image, ImageDraw, ImageFont
except ImportError:  # pragma: no cover
    sys.stderr.write("Pillow is required: pip install pillow\n")
    raise

try:
    import websocket  # websocket-client
except ImportError:  # pragma: no cover
    sys.stderr.write("websocket-client is required: pip install websocket-client\n")
    raise

# Pillow >=10 moved LANCZOS into Image.Resampling; keep compatibility with both.
try:
    RESAMPLE = Image.Resampling.LANCZOS
except AttributeError:  # pragma: no cover
    RESAMPLE = Image.LANCZOS


def log(level, msg):
    sys.stderr.write("[%s] %s\n" % (time.strftime("%H:%M:%S"), msg))


def env_int(name, default):
    try:
        return int(os.getenv(name, default))
    except (TypeError, ValueError):
        return default


def _truetype_font(size):
    candidates = [
        "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    ]
    for path in candidates:
        if os.path.exists(path):
            return ImageFont.truetype(path, size)
    return ImageFont.load_default()


class Display:
    """Abstraction over the output surface (hardware matrix or file preview)."""

    @property
    def width(self):
        raise NotImplementedError

    @property
    def height(self):
        raise NotImplementedError

    def show(self, image):
        raise NotImplementedError


class MatrixDisplay(Display):
    def __init__(self):
        from rgbmatrix import RGBMatrix, RGBMatrixOptions  # deferred import

        options = RGBMatrixOptions()
        options.rows = env_int("LEDIT_ROWS", 64)
        options.cols = env_int("LEDIT_COLS", 64)
        options.chain_length = env_int("LEDIT_CHAIN", 1)
        options.parallel = env_int("LEDIT_PARALLEL", 1)
        options.hardware_mapping = os.getenv("LEDIT_HARDWARE_MAPPING", "regular")
        options.brightness = env_int("LEDIT_BRIGHTNESS", 80)
        options.gpio_slowdown = env_int("LEDIT_GPIO_SLOWDOWN", 1)
        options.pwm_bits = 11
        self.matrix = RGBMatrix(options=options)

    @property
    def width(self):
        return self.matrix.width

    @property
    def height(self):
        return self.matrix.height

    def show(self, image):
        canvas = self.matrix.CreateFrameCanvas()
        canvas.SetImage(image)
        self.matrix.SwapOnVSync(canvas)


class FileDisplay(Display):
    """Writes each frame as a PNG file; for testing without hardware."""

    def __init__(self, outdir, width, height):
        os.makedirs(outdir, exist_ok=True)
        self.outdir = outdir
        self._width = width
        self._height = height
        self.counter = 0

    @property
    def width(self):
        return self._width

    @property
    def height(self):
        return self._height

    def show(self, image):
        self.counter += 1
        path = os.path.join(self.outdir, "frame_%06d.png" % self.counter)
        image.save(path)
        log("info", "saved frame %s" % path)


def make_display():
    preview = os.getenv("LEDIT_PREVIEW_DIR")
    if preview:
        return FileDisplay(preview, env_int("LEDIT_COLS", 64), env_int("LEDIT_ROWS", 64))
    try:
        return MatrixDisplay()
    except ImportError:
        log("warning", "rpi-rgb-led-matrix not installed; falling back to preview mode")
        return FileDisplay("/tmp/ledit_frames", env_int("LEDIT_COLS", 64), env_int("LEDIT_ROWS", 64))


class Client:
    def __init__(self, display):
        self.display = display

    def render_image(self, b64):
        raw = base64.b64decode(b64)
        img = Image.open(io.BytesIO(raw)).convert("RGB")
        img = img.resize((self.display.width, self.display.height), RESAMPLE)
        self.display.show(img)

    def render_text(self, title, message):
        img = Image.new("RGB", (self.display.width, self.display.height), (0, 0, 0))
        draw = ImageDraw.Draw(img)
        size = max(8, min(self.display.width, self.display.height) // 8)
        font = _truetype_font(size)
        lines = []
        if title:
            lines.append(title)
        if message:
            lines.extend(message.splitlines() or [message])
        y = 2
        for line in lines:
            if y > self.display.height:
                break
            draw.text((2, y), line, fill=(255, 255, 255), font=font)
            y += size + 2
        self.display.show(img)

    def on_message(self, _ws, message):
        try:
            data = json.loads(message)
        except (ValueError, TypeError):
            log("warning", "received invalid JSON frame")
            return
        if "image" in data:
            self.render_image(data["image"])
        else:
            self.render_text(data.get("title", ""), data.get("message", ""))

    def on_error(self, _ws, error):
        log("error", str(error))

    def on_close(self, _ws, *_args):
        log("info", "connection closed")


def main():
    token = os.getenv("LEDIT_TOKEN", "").strip()
    if not token:
        sys.stderr.write("LEDIT_TOKEN is required (copy it from the admin Devices page)\n")
        sys.exit(1)

    server = os.getenv("LEDIT_SERVER", "ws://localhost:8080").rstrip("/")
    url = "%s/ws/device/%s" % (server, token)

    display = make_display()
    client = Client(display)
    log("info", "connecting to %s (matrix %dx%d)" % (url, display.width, display.height))

    ws = websocket.WebSocketApp(
        url,
        on_message=client.on_message,
        on_error=client.on_error,
        on_close=client.on_close,
    )
    # run_forever with reconnect=True keeps the device online across drops.
    ws.run_forever(ping_interval=30, ping_timeout=10, reconnect=5)


if __name__ == "__main__":
    main()
