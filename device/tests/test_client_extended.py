"""Extended client tests covering rendering, spans, counters, disabled telemetry."""

import base64
import io
import json
from unittest import mock

from PIL import Image

from ledit_device.display import FileDisplay


def tiny_png_b64(width=8, height=8, color=(255, 0, 0)):
    img = Image.new("RGB", (width, height), color)
    buf = io.BytesIO()
    img.save(buf, format="PNG")
    return base64.b64encode(buf.getvalue()).decode("ascii")


# ----- Fake OTel helpers -----

class FakeSpan:
    def __init__(self, name, recorder):
        self.name = name
        self.recorder = recorder
        self.attributes = {}
        self.status = None
        self.exception = None

    def __enter__(self):
        self.recorder.append(self)
        return self

    def __exit__(self, *args):
        return False

    def set_attribute(self, k, v):
        self.attributes[k] = v

    def set_status(self, status, desc=None):
        self.status = (status, desc)

    def record_exception(self, exc):
        self.exception = exc


class FakeTracer:
    def __init__(self):
        self.spans = []

    def start_as_current_span(self, name):
        return FakeSpan(name, self.spans)


class FakeCounter:
    def __init__(self):
        self.calls = []

    def add(self, amount, attrs=None):
        self.calls.append((amount, attrs or {}))


class FakeMeter:
    def __init__(self):
        self.counters = {}

    def create_counter(self, name, **kwargs):
        c = FakeCounter()
        self.counters[name] = c
        return c


class FakeTelemetry:
    def __init__(self):
        self.tracer = FakeTracer()
        self.meter = FakeMeter()
        self.enabled = True

    def get_tracer(self, name=None):
        return self.tracer

    def get_meter(self, name=None):
        return self.meter


def _make_client_with_fake_telemetry(tmp_path, width=64, height=64):
    from ledit_device import telemetry as tel_mod
    from ledit_device import client as client_mod
    from ledit_device.client import Client

    fake = FakeTelemetry()
    # Need to patch both the telemetry module and the client module's imported reference
    p1 = mock.patch.object(tel_mod, "get_telemetry", return_value=fake)
    p2 = mock.patch.object(client_mod, "get_telemetry", return_value=fake)
    p1.start()
    p2.start()
    display = FileDisplay(str(tmp_path), width, height)
    client = Client(display)
    # Attach fake for assertions and ensure patch is stopped later via client._fake_patch
    client._fake_telemetry = fake  # type: ignore
    client._fake_patch = (p1, p2)  # type: ignore
    return client


def _stop_fake(client):
    if hasattr(client, "_fake_patch"):
        p1, p2 = client._fake_patch  # type: ignore
        p1.stop()
        p2.stop()


def test_render_image_resize(tmp_path):
    client = _make_client_with_fake_telemetry(tmp_path, 64, 64)
    try:
        client.render_image(tiny_png_b64(8, 8))
        files = list(tmp_path.glob("*.png"))
        assert len(files) == 1
        img = Image.open(files[0])
        assert img.size == (64, 64)
        counter = client._fake_telemetry.meter.counters["device.frames_rendered_total"]
        assert any(attrs.get("frame.type") == "image" for _, attrs in counter.calls)
    finally:
        _stop_fake(client)


def test_render_text_title_wrapping(tmp_path):
    client = _make_client_with_fake_telemetry(tmp_path, 64, 32)
    try:
        client.render_text("Title", "Line1\nLine2\nLine3")
        files = list(tmp_path.glob("*.png"))
        assert len(files) == 1
        img = Image.open(files[0])
        assert img.size == (64, 32)
        counter = client._fake_telemetry.meter.counters["device.frames_rendered_total"]
        assert any(attrs.get("frame.type") == "text" for _, attrs in counter.calls)
    finally:
        _stop_fake(client)


def test_on_message_image_branch_with_spans(tmp_path):
    client = _make_client_with_fake_telemetry(tmp_path)
    try:
        msg = json.dumps({"format": "PNG", "image": tiny_png_b64(), "source": "X"})
        client.on_message(None, msg)
        assert len(list(tmp_path.glob("*.png"))) == 1
        span_names = [s.name for s in client._fake_telemetry.tracer.spans]
        assert "device.message.received" in span_names
        assert "device.render.image" in span_names
    finally:
        _stop_fake(client)


def test_on_message_text_branch_with_spans(tmp_path):
    client = _make_client_with_fake_telemetry(tmp_path)
    try:
        msg = json.dumps({"title": "Hi", "message": "There"})
        client.on_message(None, msg)
        assert len(list(tmp_path.glob("*.png"))) == 1
        span_names = [s.name for s in client._fake_telemetry.tracer.spans]
        assert "device.message.received" in span_names
        assert "device.render.text" in span_names
    finally:
        _stop_fake(client)


def test_on_message_invalid_json(tmp_path, caplog):
    client = _make_client_with_fake_telemetry(tmp_path)
    try:
        client.on_message(None, "not json {")
        assert len(list(tmp_path.glob("*.png"))) == 0
        # spans: only outer received, no render span
        span_names = [s.name for s in client._fake_telemetry.tracer.spans]
        assert "device.message.received" in span_names
        assert "device.render.image" not in span_names
        assert "device.render.text" not in span_names
    finally:
        _stop_fake(client)


def test_on_error_increments_and_spans(tmp_path):
    client = _make_client_with_fake_telemetry(tmp_path)
    try:
        client.on_error(None, "boom")
        counter = client._fake_telemetry.meter.counters["device.connection_errors_total"]
        assert len(counter.calls) == 1
        assert counter.calls[0][0] == 1
        spans = [s for s in client._fake_telemetry.tracer.spans if s.name == "device.connection.error"]
        assert len(spans) == 1
        assert spans[0].attributes.get("error.type") == "boom"
        assert spans[0].status is not None
        assert spans[0].exception is not None
    finally:
        _stop_fake(client)


def test_on_close_no_exception(tmp_path):
    client = _make_client_with_fake_telemetry(tmp_path)
    try:
        client.on_close(None)
        # no counter increment for close, just log
        assert len(list(tmp_path.glob("*.png"))) == 0
    finally:
        _stop_fake(client)


def test_on_reconnect_increments(tmp_path):
    client = _make_client_with_fake_telemetry(tmp_path)
    try:
        client.on_reconnect()
        counter = client._fake_telemetry.meter.counters["device.reconnects_total"]
        assert counter.calls == [(1, {})]
        client.on_reconnect()
        assert len(counter.calls) == 2
    finally:
        _stop_fake(client)


def test_disabled_telemetry_render_path(tmp_path):
    """When telemetry is disabled (noop tracer/meter), rendering still works."""
    from ledit_device import telemetry as tel_mod
    from ledit_device.client import Client
    from ledit_device.telemetry import Telemetry, set_telemetry

    disabled = Telemetry()  # enabled=False, provides noop via global fallback
    set_telemetry(disabled)
    try:
        display = FileDisplay(str(tmp_path), 32, 32)
        client = Client(display)
        # valid image
        client.on_message(None, json.dumps({"format": "PNG", "image": tiny_png_b64()}))
        # invalid json safe
        client.on_message(None, "not json {")
        assert len(list(tmp_path.glob("*.png"))) == 1
        # on_error/on_reconnect still work (noop counters but no crash)
        client.on_error(None, "boom")
        client.on_reconnect()
        assert len(list(tmp_path.glob("*.png"))) == 1
    finally:
        set_telemetry(Telemetry())
