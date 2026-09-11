package logging

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry holds all OpenTelemetry providers (tracer, meter, logger).
type Telemetry struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	loggerProvider *sdklog.LoggerProvider

	tracer trace.Tracer
	meter  otelmetric.Meter
	logger otellog.Logger

	enabled  bool
	endpoint string
	protocol string
	mu       sync.RWMutex
}

// NewTelemetry creates a disabled Telemetry instance.
func NewTelemetry() *Telemetry {
	return &Telemetry{enabled: false}
}

// IsEnabled returns whether the telemetry pipeline is active.
func (t *Telemetry) IsEnabled() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.enabled && t.tracerProvider != nil
}

// InitTelemetry initialises the full OTel pipeline (trace, metric, log)
// based on standard OTEL_* environment variables.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is not set, noop providers are returned
// (graceful degradation).
func InitTelemetry() *Telemetry {
	t := NewTelemetry()

	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") {
		slog.Info("OTel telemetry disabled – OTEL_SDK_DISABLED=true")
		return t
	}

	// ponytail: only the general OTEL_EXPORTER_OTLP_ENDPOINT gates telemetry.
	// Add per-signal endpoints (OTEL_EXPORTER_OTLP_{TRACES,METRICS,LOGS}_ENDPOINT)
	// when a deployment needs different backends per signal.
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	protocol := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	if protocol == "" {
		protocol = "grpc"
	}

	if endpoint == "" {
		slog.Info("OTel telemetry disabled – no OTEL_EXPORTER_OTLP_ENDPOINT set")
		return t
	}

	t.enabled = true
	t.endpoint = endpoint
	t.protocol = protocol
	t.initProviders()

	return t
}

func (t *Telemetry) initProviders() {
	ctx := context.Background()

	// Build resource from env vars (OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES).
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(
			semconv.ServiceName(defaultServiceName()),
		),
	)
	if err != nil {
		slog.Warn("failed to build OTel resource, using default", "error", err)
		res = resource.Default()
	}

	// -- Trace exporter & provider --
	traceExp, err := t.createTraceExporter(ctx)
	if err != nil {
		slog.Warn("failed to create OTel trace exporter, traces disabled", "error", err)
		traceExp = nil
	}

	if traceExp != nil {
		sampler := newSampler()
		bsp := sdktrace.NewBatchSpanProcessor(traceExp)
		t.tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sampler),
			sdktrace.WithResource(res),
			sdktrace.WithSpanProcessor(bsp),
		)
		t.tracer = t.tracerProvider.Tracer("ledit")
		otel.SetTracerProvider(t.tracerProvider)
	}

	// -- Metric exporter & provider --
	metricExp, err := t.createMetricExporter(ctx)
	if err != nil {
		slog.Warn("failed to create OTel metric exporter, metrics disabled", "error", err)
		metricExp = nil
	}

	if metricExp != nil {
		reader := sdkmetric.NewPeriodicReader(metricExp)
		t.meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(reader),
		)
		t.meter = t.meterProvider.Meter("ledit")
		otel.SetMeterProvider(t.meterProvider)
	}

	// -- Log exporter & provider --
	logExp, err := t.createLogExporter(ctx)
	if err != nil {
		slog.Warn("failed to create OTel log exporter, logs OTLP disabled", "error", err)
		logExp = nil
	}

	if logExp != nil {
		processor := sdklog.NewBatchProcessor(logExp)
		t.loggerProvider = sdklog.NewLoggerProvider(
			sdklog.WithResource(res),
			sdklog.WithProcessor(processor),
		)
		t.logger = t.loggerProvider.Logger("ledit")
	}
}

// The OTLP exporters read the standard OTEL_* environment variables themselves
// (endpoint URL incl. scheme/path, TLS, headers, compression, timeouts). We
// therefore pass no endpoint option when OTEL_EXPORTER_OTLP_ENDPOINT is a full
// URL, so the SDK parses it correctly. A bare "host:port" is not a valid URL
// and would be mis-parsed by the SDK, so we fall back to the insecure
// host:port form for backwards compatibility.

func (t *Telemetry) createTraceExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	if isHTTPProtocol(t.protocol) {
		if hasScheme(t.endpoint) {
			return otlptracehttp.New(ctx)
		}
		return otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(t.endpoint), otlptracehttp.WithInsecure())
	}
	if hasScheme(t.endpoint) {
		return otlptracegrpc.New(ctx)
	}
	return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(t.endpoint), otlptracegrpc.WithInsecure())
}

