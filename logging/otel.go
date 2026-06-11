package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// OTelExporter handles exporting log records via OpenTelemetry.
// It creates an OTLP exporter, processor, and logger provider pipeline.
type OTelExporter struct {
	provider *sdklog.LoggerProvider
	logger   log.Logger
	enabled  bool
	endpoint string
	protocol string
	mu       sync.RWMutex
}

// NewOTelExporter creates a disabled OTel exporter.
func NewOTelExporter() *OTelExporter {
	return &OTelExporter{enabled: false}
}

// Configure updates the exporter settings. When enabled with a valid endpoint
// it initializes or reinitializes the OTLP pipeline.
func (e *OTelExporter) Configure(endpoint, protocol string, enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if endpoint == e.endpoint && protocol == e.protocol && enabled == e.enabled && e.provider != nil {
		return
	}

	// Shutdown previous provider if any
	if e.provider != nil {
		_ = e.provider.Shutdown(context.Background())
		e.provider = nil
	}

	e.enabled = enabled
	e.endpoint = endpoint
	e.protocol = protocol

	if enabled && endpoint != "" {
		e.initExporter()
	}
}

func (e *OTelExporter) initExporter() {
	ctx := context.Background()

	var exp sdklog.Exporter
	var err error

	switch e.protocol {
	case "http":
		exp, err = otlploghttp.New(ctx,
			otlploghttp.WithEndpoint(e.endpoint),
			otlploghttp.WithInsecure(),
		)
	default: // grpc
		exp, err = otlploggrpc.New(ctx,
			otlploggrpc.WithEndpoint(e.endpoint),
			otlploggrpc.WithInsecure(),
		)
	}

	if err != nil {
		slog.Error("failed to create OTEL exporter", "error", err, "protocol", e.protocol, "endpoint", e.endpoint)
		return
	}

	processor := sdklog.NewBatchProcessor(exp)
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(processor))

	e.provider = provider
	e.logger = provider.Logger("ledit")
}

// IsEnabled returns whether the exporter is active.
func (e *OTelExporter) IsEnabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enabled && e.provider != nil
}

// Export sends a slog record as an OpenTelemetry log record.
func (e *OTelExporter) Export(ctx context.Context, level slog.Level, source, message, metadata string) {
	e.mu.RLock()
	logger := e.logger
	e.mu.RUnlock()

	if logger == nil {
		return
	}

	rec := log.Record{}
	rec.SetTimestamp(time.Now())
	rec.SetSeverity(LevelToOTELSeverity(level))
	rec.SetSeverityText(LevelName(level))
	rec.SetBody(log.StringValue(message))

	var attrs []log.KeyValue
	if source != "" {
		attrs = append(attrs, log.String("source", source))
	}
	if metadata != "" {
		attrs = append(attrs, log.String("metadata", metadata))
	}
	if len(attrs) > 0 {
		rec.AddAttributes(attrs...)
	}

	logger.Emit(ctx, rec)
}

// Close shuts down the exporter and releases resources.
// It is safe to call multiple times.
func (e *OTelExporter) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.provider != nil {
		_ = e.provider.Shutdown(context.Background())
		e.provider = nil
	}
}
