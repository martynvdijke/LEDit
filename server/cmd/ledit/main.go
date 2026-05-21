package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/martynvdijke/ledit/internal/server"
	_ "github.com/mattn/go-sqlite3"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
)

func main() {
	dbPath := filepath.Join("data", "ledit.db")
	os.MkdirAll("data", 0755)

	drv, err := sql.Open(dialect.SQLite, dbPath+"?cache=shared&_fk=1")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer drv.Close()

	srv := server.New(drv)

	log.Println("LEDit server starting on :8080")
	if err := srv.Router.Run(":8080"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