func (t *Telemetry) createMetricExporter(ctx context.Context) (sdkmetric.Exporter, error) {
	if isHTTPProtocol(t.protocol) {
		if hasScheme(t.endpoint) {
			return otlpmetrichttp.New(ctx)
		}
		return otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpoint(t.endpoint), otlpmetrichttp.WithInsecure())
	}
	if hasScheme(t.endpoint) {
		return otlpmetricgrpc.New(ctx)
	}
	return otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpoint(t.endpoint), otlpmetricgrpc.WithInsecure())
}

func (t *Telemetry) createLogExporter(ctx context.Context) (sdklog.Exporter, error) {
	if isHTTPProtocol(t.protocol) {
		if hasScheme(t.endpoint) {
			return otlploghttp.New(ctx)
		}
		return otlploghttp.New(ctx, otlploghttp.WithEndpoint(t.endpoint), otlploghttp.WithInsecure())
	}
	if hasScheme(t.endpoint) {
		return otlploggrpc.New(ctx)
	}
	return otlploggrpc.New(ctx, otlploggrpc.WithEndpoint(t.endpoint), otlploggrpc.WithInsecure())
}

// TracerProvider returns the trace provider (nil if disabled).
func (t *Telemetry) TracerProvider() *sdktrace.TracerProvider {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.tracerProvider
}

// MeterProvider returns the meter provider (nil if disabled).
func (t *Telemetry) MeterProvider() *sdkmetric.MeterProvider {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.meterProvider
}

// LoggerProvider returns the log provider (nil if disabled).
func (t *Telemetry) LoggerProvider() *sdklog.LoggerProvider {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.loggerProvider
}

// Tracer returns the named tracer (noop if disabled).
func (t *Telemetry) Tracer() trace.Tracer {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.tracer != nil {
		return t.tracer
	}
	return trace.NewNoopTracerProvider().Tracer("ledit")
}

// Meter returns the meter (nil if disabled).
func (t *Telemetry) Meter() otelmetric.Meter {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.meter
}

// NewSlogHandler creates a slog.Handler that forwards records to the OTel
// logs SDK with trace context correlation. Returns nil if telemetry is
// disabled.
func (t *Telemetry) NewSlogHandler() slog.Handler {
	t.mu.RLock()
	lp := t.loggerProvider
	t.mu.RUnlock()

	if lp == nil {
		return nil
	}

	return otelslog.NewHandler("ledit", otelslog.WithLoggerProvider(lp))
}

// Shutdown flushes and shuts down all providers gracefully.
func (t *Telemetry) Shutdown(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tracerProvider != nil {
		_ = t.tracerProvider.Shutdown(ctx)
		t.tracerProvider = nil
	}
	if t.meterProvider != nil {
		_ = t.meterProvider.Shutdown(ctx)
		t.meterProvider = nil
	}
	if t.loggerProvider != nil {
		_ = t.loggerProvider.Shutdown(ctx)
		t.loggerProvider = nil
	}
	t.enabled = false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// defaultServiceName returns the OTEL_SERVICE_NAME value, defaulting to "ledit".
func defaultServiceName() string {
	if n := os.Getenv("OTEL_SERVICE_NAME"); n != "" {
		return n
	}
	return "ledit"
}

// newSampler returns the sampler configured by OTEL_TRACES_SAMPLER and
// OTEL_TRACES_SAMPLER_ARG env vars.
func newSampler() sdktrace.Sampler {
	samplerStr := os.Getenv("OTEL_TRACES_SAMPLER")
	argStr := os.Getenv("OTEL_TRACES_SAMPLER_ARG")

	switch samplerStr {
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		ratio := parseSampleRatio(argStr)
		return sdktrace.TraceIDRatioBased(ratio)
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		ratio := parseSampleRatio(argStr)
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
	default:
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}

func parseSampleRatio(s string) float64 {
	if s == "" {
		return 1.0
	}
	r, err := strconv.ParseFloat(s, 64)
	if err != nil || r < 0 || r > 1 {
		return 1.0
	}
	return r
}

// isHTTPProtocol reports whether an OTEL_EXPORTER_OTLP_PROTOCOL value selects
// the OTLP/HTTP transport ("http", "http/protobuf", "http/json").
func isHTTPProtocol(protocol string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(protocol)), "http")
}

