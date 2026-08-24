package handlers

import (
	"context"
	"testing"

	"ledit/ent"
	"ledit/ent/enttest"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
)

func openMemDB(t *testing.T) *sql.Driver {
	t.Helper()
	drv, err := sql.Open(dialect.SQLite, "file:memdb1?mode=memory&cache=shared&_fk=1")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if db := drv.DB(); db != nil {
		db.SetMaxOpenConns(1)
	}
	return drv
}

func TestBrightnessPersistenceRoundTrip(t *testing.T) {
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(openMemDB(t))))
	defer client.Close()
	ctx := context.Background()
	// create with brightness
	ds := client.DeviceSettings.Create().
		SetName("persist").
		SetBrightnessEnabled(true).
		SetBrightnessSchedules(`[{"days":[1],"start":"22:00","end":"23:00","level":30}]`).
		SetBrightnessOverride(80).
		SaveX(ctx)
	if !ds.BrightnessEnabled || ds.BrightnessOverride == nil || *ds.BrightnessOverride != 80 {
		t.Fatalf("persist create %v", ds)
	}
	// clear override
	ds2 := client.DeviceSettings.UpdateOneID(ds.ID).ClearBrightnessOverride().SaveX(ctx)
	if ds2.BrightnessOverride != nil {
		t.Fatalf("clear override %v", ds2.BrightnessOverride)
	}
	// set override
	ds3 := client.DeviceSettings.UpdateOneID(ds.ID).SetBrightnessOverride(20).SaveX(ctx)
	if *ds3.BrightnessOverride != 20 {
		t.Fatalf("set override %d", *ds3.BrightnessOverride)
	}
	_ = ctx
}
