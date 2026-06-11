package logging

import (
	"context"
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
