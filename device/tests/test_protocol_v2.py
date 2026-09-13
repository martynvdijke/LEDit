"""Protocol v2 negotiation, brightness hints and spectrum tap tests (no hardware)."""

import base64
import io
import json
import time
from unittest import mock

from PIL import Image

from ledit_device import config
from ledit_device.client import Client, build_ws_url
from ledit_device.display import FileDisplay


def tiny_png_b64(width=8, height=8, color=(255, 0, 0)):
    img = Image.new("RGB", (width, height), color)
    buf = io.BytesIO()
    img.save(buf, format="PNG")
    return base64.b64encode(buf.getvalue()).decode("ascii")


def make_client(tmp_path, **kwargs):
    return Client(FileDisplay(str(tmp_path), 8, 8), **kwargs)


class FakeWS:
    def __init__(self):
        self.sent = []

    def send(self, msg):
        self.sent.append(json.loads(msg))


# -- negotiation -------------------------------------------------------------


def test_build_ws_url_has_protocol_param():
    assert build_ws_url("ws://host:8080/", "tok") == "ws://host:8080/ws/device/tok?protocol=2"


def test_welcome_enables_capabilities(tmp_path):
    c = make_client(tmp_path)
    c.on_message(None, json.dumps({
        "type": "welcome",
        "protocol": 2,
        "capabilities": ["brightness", "spectrum", "hold"],
    }))
    assert c._protocol == 2
    assert {"brightness", "spectrum", "hold"} <= c._capabilities
    c.close()


def test_missing_welcome_stays_v1(tmp_path):
    c = make_client(tmp_path)
    # A frame arriving with no prior welcome must not enable v2 behaviour.
    c.on_message(None, json.dumps({
        "format": "PNG", "image": tiny_png_b64(), "source": "Clock", "brightness": 30,
    }))
    assert c._protocol == 1
    assert c._capabilities == set()
    c.close()


def test_welcome_v1_and_malformed_stay_v1(tmp_path):
    c = make_client(tmp_path)
    c.on_message(None, json.dumps({"type": "welcome", "protocol": 1, "capabilities": ["brightness"]}))
    assert c._protocol == 1
    c.on_message(None, json.dumps({"type": "welcome", "protocol": 2}))  # no capabilities list
    assert c._protocol == 1
    c.close()


# -- brightness hint ---------------------------------------------------------


def test_brightness_applied_when_capable(tmp_path):
    display = mock.MagicMock(width=8, height=8)
    c = Client(display)
    c._handle_welcome({"protocol": 2, "capabilities": ["brightness"]})
    c.on_message(None, json.dumps({
        "format": "PNG", "image": tiny_png_b64(), "source": "Clock", "brightness": 42,
    }))
    display.set_brightness.assert_called_once_with(42)
    c.close()


def test_brightness_ignored_without_capability(tmp_path):
    display = mock.MagicMock(width=8, height=8)
    c = Client(display)
    c.on_message(None, json.dumps({
        "format": "PNG", "image": tiny_png_b64(), "source": "Clock", "brightness": 42,
    }))
    display.set_brightness.assert_not_called()
    c.close()


def test_brightness_ignored_out_of_range_and_non_int(tmp_path):
    display = mock.MagicMock(width=8, height=8)
    c = Client(display)
    c._handle_welcome({"protocol": 2, "capabilities": ["brightness"]})
    for bad in (150, -10, "30", 30.5, True):
        c.on_message(None, json.dumps({
            "format": "PNG", "image": tiny_png_b64(), "source": "Clock", "brightness": bad,
        }))
    display.set_brightness.assert_not_called()
    c.close()


# -- spectrum tap ------------------------------------------------------------


def test_spectrum_not_sent_when_not_opted_in(tmp_path, monkeypatch):
    monkeypatch.delenv("LEDIT_SPECTRUM", raising=False)
    ws = FakeWS()
    c = make_client(tmp_path, spectrum_capture=lambda: [7] * 16, spectrum_interval=0.01)
    c._handle_welcome({"protocol": 2, "capabilities": ["spectrum"]})
    c.on_message(ws, json.dumps({
        "format": "PNG", "image": tiny_png_b64(), "source": "audio:visualizer",
    }))
    time.sleep(0.15)
    assert ws.sent == []
    c.close()


def test_spectrum_not_sent_without_welcome(tmp_path, monkeypatch):
    monkeypatch.setenv("LEDIT_SPECTRUM", "1")
    ws = FakeWS()
    c = make_client(tmp_path, spectrum_capture=lambda: [7] * 16, spectrum_interval=0.01)
    c.on_message(ws, json.dumps({
        "format": "PNG", "image": tiny_png_b64(), "source": "audio:visualizer",
    }))
    time.sleep(0.15)
    assert ws.sent == []
    c.close()


