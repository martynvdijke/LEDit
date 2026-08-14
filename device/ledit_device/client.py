"""WebSocket client: pulls frames from the server and renders them."""

import base64
import io
import json

from PIL import Image, ImageDraw

from .config import log
from .telemetry import get_telemetry

# Pillow >=10 moved LANCZOS into Image.Resampling; keep compatibility with both.
try:
    RESAMPLE = Image.Resampling.LANCZOS
except AttributeError:  # pragma: no cover
    RESAMPLE = Image.LANCZOS


class Client:
    def __init__(self, display):
        self.display = display
        telemetry = get_telemetry()
        self._tracer = telemetry.get_tracer()
        meter = telemetry.get_meter()
        self._frames_rendered = meter.create_counter(
            "device.frames_rendered_total",
            unit="{frame}",
            description="Total number of frames rendered to the display",
        )
        self._connection_errors = meter.create_counter(
            "device.connection_errors_total",
            unit="{error}",
            description="Total number of WebSocket connection errors",
        )
        self._reconnects = meter.create_counter(
            "device.reconnects_total",
            unit="{reconnect}",
            description="Total number of WebSocket reconnects",
        )

    def render_image(self, b64):
        raw = base64.b64decode(b64)
        img = Image.open(io.BytesIO(raw)).convert("RGB")
        img = img.resize((self.display.width, self.display.height), RESAMPLE)
        self.display.show(img)
        self._frames_rendered.add(1, {"frame.type": "image"})

    def render_text(self, title, message):
        from .display import _truetype_font

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
        self._frames_rendered.add(1, {"frame.type": "text"})

    def on_message(self, _ws, message):
        with self._tracer.start_as_current_span("device.message.received"):
            try:
                data = json.loads(message)
            except (ValueError, TypeError):
                log("warning", "received invalid JSON frame")
                return
            if "image" in data:
                with self._tracer.start_as_current_span("device.render.image"):
                    self.render_image(data["image"])
            else:
                with self._tracer.start_as_current_span("device.render.text"):
                    self.render_text(data.get("title", ""), data.get("message", ""))

    def on_error(self, _ws, error):
        self._connection_errors.add(1)
        with self._tracer.start_as_current_span("device.connection.error") as span:
            span.set_attribute("error.type", str(error))
            span.record_exception(Exception(error))
            span.set_status("ERROR", str(error))
        log("error", str(error))

    def on_close(self, _ws, *_args):
        log("info", "connection closed")

    def on_reconnect(self):
        self._reconnects.add(1)
