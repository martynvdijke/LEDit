package main

import (
	"context"
	"log/slog"
	"os"

	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/db"
	"ledit/handlers"
	"ledit/logging"
)

func main() {
	drv, err := sql.Open("sqlite3", db.DSN())
	if err != nil {
		slog.Error("Failed to open database", "error", err)
		os.Exit(1)
	}
	defer drv.Close()

	// Initialise OTel telemetry pipeline (noop if no OTEL_EXPORTER_OTLP_ENDPOINT).
	telemetry := logging.InitTelemetry()
	defer telemetry.Shutdown(context.Background())

	srv := handlers.New(drv, telemetry)

	// Wire the OTel slog bridge for log-to-trace correlation.
	if telemetry.IsEnabled() {
		if otelHandler := telemetry.NewSlogHandler(); otelHandler != nil {
			currentHandler := slog.Default().Handler()
			slog.SetDefault(slog.New(slog.NewMultiHandler(currentHandler, otelHandler)))
		}
		logging.InitMetrics(telemetry.Meter())
	}

	slog.Info("LEDit server starting", "port", 8080)
	if err := srv.Router.Run(":8080"); err != nil {
		slog.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}
