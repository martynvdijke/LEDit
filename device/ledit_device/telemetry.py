"""OpenTelemetry support for the LEDit device library.

Mirrors the Go server's ``logging/otel.go`` conventions: the whole pipeline
(traces, metrics, log export) is configured from standard ``OTEL_*``
environment variables and degrades to a no-op when
``OTEL_EXPORTER_OTLP_ENDPOINT`` is not set, so devices without a collector
run exactly as before.
"""

import logging
import os

from opentelemetry import metrics as otel_metrics
from opentelemetry import trace as otel_trace
from opentelemetry._logs import set_logger_provider as otel_set_logger_provider
from opentelemetry.exporter.otlp.proto.grpc._log_exporter import OTLPLogExporter
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.exporter.otlp.proto.http._log_exporter import (
    OTLPLogExporter as OTLPLogExporterHTTP,
)
from opentelemetry.exporter.otlp.proto.http.metric_exporter import (
    OTLPMetricExporter as OTLPMetricExporterHTTP,
)
from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
    OTLPSpanExporter as OTLPSpanExporterHTTP,
)
from opentelemetry.instrumentation.logging import LoggingInstrumentor
from opentelemetry.instrumentation.logging.handler import LoggingHandler
from opentelemetry.sdk._logs import (
    LoggerProvider,
    SynchronousMultiLogRecordProcessor,
)
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

DEFAULT_SERVICE_NAME = "ledit-device"
INSTRUMENTATION_NAME = "ledit-device"


class Telemetry:
    """Holds the OpenTelemetry providers (tracer, meter, logger)."""

    def __init__(self):
        self.tracer_provider = None
        self.meter_provider = None
        self.logger_provider = None
        self.enabled = False
        self.endpoint = ""
        self.protocol = "grpc"
        self._logging_handler = None

    def is_enabled(self):
        return self.enabled

    # -- provider accessors (all safe to call when disabled) ----------------

    def get_tracer(self, name=INSTRUMENTATION_NAME):
        provider = self.tracer_provider or otel_trace.get_tracer_provider()
        return provider.get_tracer(name)

    def get_meter(self, name=INSTRUMENTATION_NAME):
        provider = self.meter_provider or otel_metrics.get_meter_provider()
        return provider.get_meter(name)

    # -- pipeline construction ----------------------------------------------

    def init_providers(self):
        """Build trace/metric/log providers and wire log export."""
        resource = _build_resource()

        # -- Trace provider (sampler read from OTEL_TRACES_SAMPLER[_ARG]) --
        self.tracer_provider = TracerProvider(resource=resource)
        exporter = self._create_span_exporter()
        self.tracer_provider.add_span_processor(
            BatchSpanProcessor(exporter)
        )
        otel_trace.set_tracer_provider(self.tracer_provider)

        # -- Metric provider --
        self.meter_provider = MeterProvider(
            resource=resource,
            metric_readers=[
                PeriodicExportingMetricReader(self._create_metric_exporter())
            ],
        )
        otel_metrics.set_meter_provider(self.meter_provider)

        # -- Log provider + stderr/OTLP log bridge --
        multi_processor = SynchronousMultiLogRecordProcessor()
        multi_processor.add_log_record_processor(
            BatchLogRecordProcessor(self._create_log_exporter())
        )
        self.logger_provider = LoggerProvider(
            resource=resource,
            multi_log_record_processor=multi_processor,
        )
        otel_set_logger_provider(self.logger_provider)

        # Forward stdlib logging records to OTel with trace context.
        LoggingInstrumentor().instrument(inject_trace_context=True)
        self._logging_handler = LoggingHandler(
            level=logging.NOTSET, logger_provider=self.logger_provider
        )
        logging.getLogger().addHandler(self._logging_handler)
    # -- exporter factories (protocol: grpc default, http/protobuf) --------

    def _create_span_exporter(self):
        if self.protocol == "http/protobuf":
            return OTLPSpanExporterHTTP(endpoint=self.endpoint)
        return OTLPSpanExporter(endpoint=self.endpoint, insecure=True)

    def _create_metric_exporter(self):
        if self.protocol == "http/protobuf":
            return OTLPMetricExporterHTTP(endpoint=self.endpoint)
        return OTLPMetricExporter(endpoint=self.endpoint, insecure=True)

    def _create_log_exporter(self):
        if self.protocol == "http/protobuf":
            return OTLPLogExporterHTTP(endpoint=self.endpoint)
        return OTLPLogExporter(endpoint=self.endpoint, insecure=True)

    # -- shutdown -----------------------------------------------------------

    def shutdown(self):
        """Flush and shut down all providers. Safe to call multiple times."""
        for provider in (
            self.tracer_provider,
            self.meter_provider,
            self.logger_provider,
        ):
            if provider is not None:
                try:
                    provider.shutdown()
                except Exception:  # pragma: no cover - defensive
                    pass
        self.tracer_provider = None
        self.meter_provider = None
        self.logger_provider = None
        self.enabled = False
        if self._logging_handler is not None:
            try:
                logging.getLogger().removeHandler(self._logging_handler)
            except Exception:  # pragma: no cover - defensive
                pass
            self._logging_handler = None
        try:
            if LoggingInstrumentor().is_instrumented_by_opentelemetry():
                LoggingInstrumentor().uninstrument()
        except Exception:  # pragma: no cover - defensive
            pass


def init_telemetry():
    """Initialise the OTel pipeline from ``OTEL_*`` environment variables.

    Returns a disabled :class:`Telemetry` when ``OTEL_EXPORTER_OTLP_ENDPOINT``
    is unset (graceful degradation).
    """
    telemetry = Telemetry()

    endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "").strip()
    protocol = os.getenv("OTEL_EXPORTER_OTLP_PROTOCOL", "").strip() or "grpc"

    if not endpoint:
        logging.getLogger("ledit_device.telemetry").debug(
            "OTel telemetry disabled - no OTEL_EXPORTER_OTLP_ENDPOINT set"
        )
    else:
        telemetry.enabled = True
        telemetry.endpoint = endpoint
        telemetry.protocol = protocol
        telemetry.init_providers()

    set_telemetry(telemetry)
    return telemetry


def _service_name():
    return os.getenv("OTEL_SERVICE_NAME", "").strip() or DEFAULT_SERVICE_NAME


def _build_resource():
    # Resource.create merges OTEL_RESOURCE_ATTRIBUTES / OTEL_SERVICE_NAME
    # from the environment with the explicit service name.
    return Resource.create({"service.name": _service_name()})


# Module-level default instance. init_telemetry() replaces it; get_telemetry()
# returns whatever is currently active (disabled no-op until then), so
# modules can import get_telemetry() and stay testable without wiring.
_default_telemetry = Telemetry()


def get_telemetry():
    return _default_telemetry


def set_telemetry(telemetry):
    """Install a telemetry instance as the module default (used by tests)."""
    global _default_telemetry
    _default_telemetry = telemetry
