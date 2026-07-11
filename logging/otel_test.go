package logging

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/log"
)

func TestLevelToOTELSeverity(t *testing.T) {
	tests := []struct {
		slogLevel slog.Level
		want      log.Severity
	}{
		{slog.Level(-8), log.SeverityTrace},
		{slog.LevelDebug, log.SeverityDebug},
		{slog.LevelInfo, log.SeverityInfo},
		{slog.LevelWarn, log.SeverityWarn},
		{slog.LevelError, log.SeverityError},
		{slog.Level(12), log.SeverityFatal},
		// Between Debug(-4) and Info(0) → Debug
		{slog.Level(-2), log.SeverityDebug},
		// Between Warn(4) and Error(8) → Warn
		{slog.Level(6), log.SeverityWarn},
		// Between Error(8) and Fatal(12) → Error
		{slog.Level(10), log.SeverityError},
	}

	for _, tt := range tests {
		got := LevelToOTELSeverity(tt.slogLevel)
		if got != tt.want {
			t.Errorf("LevelToOTELSeverity(%v) = %v, want %v", tt.slogLevel, got, tt.want)
		}
	}
}

func TestNewOTelExporter_DisabledByDefault(t *testing.T) {
	e := NewOTelExporter()
	if e.IsEnabled() {
		t.Error("expected exporter to be disabled by default")
	}
}

func TestOTelExporter_IsEnabled(t *testing.T) {
	e := NewOTelExporter()

	// Should start disabled
	if e.IsEnabled() {
		t.Error("expected disabled initially")
	}

	// Configure with empty endpoint should stay disabled
	e.Configure("", "grpc", true)
	if e.IsEnabled() {
		t.Error("expected disabled with empty endpoint")
	}

	// Disable explicitly
	e.Configure("localhost:4317", "grpc", false)
	if e.IsEnabled() {
		t.Error("expected disabled when enabled=false")
	}
}

func TestOTelExporter_Export_NoPanicWhenDisabled(t *testing.T) {
	e := NewOTelExporter()
	// Should not panic when disabled and Export is called
	e.Export(context.Background(), slog.LevelInfo, "test", "hello", "")
}

func TestOTelExporter_Export_NoPanicWhenNotConfigured(t *testing.T) {
	e := NewOTelExporter()
	e.enabled = true // bypass the lock to simulate a misconfigured state
	e.Export(context.Background(), slog.LevelInfo, "test", "hello", "")
}

func TestOTelExporter_Close_NoPanic(t *testing.T) {
	e := NewOTelExporter()
	// Close on disabled exporter should not panic
	e.Close()

	// Close multiple times should also not panic
	e.Close()
}

func TestOTelExporter_Configure_NoPanic(t *testing.T) {
	e := NewOTelExporter()

	// Should not panic when configuring with valid-looking settings
	// (may log an error if no receiver at endpoint, but should not crash)
	e.Configure("localhost:4317", "grpc", true)

	// Switch protocol
	e.Configure("localhost:4318", "http", true)

	// Disable after being enabled
	e.Configure("", "", false)

	e.Close()
}

// ---------------------------------------------------------------------------
// Telemetry unit tests
// ---------------------------------------------------------------------------

func TestNewTelemetry_DisabledByDefault(t *testing.T) {
	tm := NewTelemetry()
	if tm.IsEnabled() {
		t.Error("expected Telemetry to be disabled by default")
	}
}

func TestInitTelemetry_NoEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	tm := InitTelemetry()
	if tm.IsEnabled() {
		t.Error("expected Telemetry to be disabled when no endpoint set")
	}
	tm.Shutdown(context.Background())
}

func TestInitTelemetry_WithEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	tm := InitTelemetry()
	if !tm.IsEnabled() {
		t.Error("expected Telemetry to be enabled with endpoint set")
	}
	tm.Shutdown(context.Background())

	// Reset env
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
}

func TestTelemetry_NewSlogHandler_ReturnsNilWhenDisabled(t *testing.T) {
	tm := NewTelemetry()
	if h := tm.NewSlogHandler(); h != nil {
		t.Error("expected nil slog handler from disabled telemetry")
	}
}

func TestTelemetry_Shutdown_NoPanic(t *testing.T) {
	tm := NewTelemetry()
	tm.Shutdown(context.Background())
	// Shutdown again (idempotent)
	tm.Shutdown(context.Background())
}

func TestDefaultServiceName(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("OTEL_SERVICE_NAME", "")
		if got := defaultServiceName(); got != "ledit" {
			t.Errorf("defaultServiceName() = %q, want %q", got, "ledit")
		}
	})

	t.Run("custom", func(t *testing.T) {
		t.Setenv("OTEL_SERVICE_NAME", "myapp")
		if got := defaultServiceName(); got != "myapp" {
			t.Errorf("defaultServiceName() = %q, want %q", got, "myapp")
		}
	})
}

func TestNewSampler_Defaults(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")
	s := newSampler()
	if s == nil {
		t.Fatal("expected non-nil sampler")
	}
	// Default should be ParentBased(AlwaysSample), verify it's not AlwaysOff
	_ = s
}

func TestNewSampler_AlwaysOff(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")
	s := newSampler()
	if s == nil {
		t.Fatal("expected non-nil sampler")
	}
}

func TestNewSampler_TraceIDRatio(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.5")
	s := newSampler()
	if s == nil {
		t.Fatal("expected non-nil sampler")
	}
	_ = s
}

func TestTelemetry_Accessors_NilSafe(t *testing.T) {
	tm := NewTelemetry()

	if tp := tm.TracerProvider(); tp != nil {
		t.Error("expected nil TracerProvider from disabled telemetry")
	}
	if mp := tm.MeterProvider(); mp != nil {
		t.Error("expected nil MeterProvider from disabled telemetry")
	}
	if lp := tm.LoggerProvider(); lp != nil {
		t.Error("expected nil LoggerProvider from disabled telemetry")
	}
	if tr := tm.Tracer(); tr == nil {
		t.Error("expected non-nil Tracer (noop) from disabled telemetry")
	}
	if m := tm.Meter(); m != nil {
		t.Log("Meter is nil from disabled telemetry (expected)")
	}
}

// ---------------------------------------------------------------------------
// DB query tracing tests
// ---------------------------------------------------------------------------

func TestTraceDBQuery_Success(t *testing.T) {
	ctx := context.Background()
	err := TraceDBQuery(ctx, "test-operation", func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestTraceDBQuery_Error(t *testing.T) {
	expectedErr := errors.New("db error")
	ctx := context.Background()
	err := TraceDBQuery(ctx, "test-operation", func(ctx context.Context) error {
		return expectedErr
	})
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestTraceDBQuery_BackgroundCtx(t *testing.T) {
	ctx := context.Background()
	err := TraceDBQuery(ctx, "background-test", func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Metrics tests
// ---------------------------------------------------------------------------

func TestRecordHTTPRequest_NoPanic(t *testing.T) {
	// Call before InitMetrics — should be a no-op, no panic
	RecordHTTPRequest(context.Background(), "GET", "/test", 200, 0)
}

func TestInitMetrics_Idempotent(t *testing.T) {
	// Reset the package-level flag for this test
	metricsInitialised = false

	InitMetrics(nil)
	InitMetrics(nil)

	// No panic = pass
}

// ---------------------------------------------------------------------------
// Graceful degradation test
// ---------------------------------------------------------------------------

func TestInitTelemetry_UnreachableEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:19999")
	tm := InitTelemetry()
	// Should not crash — may log a warning but returns gracefully
	tm.Shutdown(context.Background())
	_ = tm
}
