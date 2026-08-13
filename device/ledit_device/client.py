"""WebSocket client: pulls frames from the server and renders them."""

import base64
import io
import json

from PIL import Image, ImageDraw

from .config import log

# Pillow >=10 moved LANCZOS into Image.Resampling; keep compatibility with both.
try:
    RESAMPLE = Image.Resampling.LANCZOS
except AttributeError:  # pragma: no cover
    RESAMPLE = Image.LANCZOS


class Client:
    def __init__(self, display):
        self.display = display

    def render_image(self, b64):
        raw = base64.b64decode(b64)
        img = Image.open(io.BytesIO(raw)).convert("RGB")
        img = img.resize((self.display.width, self.display.height), RESAMPLE)
        self.display.show(img)

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
