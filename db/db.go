package db

import (
	"os"
	"path/filepath"
)

func DSN() string {
	dbDir := "data"
	dbPath := filepath.Join(dbDir, "ledit.db")
	os.MkdirAll(dbDir, 0755)
	return dbPath + "?cache=shared&_fk=1"
}
