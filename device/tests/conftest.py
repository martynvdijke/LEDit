"""Shared pytest fixtures for ledit_device tests."""

import os
import sys
from unittest import mock

# Mock rgbmatrix at import time so hardware is never required.
if "rgbmatrix" not in sys.modules:
    rgbmatrix_mock = mock.MagicMock()
    # Provide RGBMatrix and RGBMatrixOptions as mock classes
    sys.modules["rgbmatrix"] = rgbmatrix_mock

import pytest

from ledit_device import telemetry as telemetry_module
from ledit_device.display import FileDisplay


_OTEL_KEYS = (
    "OTEL_EXPORTER_OTLP_ENDPOINT",
    "OTEL_EXPORTER_OTLP_PROTOCOL",
    "OTEL_SERVICE_NAME",
    "OTEL_RESOURCE_ATTRIBUTES",
    "OTEL_TRACES_SAMPLER",
    "OTEL_TRACES_SAMPLER_ARG",
)


@pytest.fixture(autouse=True)
def no_otel(monkeypatch):
    """Clean OTel env and reset telemetry singleton for each test."""
    for k in _OTEL_KEYS:
        monkeypatch.delenv(k, raising=False)
    # Reset global telemetry to disabled pristine instance
    orig = telemetry_module.get_telemetry()
    # Install disabled instance
    clean = telemetry_module.Telemetry()
    telemetry_module.set_telemetry(clean)
    try:
        yield
    finally:
        try:
            clean.shutdown()
        except Exception:
            pass
        # shutdown any telemetry that test may have installed
        try:
            cur = telemetry_module.get_telemetry()
            if cur is not clean:
                cur.shutdown()
        except Exception:
            pass
        telemetry_module.set_telemetry(telemetry_module.Telemetry())
        for k in _OTEL_KEYS:
            monkeypatch.delenv(k, raising=False)


@pytest.fixture
def file_display(tmp_path):
    return FileDisplay(str(tmp_path), 64, 64)


@pytest.fixture
def client(file_display):
    from ledit_device.client import Client

    return Client(file_display)
