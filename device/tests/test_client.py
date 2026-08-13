"""Unit tests for the ledit_device package (no hardware required)."""

import base64
import io
import json
import os
import shutil
import tempfile
import unittest

from PIL import Image

from ledit_device import Client
from ledit_device import config
from ledit_device.display import FileDisplay


def tiny_png_b64(width=8, height=8, color=(255, 0, 0)):
    """Return a base64-encoded PNG of the given size."""
    img = Image.new("RGB", (width, height), color)
    buf = io.BytesIO()
    img.save(buf, format="PNG")
    return base64.b64encode(buf.getvalue()).decode("ascii")


class TestConfig(unittest.TestCase):
    def test_env_int_valid(self):
        os.environ["LEDIT_TEST_NUM"] = "42"
        self.assertEqual(config.env_int("LEDIT_TEST_NUM", 0), 42)
        del os.environ["LEDIT_TEST_NUM"]

    def test_env_int_invalid_falls_back(self):
        os.environ["LEDIT_TEST_NUM"] = "notanumber"
        self.assertEqual(config.env_int("LEDIT_TEST_NUM", 7), 7)
        del os.environ["LEDIT_TEST_NUM"]

    def test_env_int_missing_falls_back(self):
        os.environ.pop("LEDIT_TEST_NUM", None)
        self.assertEqual(config.env_int("LEDIT_TEST_NUM", 9), 9)

    def test_server_url_default(self):
        os.environ.pop("LEDIT_SERVER", None)
        self.assertEqual(config.server_url(), "ws://localhost:8080")

    def test_server_url_strips_trailing_slash(self):
        os.environ["LEDIT_SERVER"] = "ws://ledit.local:8080/"
        self.assertEqual(config.server_url(), "ws://ledit.local:8080")
        del os.environ["LEDIT_SERVER"]


class TestClient(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.display = FileDisplay(self.tmp, 64, 64)
        self.client = Client(self.display)

    def tearDown(self):
        shutil.rmtree(self.tmp, ignore_errors=True)

    def frames(self):
        return sorted(f for f in os.listdir(self.tmp) if f.endswith(".png"))

    def test_render_image_writes_frame(self):
        self.client.render_image(tiny_png_b64())
        self.assertEqual(len(self.frames()), 1)

    def test_render_text_writes_frame(self):
        self.client.render_text("Title", "Body")
        self.assertEqual(len(self.frames()), 1)

    def test_on_message_image(self):
        msg = json.dumps({"format": "PNG", "image": tiny_png_b64(), "source": "System Stats"})
        self.client.on_message(None, msg)
        self.assertEqual(len(self.frames()), 1)

    def test_on_message_notification(self):
        msg = json.dumps({"format": "PNG", "source": "NOTIFICATION", "title": "Hi", "message": "There"})
        self.client.on_message(None, msg)
        self.assertEqual(len(self.frames()), 1)

    def test_on_message_invalid_json(self):
        self.client.on_message(None, "not json {")
        self.assertEqual(len(self.frames()), 0)


if __name__ == "__main__":
    unittest.main()
