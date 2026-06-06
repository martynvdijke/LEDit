package logging

import (
	"context"
	"log"
	"sync"
)

// OTelExporter is a placeholder for OpenTelemetry log export.
// In V1, this logs a message indicating OTEL would export.
// Full OTLP integration requires adding go.opentelemetry.io/otel dependencies.
type OTelExporter struct {
	enabled  bool
	endpoint string
	protocol string
	mu       sync.RWMutex
}

// NewOTelExporter creates a disabled OTel exporter.
func NewOTelExporter() *OTelExporter {
	return &OTelExporter{enabled: false}
}

// Configure updates the exporter settings.
func (e *OTelExporter) Configure(endpoint, protocol string, enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enabled = enabled
	e.endpoint = endpoint
	e.protocol = protocol
}

// IsEnabled returns whether the exporter is active.
func (e *OTelExporter) IsEnabled() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.enabled
}

// Export sends a log record via OTLP.
// Currently a no-op that logs the intent. Full OTLP integration (task 12) will
// replace this with actual gRPC/HTTP export.
func (e *OTelExporter) Export(ctx context.Context, level, source, message, metadata string) {
	e.mu.RLock()
	enabled := e.enabled
	endpoint := e.endpoint
	protocol := e.protocol
	e.mu.RUnlock()

	if !enabled {
		return
	}

	// Placeholder: log the export intent
	log.Printf("OTEL export (%s): level=%s source=%s msg=%s endpoint=%s", protocol, level, source, message, endpoint)
}

// Close shuts down the exporter.
func (e *OTelExporter) Close() {
	log.Println("OTEL exporter closed")
}