def test_spectrum_sent_when_gated_and_opted_in(tmp_path, monkeypatch):
    monkeypatch.setenv("LEDIT_SPECTRUM", "1")
    ws = FakeWS()
    c = make_client(tmp_path, spectrum_capture=lambda: [7] * 16, spectrum_interval=0.01)
    c._handle_welcome({"protocol": 2, "capabilities": ["spectrum"]})
    c.on_message(ws, json.dumps({
        "format": "PNG", "image": tiny_png_b64(), "source": "audio:visualizer",
    }))
    deadline = time.time() + 1.0
    while time.time() < deadline and not ws.sent:
        time.sleep(0.01)
    c.close()
    assert ws.sent, "expected spectrum messages while visualizer active"
    assert ws.sent[0]["type"] == "spectrum"
    assert len(ws.sent[0]["bins"]) == 16


def test_spectrum_sent_for_builtin_display_name(tmp_path, monkeypatch):
    # The server's built-in visualizer renders under the display name rather
    # than the spec id; the gate must accept both.
    monkeypatch.setenv("LEDIT_SPECTRUM", "1")
    ws = FakeWS()
    c = make_client(tmp_path, spectrum_capture=lambda: [7] * 16, spectrum_interval=0.01)
    c._handle_welcome({"protocol": 2, "capabilities": ["spectrum"]})
    c.on_message(ws, json.dumps({
        "format": "PNG", "image": tiny_png_b64(), "source": "Audio Visualizer",
    }))
    deadline = time.time() + 1.0
    while time.time() < deadline and not ws.sent:
        time.sleep(0.01)
    c.close()
    assert ws.sent, "expected spectrum for built-in Audio Visualizer source"


def test_spectrum_stops_when_source_changes(tmp_path, monkeypatch):
    monkeypatch.setenv("LEDIT_SPECTRUM", "1")
    ws = FakeWS()
    c = make_client(tmp_path, spectrum_capture=lambda: [7] * 16, spectrum_interval=0.01)
    c._handle_welcome({"protocol": 2, "capabilities": ["spectrum"]})
    c.on_message(ws, json.dumps({
        "format": "PNG", "image": tiny_png_b64(), "source": "audio:visualizer",
    }))
    time.sleep(0.05)
    c.on_message(ws, json.dumps({
        "format": "PNG", "image": tiny_png_b64(), "source": "Clock",
    }))
    count = len(ws.sent)
    time.sleep(0.15)
    # At most one in-flight message may land after the source changes.
    assert len(ws.sent) <= count + 1
    c.close()


def test_mic_unavailable_returns_none_and_no_crash(tmp_path, monkeypatch):
    monkeypatch.setenv("LEDIT_SPECTRUM", "1")
    c = make_client(tmp_path, spectrum_interval=0.01)
    # Default capture path must fail gracefully without sounddevice.
    assert c._capture_bins() is None
    ws = FakeWS()
    c._handle_welcome({"protocol": 2, "capabilities": ["spectrum"]})
    c.on_message(ws, json.dumps({
        "format": "PNG", "image": tiny_png_b64(), "source": "audio:visualizer",
    }))
    time.sleep(0.1)
    assert ws.sent == []
    c.close()


def test_capture_exception_is_swallowed(tmp_path, monkeypatch):
    monkeypatch.setenv("LEDIT_SPECTRUM", "1")

    def boom():
        raise RuntimeError("mic died")

    c = make_client(tmp_path, spectrum_capture=boom, spectrum_interval=0.01)
    assert c._capture_bins() is None
    c.close()


# -- config helpers ----------------------------------------------------------


def test_env_bool(monkeypatch):
    monkeypatch.setenv("LEDIT_TEST_BOOL", "1")
    assert config.env_bool("LEDIT_TEST_BOOL") is True
    monkeypatch.setenv("LEDIT_TEST_BOOL", "TRUE")
    assert config.env_bool("LEDIT_TEST_BOOL") is True
    monkeypatch.setenv("LEDIT_TEST_BOOL", "no")
    assert config.env_bool("LEDIT_TEST_BOOL") is False
    monkeypatch.delenv("LEDIT_TEST_BOOL", raising=False)
    assert config.env_bool("LEDIT_TEST_BOOL", default=True) is True


def test_spectrum_enabled(monkeypatch):
    monkeypatch.setenv("LEDIT_SPECTRUM", "1")
    assert config.spectrum_enabled() is True
    monkeypatch.setenv("LEDIT_SPECTRUM", "0")
    assert config.spectrum_enabled() is False
    monkeypatch.delenv("LEDIT_SPECTRUM", raising=False)
    assert config.spectrum_enabled() is False
