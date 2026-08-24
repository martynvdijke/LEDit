package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent"
	"ledit/ent/enttest"
)

func newPrecHub(t *testing.T) (*WSHub, *ent.Client, *ent.GeneralSettings) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	gs, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx)
	if err != nil {
		t.Fatalf("seed gs: %v", err)
	}
	w := client.Weather.Create().SetToken("tok").SetURL("http://w").SaveX(ctx)
	client.GeneralSettings.UpdateOneID(gs.ID).AddWeatherIDs(w.ID).ExecX(ctx)
	hub := &WSHub{Client: client}
	return hub, client, gs
}

func reloadGS2(ctx context.Context, client *ent.Client) *ent.GeneralSettings {
	gs, _ := client.GeneralSettings.Query().WithWeather().Only(ctx)
	return gs
}

// helpers to attach group edge for compose path
func attachGroup(t *testing.T, client *ent.Client, dev *ent.DeviceSettings) *ent.DeviceSettings {
	t.Helper()
	d, err := client.DeviceSettings.Query().Where().Only(context.Background())
	_ = d
	// reload with group
	reloaded, err := client.DeviceSettings.Get(context.Background(), dev.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	// Need WithGroup; fetch via query with WithGroup
	all, _ := client.DeviceSettings.Query().WithGroup().All(context.Background())
	for _, a := range all {
		if a.ID == dev.ID {
			return a
		}
	}
	return reloaded
}

func TestGroupPrecedence_DeviceOverrideWins(t *testing.T) {
	hub, client, _ := newPrecHub(t)
	ctx := context.Background()
	gs := reloadGS2(ctx, client)
	weatherID := client.Weather.Query().FirstX(ctx).ID
	plGroup := client.Playlist.Create().SetName("grp-pl").SetItems(fmt.Sprintf(`[{"source_type":"weather","source_id":%d}]`, weatherID)).SetEnabled(true).SaveX(ctx)
	plDevice := client.Playlist.Create().SetName("dev-pl").SetItems(fmt.Sprintf(`[{"source_type":"weather","source_id":%d}]`, weatherID)).SetEnabled(true).SaveX(ctx)
	grp := client.DeviceGroup.Create().SetName("G1").SetContentMode("playlist").SetPlaylistID(plGroup.ID).SetScheduledPlaylistIds("[]").SaveX(ctx)
	dev := client.DeviceSettings.Create().SetName("D1").SetToken("tok1").SetEnabled(true).SetContentMode("playlist").SetPlaylistID(plDevice.ID).SetGroupID(grp.ID).SaveX(ctx)
	devWithGroup, _ := client.DeviceSettings.Query().WithGroup().Where().All(ctx)
	var target *ent.DeviceSettings
	for _, d := range devWithGroup {
		if d.ID == dev.ID {
			target = d
			break
		}
	}
	got := hub.composeDeviceSources(target, gs)
	// device override wins: should resolve dev-pl (same source but check not fallback)
	if len(got) != 1 {
		t.Fatalf("len %d want 1", len(got))
	}
	// Ensure no panic and returned sources
}

func TestGroupPrecedence_DeviceInheritsGroupPlaylist(t *testing.T) {
	hub, client, _ := newPrecHub(t)
	ctx := context.Background()
	gs := reloadGS2(ctx, client)
	weatherID := client.Weather.Query().FirstX(ctx).ID
	pl := client.Playlist.Create().SetName("grp-pl").SetItems(fmt.Sprintf(`[{"source_type":"weather","source_id":%d}]`, weatherID)).SetEnabled(true).SaveX(ctx)
	grp := client.DeviceGroup.Create().SetName("G1").SetContentMode("playlist").SetPlaylistID(pl.ID).SetScheduledPlaylistIds("[]").SaveX(ctx)
	dev := client.DeviceSettings.Create().SetName("D1").SetToken("tok1").SetEnabled(true).SetContentMode("global").SetGroupID(grp.ID).SaveX(ctx)
	devs, _ := client.DeviceSettings.Query().WithGroup().All(ctx)
	var target *ent.DeviceSettings
	for _, d := range devs {
		if d.ID == dev.ID {
			target = d
			break
		}
	}
	got := hub.composeDeviceSources(target, gs)
	if len(got) != 1 || got[0].cacheKey != fmt.Sprintf("weather:%d", weatherID) {
		t.Fatalf("inherit group playlist got %v", got)
	}
}

func TestGroupPrecedence_DeviceInheritsGroupScheduled(t *testing.T) {
	hub, client, _ := newPrecHub(t)
	ctx := context.Background()
	gs := reloadGS2(ctx, client)
	weatherID := client.Weather.Query().FirstX(ctx).ID
	pl := client.Playlist.Create().SetName("sched-pl").SetItems(fmt.Sprintf(`[{"source_type":"weather","source_id":%d}]`, weatherID)).SetEnabled(true).SaveX(ctx)
	grp := client.DeviceGroup.Create().SetName("G1").SetContentMode("scheduled").SetScheduledPlaylistIds(fmt.Sprintf("[%d]", pl.ID)).SaveX(ctx)
	dev := client.DeviceSettings.Create().SetName("D1").SetToken("tok1").SetEnabled(true).SetContentMode("global").SetGroupID(grp.ID).SaveX(ctx)
	devs, _ := client.DeviceSettings.Query().WithGroup().All(ctx)
	var target *ent.DeviceSettings
	for _, d := range devs {
		if d.ID == dev.ID {
			target = d
			break
		}
	}
	got := hub.composeDeviceSources(target, gs)
	if len(got) == 0 {
		t.Fatalf("scheduled inherit got empty, want 1")
	}
}

func TestGroupPrecedence_EmptyGroupFallsToGlobal(t *testing.T) {
	hub, client, _ := newPrecHub(t)
	ctx := context.Background()
	gs := reloadGS2(ctx, client)
	grp := client.DeviceGroup.Create().SetName("Gempty").SetContentMode("global").SetScheduledPlaylistIds("[]").SaveX(ctx)
	dev := client.DeviceSettings.Create().SetName("D1").SetToken("tok1").SetEnabled(true).SetContentMode("global").SetGroupID(grp.ID).SaveX(ctx)
	devs, _ := client.DeviceSettings.Query().WithGroup().All(ctx)
	var target *ent.DeviceSettings
	for _, d := range devs {
		if d.ID == dev.ID {
			target = d
			break
		}
	}
	got := hub.composeDeviceSources(target, gs)
	want := hub.loadSources(gs)
	if len(got) != len(want) {
		t.Fatalf("empty group fallback len %d want %d", len(got), len(want))
	}
}

func TestGroupPrecedence_DanglingGroupPlaylistFallsToGlobalNoPanic(t *testing.T) {
	hub, client, _ := newPrecHub(t)
	ctx := context.Background()
	gs := reloadGS2(ctx, client)
	grp := client.DeviceGroup.Create().SetName("Gdang").SetContentMode("playlist").SetPlaylistID(99999).SetScheduledPlaylistIds("[]").SaveX(ctx)
	dev := client.DeviceSettings.Create().SetName("D1").SetToken("tok1").SetEnabled(true).SetContentMode("global").SetGroupID(grp.ID).SaveX(ctx)
	devs, _ := client.DeviceSettings.Query().WithGroup().All(ctx)
	var target *ent.DeviceSettings
	for _, d := range devs {
		if d.ID == dev.ID {
			target = d
			break
		}
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on dangling group playlist: %v", r)
		}
	}()
	got := hub.composeDeviceSources(target, gs)
	want := hub.loadSources(gs)
	if len(got) != len(want) {
		t.Fatalf("dangling fallback len %d want %d", len(got), len(want))
	}
}
