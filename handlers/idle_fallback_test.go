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

func newIdleTestHub(t *testing.T) (*WSHub, *ent.Client) {
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
	_, err = client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx)
	if err != nil {
		t.Fatalf("seed gs: %v", err)
	}
	hub := &WSHub{Client: client}
	return hub, client
}

func TestIdleFallback(t *testing.T) {
	hub, _ := newIdleTestHub(t)

	t.Run("nil device returns nil", func(t *testing.T) {
		if got := hub.idleFallback(nil); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
	t.Run("nil idle returns nil", func(t *testing.T) {
		dev := &ent.DeviceSettings{}
		if got := hub.idleFallback(dev); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
	t.Run("empty string returns nil", func(t *testing.T) {
		s := ""
		dev := &ent.DeviceSettings{IdleScreensaver: &s}
		if got := hub.idleFallback(dev); got != nil {
			t.Fatalf("expected nil, got %v", got)
		}
	})
	t.Run("invalid variant returns nil", func(t *testing.T) {
		s := "invalid"
		dev := &ent.DeviceSettings{IdleScreensaver: &s}
		if got := hub.idleFallback(dev); got != nil {
			t.Fatalf("expected nil for invalid, got %v", got)
		}
	})
	for _, v := range []string{"starfield", "dvd", "matrix", "plasma"} {
		v := v
		t.Run("valid "+v, func(t *testing.T) {
			s := v
			dev := &ent.DeviceSettings{IdleScreensaver: &s}
			got := hub.idleFallback(dev)
			if len(got) != 1 {
				t.Fatalf("expected 1, got %d", len(got))
			}
			if got[0].Name != "Screensaver: "+v {
				t.Fatalf("name %q", got[0].Name)
			}
			if got[0].cacheKey != fmt.Sprintf("screensaver:%d", map[string]int{"starfield": 0, "dvd": 1, "matrix": 2, "plasma": 3}[v]) {
				t.Fatalf("cacheKey %q", got[0].cacheKey)
			}
		})
	}
}

func TestComposeScheduledSourcesIdleFallback(t *testing.T) {
	ctx := context.Background()
	t.Run("global list set renders global not idle", func(t *testing.T) {
		hub, client := newIdleTestHub(t)
		gs := reloadGS(ctx, client)
		s := "starfield"
		dev := client.DeviceSettings.Create().SetName("d1").SetToken("tok1").SetEnabled(true).SetContentMode("scheduled").SetScheduledPlaylistIds("[]").SetIdleScreensaver(s).SaveX(ctx)
		got := hub.composeScheduledSources(dev, gs)
		// global list is non-empty (builtins), so idle should NOT be used
		want := hub.loadSources(gs)
		if len(got) != len(want) {
			t.Fatalf("expected global %d got %d", len(want), len(got))
		}
		for _, sw := range got {
			if strings.HasPrefix(sw.cacheKey, "screensaver:") && len(got) == 1 {
				t.Fatalf("should not be idle fallback when global non-empty, got %v", got)
			}
		}
	})
	t.Run("disabled idle leaves existing path", func(t *testing.T) {
		hub, client := newIdleTestHub(t)
		gs := reloadGS(ctx, client)
		// No idle set (nil), scheduled with empty ids and no fallback -> compose returns global (builtins)
		dev := client.DeviceSettings.Create().SetName("d1").SetToken("tok1").SetEnabled(true).SetContentMode("scheduled").SetScheduledPlaylistIds("[]").SaveX(ctx)
		got := hub.composeScheduledSources(dev, gs)
		if len(got) == 0 {
			t.Fatalf("expected global fallback, got empty")
		}
		// idleFallback with nil should be nil
		if got2 := hub.idleFallback(dev); got2 != nil {
			t.Fatalf("expected nil idle fallback, got %v", got2)
		}
	})
	t.Run("idle fallback directly when global empty simulation", func(t *testing.T) {
		hub, _ := newIdleTestHub(t)
		s := "plasma"
		dev := &ent.DeviceSettings{IdleScreensaver: &s}
		got := hub.idleFallback(dev)
		if len(got) != 1 || got[0].cacheKey != "screensaver:3" {
			t.Fatalf("expected plasma idle, got %v", got)
		}
	})
}
