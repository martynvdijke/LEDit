package main

import (
	"log/slog"
	"os"

	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/db"
	"ledit/handlers"
)

func main() {
	drv, err := sql.Open("sqlite3", db.DSN())
	if err != nil {
		slog.Error("Failed to open database", "error", err)
		os.Exit(1)
	}
	defer drv.Close()

	srv := handlers.New(drv)

	slog.Info("LEDit server starting", "port", 8080)
	if err := srv.Router.Run(":8080"); err != nil {
		slog.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}
