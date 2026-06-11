package logging

import (
	"context"
	"log"
	"log/slog"
	"time"

	"ledit/ent"
)

// DBHandler is a slog.Handler that writes log entries to the database via LogStore
// and optionally exports via OTel.
type DBHandler struct {
	store    *LogStore
	otel     *OTelExporter
	minLevel slog.Level
}

// NewDBHandler creates a new DBHandler with the given store and minimum level.
func NewDBHandler(store *LogStore, otel *OTelExporter, minLevel slog.Level) *DBHandler {
	return &DBHandler{
		store:    store,
		otel:     otel,
		minLevel: minLevel,
	}
}

// SetMinLevel updates the minimum log level dynamically (e.g., from settings).
func (h *DBHandler) SetMinLevel(level slog.Level) {
	h.minLevel = level
}

// Enabled reports whether the handler handles records at the given level.
func (h *DBHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

// Handle writes the log record to the database and OTel.
func (h *DBHandler) Handle(_ context.Context, r slog.Record) error {
	levelName := LevelName(r.Level)
	source := "app"
	msg := r.Message

	// Collect attributes as JSON-like metadata string
	var metadata string
	if r.NumAttrs() > 0 {
		meta := ""
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "source" {
				source = a.Value.String()
				return true
			}
			if meta != "" {
				meta += ","
			}
			meta += a.Key + "=" + a.Value.String()
			return true
		})
		metadata = meta
	}

	// Store in database
	h.store.Submit(r.Time, levelName, source, msg, metadata)

	// Export via OTel
	if h.otel != nil && h.otel.IsEnabled() {
		h.otel.Export(context.Background(), r.Level, source, msg, metadata)
	}

	return nil
}

// WithAttrs returns a new handler with additional attributes (not implemented for simplicity).
func (h *DBHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

// WithGroup returns a new handler with a group (not implemented for simplicity).
func (h *DBHandler) WithGroup(name string) slog.Handler {
	return h
}

// InitLogging initializes the global slog logger with the DB-backed handler.
// It creates the LogStore, OTelExporter, LogCleanup, and sets slog.SetDefault.
func InitLogging(client *ent.Client, minLevelStr string) (*LogStore, *OTelExporter, *LogCleanup) {
	store := NewLogStore(client, 1000)
	otel := NewOTelExporter()
	cleanup := NewLogCleanup(client, 1*time.Hour)

	minLevel := ParseLevel(minLevelStr)
	handler := NewDBHandler(store, otel, minLevel)
	slog.SetDefault(slog.New(handler))

	log.Println("central logging initialized, minimum level:", minLevelStr)

	return store, otel, cleanup
}