// hasScheme reports whether endpoint is a full URL (e.g.
// "https://collector.example.com:4318") rather than a bare "host:port".
func hasScheme(endpoint string) bool {
	return strings.Contains(endpoint, "://")
}

// otlpEndpoint splits an admin-configured OTLP endpoint into its host, URL
// path, and whether the connection should be insecure. A bare "host:port" is
// treated as insecure to match historical behaviour.
func otlpEndpoint(endpoint string) (host, path string, insecure bool) {
	if !hasScheme(endpoint) {
		return endpoint, "", true
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint, "", true
	}
	return u.Host, u.Path, u.Scheme != "https"
}

func logHTTPOptions(endpoint string) []otlploghttp.Option {
	host, path, insecure := otlpEndpoint(endpoint)
	opts := []otlploghttp.Option{otlploghttp.WithEndpoint(host)}
	if path != "" {
		opts = append(opts, otlploghttp.WithURLPath(path))
	}
	if insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	return opts
}

func logGRPCOptions(endpoint string) []otlploggrpc.Option {
	host, _, insecure := otlpEndpoint(endpoint)
	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(host)}
	if insecure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	return opts
}

// ---------------------------------------------------------------------------
// Legacy OTelExporter (kept for backward compatibility with existing DBHandler)
// ---------------------------------------------------------------------------

// OTelExporter handles exporting log records via OpenTelemetry.
type OTelExporter struct {
	provider *sdklog.LoggerProvider
	logger   otellog.Logger
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
// it initialises or reinitialises the OTLP pipeline.
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

	if isHTTPProtocol(e.protocol) {
		exp, err = otlploghttp.New(ctx, logHTTPOptions(e.endpoint)...)
	} else {
		exp, err = otlploggrpc.New(ctx, logGRPCOptions(e.endpoint)...)
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

	rec := otellog.Record{}
	rec.SetTimestamp(time.Now())
	rec.SetSeverity(LevelToOTELSeverity(level))
	rec.SetSeverityText(LevelName(level))
	rec.SetBody(attribute.StringValue(message))

	var attrs []attribute.KeyValue
	if source != "" {
		attrs = append(attrs, attribute.String("source", source))
	}
	if metadata != "" {
		attrs = append(attrs, attribute.String("metadata", metadata))
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

// ---------------------------------------------------------------------------
// Metrics instruments (initialised once from InitTelemetry)
// ---------------------------------------------------------------------------

var (
	httpRequestsTotal   otelmetric.Int64Counter
	httpRequestDuration otelmetric.Float64Histogram
	metricsInitialised  bool
	metricsInitMu       sync.Mutex
)

// InitMetrics creates OTel metric instruments on the given meter.
// Safe to call multiple times (idempotent).
func InitMetrics(meter otelmetric.Meter) {
	if meter == nil {
		// Check if the interface is nil (Go interface quirk)
		return
	}
	metricsInitMu.Lock()
	defer metricsInitMu.Unlock()
	if metricsInitialised {
		return
	}

	var err error
	httpRequestsTotal, err = meter.Int64Counter("otel_http_requests_total",
		otelmetric.WithDescription("Total number of HTTP requests"),
	)
	if err != nil {
		slog.Warn("failed to create http_requests_total counter", "error", err)
	}

	httpRequestDuration, err = meter.Float64Histogram("otel_http_request_duration_seconds",
		otelmetric.WithDescription("Duration of HTTP requests in seconds"),
		otelmetric.WithUnit("s"),
	)
	if err != nil {
		slog.Warn("failed to create http_request_duration_seconds histogram", "error", err)
	}

	metricsInitialised = true
}

// RecordHTTPRequest records metrics for a single HTTP request.
func RecordHTTPRequest(ctx context.Context, method, path string, status int, duration time.Duration) {
	if !metricsInitialised {
		return
	}
	attrs := otelmetric.WithAttributes(
		attribute.String("http.method", method),
		attribute.String("http.path", path),
		attribute.Int("http.status_code", status),
	)

	if httpRequestsTotal != nil {
		httpRequestsTotal.Add(ctx, 1, attrs)
	}
	if httpRequestDuration != nil {
		httpRequestDuration.Record(ctx, duration.Seconds(), attrs)
	}
}

// Compile-time checks to avoid unused import errors.
var (
	_ = otellog.Logger(nil)
	_ = otelmetric.Meter(nil)
	_ = trace.Tracer(nil)
)
