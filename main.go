package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"strings"

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

	// Start the background alert engine (polls health registry + device liveness).
	srv.StartAlertEngine(context.Background())

	// Wire the OTel slog bridge for log-to-trace correlation.
	if telemetry.IsEnabled() {
		if otelHandler := telemetry.NewSlogHandler(); otelHandler != nil {
			currentHandler := slog.Default().Handler()
			slog.SetDefault(slog.New(slog.NewMultiHandler(currentHandler, otelHandler)))
		}
		logging.InitMetrics(telemetry.Meter())
	}

	// Port resolution: --port / --addr / -port / -addr flags take precedence,
	// then LEDIT_PORT, PORT, LEDIT_ADDR env vars, then default 8080.
	// Supports ephemeral-port integration harness (PORT=0 or --port 0).
	var flagPort, flagAddr string
	flag.StringVar(&flagPort, "port", "", "port to listen on")
	flag.StringVar(&flagPort, "p", "", "port to listen on (shorthand)")
	flag.StringVar(&flagAddr, "addr", "", "address to listen on (e.g. :8080)")
	// Also register long-form with dash prefix handling via flag package; manual scan for --port/--addr for robustness.
	flag.Parse()
	// Manual scan for --port / --addr in case flag parsing missed due to Gin args.
	for i, arg := range os.Args {
		if (arg == "--port" || arg == "-port" || arg == "--p" || arg == "-p") && i+1 < len(os.Args) {
			flagPort = os.Args[i+1]
		} else if strings.HasPrefix(arg, "--port=") {
			flagPort = strings.TrimPrefix(arg, "--port=")
		} else if strings.HasPrefix(arg, "-port=") {
			flagPort = strings.TrimPrefix(arg, "-port=")
		} else if (arg == "--addr" || arg == "-addr") && i+1 < len(os.Args) {
			flagAddr = os.Args[i+1]
		} else if strings.HasPrefix(arg, "--addr=") {
			flagAddr = strings.TrimPrefix(arg, "--addr=")
		} else if strings.HasPrefix(arg, "-addr=") {
			flagAddr = strings.TrimPrefix(arg, "-addr=")
		}
	}

	port := flagPort
	addr := flagAddr
	if port == "" && addr == "" {
		port = os.Getenv("LEDIT_PORT")
		if port == "" {
			port = os.Getenv("PORT")
		}
		if port == "" {
			if v := os.Getenv("LEDIT_ADDR"); v != "" {
				addr = v
			}
		}
	}
	if addr != "" {
		// Normalize addr: if it contains ":", use as-is; otherwise treat as :port or host:port.
		if !strings.Contains(addr, ":") {
			addr = ":" + addr
		}
		slog.Info("LEDit server starting", "addr", addr)
		if err := srv.Router.Run(addr); err != nil {
			slog.Error("Failed to start server", "error", err)
			os.Exit(1)
		}
		return
	}
	if port == "" {
		port = "8080"
	}
	// Strip leading ":" if caller passed ":8080".
	port = strings.TrimPrefix(port, ":")
	slog.Info("LEDit server starting", "port", port)
	if err := srv.Router.Run(":" + port); err != nil {
		slog.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}
