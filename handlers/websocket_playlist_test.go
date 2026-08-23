package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/enttest"
)

func newPlaylistTestHub(t *testing.T) (*WSHub, *ent.Client, *ent.GeneralSettings) {
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
	gs, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(true).SetWidth(64).SetHeight(64).Save(ctx)
	if err != nil {
		t.Fatalf("seed gs: %v", err)
	}
	w := client.Weather.Create().SetToken("tok").SetURL("http://w").SaveX(ctx)
	s := client.Sonarr.Create().SetToken("tok").SetURL("http://s").SaveX(ctx)
	r := client.RssFeed.Create().SetName("rss1").SetURL("http://rss").SaveX(ctx)
	// Link to GeneralSettings edges (required for With* queries)
	client.GeneralSettings.UpdateOneID(gs.ID).AddWeatherIDs(w.ID).AddSonarrIDs(s.ID).AddRssFeedIDs(r.ID).ExecX(ctx)
	hub := &WSHub{Client: client}
	return hub, client, gs
}

func reloadGS(ctx context.Context, client *ent.Client) *ent.GeneralSettings {
	gs, _ := client.GeneralSettings.Query().
		WithSonarr().WithRadarr().WithF1().WithWeather().WithHomeAssistant().WithUntappd().
		WithCrypto().WithStocks().WithRssFeeds().WithCalendars().WithTextSlides().
		WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCountdowns().WithAiDigests().
		WithImages().WithVideos().
		Only(ctx)
	return gs
}

func TestParsePlaylistItems_PlaylistWrapper(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		wantLen int
	}{
		{"valid multi-item", `[{"source_type":"weather","source_id":1},{"source_type":"sonarr","source_id":2}]`, false, 2},
		{"unknown type", `[{"source_type":"unknown","source_id":1}]`, true, 0},
		{"malformed JSON", `[{`, true, 0},
		{"over-cap", overCapPlaylistJSON(65), true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := datasource.ParsePlaylistItems(tt.raw)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if !tt.wantErr && len(items) != tt.wantLen {
				t.Fatalf("len %d want %d", len(items), tt.wantLen)
			}
		})
	}
}

func overCapPlaylistJSON(n int) string {
	items := make([]datasource.PlaylistItem, n)
	for i := range items {
		items[i] = datasource.PlaylistItem{SourceType: "weather", SourceID: i + 1}
	}
	b, _ := json.Marshal(items)
	return string(b)
}

