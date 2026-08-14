"""Unit tests for the ledit_device telemetry module (no network required)."""

import os
import unittest
from unittest import mock

from ledit_device import telemetry
from ledit_device.telemetry import (
    DEFAULT_SERVICE_NAME,
    Telemetry,
    init_telemetry,
    set_telemetry,
    _service_name,
)


def _clean_otel_env():
    for key in (
        "OTEL_EXPORTER_OTLP_ENDPOINT",
        "OTEL_EXPORTER_OTLP_PROTOCOL",
        "OTEL_SERVICE_NAME",
        "OTEL_RESOURCE_ATTRIBUTES",
        "OTEL_TRACES_SAMPLER",
        "OTEL_TRACES_SAMPLER_ARG",
    ):
        os.environ.pop(key, None)


class TestTelemetry(unittest.TestCase):
    def setUp(self):
        _clean_otel_env()
        self._orig_default = telemetry.get_telemetry()

    def tearDown(self):
        _clean_otel_env()
        # Restore a pristine disabled instance so later tests stay isolated.
        set_telemetry(Telemetry())

    def test_disabled_by_default(self):
        t = Telemetry()
        self.assertFalse(t.is_enabled())
        self.assertIsNone(t.tracer_provider)
        self.assertIsNone(t.meter_provider)
        self.assertIsNone(t.logger_provider)

    def test_init_without_endpoint_returns_disabled(self):
        t = init_telemetry()
        self.assertFalse(t.is_enabled())
        t.shutdown()

    def test_init_with_endpoint_enables_grpc_default(self):
        os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"] = "localhost:4317"
        t = init_telemetry()
        self.assertTrue(t.is_enabled())
        self.assertEqual(t.protocol, "grpc")
        self.assertIsNotNone(t.get_tracer())
        self.assertIsNotNone(t.get_meter())
        t.shutdown()

    def test_init_with_http_protocol(self):
        os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"] = "http://localhost:4318"
        os.environ["OTEL_EXPORTER_OTLP_PROTOCOL"] = "http/protobuf"
        t = init_telemetry()
        self.assertTrue(t.is_enabled())
        self.assertEqual(t.protocol, "http/protobuf")
        t.shutdown()

    def test_shutdown_idempotent(self):
        t = Telemetry()
        t.shutdown()
        t.shutdown()
        self.assertFalse(t.is_enabled())

    def test_get_telemetry_returns_default(self):
        _clean_otel_env()
        init_telemetry()
        self.assertFalse(telemetry.get_telemetry().is_enabled())

    def test_service_name_default(self):
        self.assertEqual(_service_name(), DEFAULT_SERVICE_NAME)

    def test_service_name_from_env(self):
        os.environ["OTEL_SERVICE_NAME"] = "my-led-matrix"
        self.assertEqual(_service_name(), "my-led-matrix")

    def test_protocol_defaults_to_grpc(self):
        os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"] = "localhost:4317"
        os.environ.pop("OTEL_EXPORTER_OTLP_PROTOCOL", None)
        with mock.patch.object(telemetry, "set_telemetry"):
            t = init_telemetry()
        self.assertEqual(t.protocol, "grpc")
        t.shutdown()


class TestClientNoOp(unittest.TestCase):
    """Rendering with telemetry disabled must work and not panic."""

    def setUp(self):
        _clean_otel_env()
        init_telemetry()  # disabled

    def tearDown(self):
        _clean_otel_env()
        set_telemetry(Telemetry())

    def test_client_renders_with_telemetry_disabled(self):
        import base64
        import io
        import json
        import shutil
        import tempfile

        from PIL import Image

        from ledit_device import Client
        from ledit_device.display import FileDisplay

        tmp = tempfile.mkdtemp()
        try:
            display = FileDisplay(tmp, 8, 8)
            client = Client(display)

            img = Image.new("RGB", (8, 8), (255, 0, 0))
            buf = io.BytesIO()
            img.save(buf, format="PNG")
            b64 = base64.b64encode(buf.getvalue()).decode("ascii")

            client.on_message(None, json.dumps({"format": "PNG", "image": b64}))
            client.on_message(None, "not json {")
            client.on_error(None, "boom")
            client.on_reconnect()

            self.assertEqual(len(os.listdir(tmp)), 1)
        finally:
            shutil.rmtree(tmp, ignore_errors=True)
            telemetry.get_telemetry().shutdown()


if __name__ == "__main__":
    unittest.main()
