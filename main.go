package main

import (
	"log"

	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/db"
	"ledit/handlers"
)

func main() {
	drv, err := sql.Open("sqlite3", db.DSN())
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer drv.Close()

	srv := handlers.New(drv)

	log.Println("LEDit server starting on :8080")
	if err := srv.Router.Run(":8080"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