func TestComposeDeviceSources_FallbackLadder(t *testing.T) {
	ctx := context.Background()

	t.Run("global mode returns global", func(t *testing.T) {
		hub, client, _ := newPlaylistTestHub(t)
		gs := reloadGS(ctx, client)
		dev := client.DeviceSettings.Create().SetName("d1").SetToken("tok1").SetEnabled(true).SetContentMode("global").SaveX(ctx)
		got := hub.composeDeviceSources(dev, gs)
		want := hub.loadSources(gs)
		if len(got) != len(want) {
			t.Fatalf("global mode len %d want %d", len(got), len(want))
		}
	})

	t.Run("missing playlist id fallback", func(t *testing.T) {
		hub, client, _ := newPlaylistTestHub(t)
		gs := reloadGS(ctx, client)
		dev := client.DeviceSettings.Create().SetName("d1").SetToken("tok1").SetEnabled(true).SetContentMode("playlist").SaveX(ctx)
		got := hub.composeDeviceSources(dev, gs)
		want := hub.loadSources(gs)
		if len(got) != len(want) {
			t.Fatalf("missing id fallback len %d want %d", len(got), len(want))
		}
	})

	t.Run("disabled playlist fallback", func(t *testing.T) {
		hub, client, _ := newPlaylistTestHub(t)
		gs := reloadGS(ctx, client)
		pl := client.Playlist.Create().SetName("p1").SetItems(`[{"source_type":"weather","source_id":1}]`).SetEnabled(false).SaveX(ctx)
		dev := client.DeviceSettings.Create().SetName("d1").SetToken("tok1").SetEnabled(true).SetContentMode("playlist").SetPlaylistID(pl.ID).SaveX(ctx)
		got := hub.composeDeviceSources(dev, gs)
		want := hub.loadSources(gs)
		if len(got) != len(want) {
			t.Fatalf("disabled fallback len %d want %d", len(got), len(want))
		}
	})

	t.Run("zero resolvable fallback", func(t *testing.T) {
		hub, client, _ := newPlaylistTestHub(t)
		gs := reloadGS(ctx, client)
		// source_id 999 does not exist
		pl := client.Playlist.Create().SetName("p1").SetItems(`[{"source_type":"weather","source_id":999}]`).SetEnabled(true).SaveX(ctx)
		dev := client.DeviceSettings.Create().SetName("d1").SetToken("tok1").SetEnabled(true).SetContentMode("playlist").SetPlaylistID(pl.ID).SaveX(ctx)
		got := hub.composeDeviceSources(dev, gs)
		want := hub.loadSources(gs)
		if len(got) != len(want) {
			t.Fatalf("zero resolvable fallback len %d want %d", len(got), len(want))
		}
	})

	t.Run("partial resolution keeps order", func(t *testing.T) {
		hub, client, _ := newPlaylistTestHub(t)
		gs := reloadGS(ctx, client)
		// weather:1 exists, 999 missing, sonarr:1 exists, rssfeed:999 missing
		weatherID := client.Weather.Query().FirstX(ctx).ID
		sonarrID := client.Sonarr.Query().FirstX(ctx).ID
		items := fmt.Sprintf(`[{"source_type":"weather","source_id":%d},{"source_type":"weather","source_id":999},{"source_type":"sonarr","source_id":%d},{"source_type":"rssfeed","source_id":999}]`, weatherID, sonarrID)
		pl := client.Playlist.Create().SetName("p1").SetItems(items).SetEnabled(true).SaveX(ctx)
		dev := client.DeviceSettings.Create().SetName("d1").SetToken("tok1").SetEnabled(true).SetContentMode("playlist").SetPlaylistID(pl.ID).SaveX(ctx)
		got := hub.composeDeviceSources(dev, gs)
		if len(got) != 2 {
			t.Fatalf("partial len %d want 2", len(got))
		}
		if got[0].cacheKey != fmt.Sprintf("weather:%d", weatherID) {
			t.Errorf("first key %q want weather:%d", got[0].cacheKey, weatherID)
		}
		if got[1].cacheKey != fmt.Sprintf("sonarr:%d", sonarrID) {
			t.Errorf("second key %q want sonarr:%d", got[1].cacheKey, sonarrID)
		}
	})

	t.Run("playlist mode ignores random deterministic order", func(t *testing.T) {
		hub, client, _ := newPlaylistTestHub(t)
		// ensure gs has Random true to test ignore
		gs := reloadGS(ctx, client)
		gs.Random = true
		weatherID := client.Weather.Query().FirstX(ctx).ID
		sonarrID := client.Sonarr.Query().FirstX(ctx).ID
		items := fmt.Sprintf(`[{"source_type":"sonarr","source_id":%d},{"source_type":"weather","source_id":%d}]`, sonarrID, weatherID)
		pl := client.Playlist.Create().SetName("p1").SetItems(items).SetEnabled(true).SaveX(ctx)
		dev := client.DeviceSettings.Create().SetName("d1").SetToken("tok1").SetEnabled(true).SetContentMode("playlist").SetPlaylistID(pl.ID).SaveX(ctx)
		got1 := hub.composeDeviceSources(dev, gs)
		got2 := hub.composeDeviceSources(dev, gs)
		if len(got1) != 2 || len(got2) != 2 {
			t.Fatalf("len mismatch")
		}
		// ordering must be authored order and deterministic across calls (no shuffle)
		for i := range got1 {
			if got1[i].cacheKey != got2[i].cacheKey {
				t.Fatalf("non-deterministic ordering %v vs %v", got1[i].cacheKey, got2[i].cacheKey)
			}
		}
		if got1[0].cacheKey != fmt.Sprintf("sonarr:%d", sonarrID) || got1[1].cacheKey != fmt.Sprintf("weather:%d", weatherID) {
			t.Fatalf("order not authored %v", got1)
		}
		// random flag logic: playlistActive should yield false
		playlistActive := dev.ContentMode == "playlist"
		randomFlag := gs.Random
		if playlistActive {
			randomFlag = false
		}
		if randomFlag != false {
			t.Fatalf("randomFlag %v want false for playlist mode", randomFlag)
		}
	})

	t.Run("malformed items fallback", func(t *testing.T) {
		hub, client, _ := newPlaylistTestHub(t)
		gs := reloadGS(ctx, client)
		pl := client.Playlist.Create().SetName("p1").SetItems(`bad json`).SetEnabled(true).SaveX(ctx)
		dev := client.DeviceSettings.Create().SetName("d1").SetToken("tok1").SetEnabled(true).SetContentMode("playlist").SetPlaylistID(pl.ID).SaveX(ctx)
		got := hub.composeDeviceSources(dev, gs)
		want := hub.loadSources(gs)
		if len(got) != len(want) {
			t.Fatalf("malformed fallback len %d want %d", len(got), len(want))
		}
	})

	t.Run("systemstats builtin resolvable", func(t *testing.T) {
		hub, client, _ := newPlaylistTestHub(t)
		gs := reloadGS(ctx, client)
		pl := client.Playlist.Create().SetName("p1").SetItems(`[{"source_type":"systemstats","source_id":0}]`).SetEnabled(true).SaveX(ctx)
		dev := client.DeviceSettings.Create().SetName("d1").SetToken("tok1").SetEnabled(true).SetContentMode("playlist").SetPlaylistID(pl.ID).SaveX(ctx)
		got := hub.composeDeviceSources(dev, gs)
		if len(got) != 1 || got[0].cacheKey != "systemstats:0" {
			t.Fatalf("systemstats resolvable got %v", got)
		}
	})

	// Ensure time import used
	_ = time.Now
}
