package handlers

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/enttest"
	"ledit/ent/generalsettings"
)

func newNowPlayingIntegrationServer(t *testing.T) (*Server, *ent.Client) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	if _, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := &Server{DB: client, Ctx: ctx, Router: gin.New(), WSHub: NewWSHub(client)}
	return srv, client
}

func TestNowPlayingIntegrationFeedPlaylistMatrix(t *testing.T) {
	srv, client := newNowPlayingIntegrationServer(t)
	idle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer idle.Close()

	np := client.NowPlayingSource.Create().
		SetName("Living Room").SetProvider("jellyfin").SetURL(idle.URL).
		SetToken("tok").SetUsername("alice").SetShowAlbumArt(false).
		SaveX(srv.Ctx)
	if _, err := client.GeneralSettings.UpdateOneID(1).AddNowPlayingSources(np).Save(srv.Ctx); err != nil {
		t.Fatalf("add edge: %v", err)
	}
	wantKey := fmt.Sprintf("nowplaying:%d", np.ID)

	load := func() *ent.GeneralSettings {
		t.Helper()
		gs, err := client.GeneralSettings.Query().Where(generalsettings.ID(1)).WithNowPlayingSources().WithMatrixLayouts().Only(srv.Ctx)
		if err != nil {
			t.Fatalf("load settings: %v", err)
		}
		return gs
	}

	// Global feed carries the configured provider fields.
	gs := load()
	var feedSrc datasource.Datasource
	for _, s := range srv.WSHub.loadSources(gs) {
		if s.cacheKey == wantKey {
			feedSrc = s.Source
		}
	}
	ds, ok := feedSrc.(*datasource.NowPlayingSourceDS)
	if !ok {
		t.Fatal("nowplaying source not found in global feed")
	}
	if ds.Provider != "jellyfin" || ds.URL != idle.URL || ds.Token != "tok" || ds.Username != "alice" {
		t.Errorf("feed options = %+v", ds)
	}

	// Device playlist resolves the source in authored order.
	pl := client.Playlist.Create().
		SetName("np playlist").
		SetItems(fmt.Sprintf(`[{"source_type":"nowplaying","source_id":%d}]`, np.ID)).
		SetEnabled(true).
		SaveX(srv.Ctx)
	dev := client.DeviceSettings.Create().
		SetName("NP Device").SetToken("np-token").SetEnabled(true).
		SetContentMode("playlist").SetPlaylistID(pl.ID).
		SaveX(srv.Ctx)
	composed := srv.WSHub.composeDeviceSources(dev, load())
	found := false
	for _, s := range composed {
		if s.cacheKey == wantKey {
			found = true
		}
	}
	if !found {
		t.Error("playlist feed should include the now-playing source")
	}

	// Matrix cell binding renders (idle provider) at a small cell size.
	ml := client.MatrixLayout.Create().
		SetName("NP grid").SetRows(1).SetCols(3).SetGap(1).SetBackground("#282a36").
		SetBindings(fmt.Sprintf(`[{"row":0,"col":0,"source_type":"nowplaying","source_id":%d}]`, np.ID)).
		SetEnabled(true).
		SaveX(srv.Ctx)
	mds := srv.WSHub.buildMatrixDS(load(), ml, 0)
	if mds == nil {
		t.Fatal("buildMatrixDS returned nil")
	}
	img, err := mds.GetPNG(30, 12)
	if err != nil {
		t.Fatalf("matrix render error: %v", err)
	}
	if img == nil || img.Format != "PNG" || len(img.Data) == 0 {
		t.Fatalf("matrix render = %+v", img)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("matrix PNG decode: %v", err)
	}
}

func TestNowPlayingPreviewHealthAndLKG(t *testing.T) {
	srv, client := newNowPlayingIntegrationServer(t)
	Health.Reset()
	defaultLKG = NewLKGCache(DefaultLKGCapacity)
	t.Cleanup(func() {
		Health.Reset()
		defaultLKG = NewLKGCache(DefaultLKGCapacity)
	})

	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer upstream.Close()

	np := client.NowPlayingSource.Create().
		SetName("NP").SetProvider("jellyfin").SetURL(upstream.URL).
		SetToken("tok").SetShowAlbumArt(false).
		SaveX(srv.Ctx)
	client.GeneralSettings.UpdateOneID(1).AddNowPlayingSources(np).SaveX(srv.Ctx)
	srv.Router.GET("/admin/preview", srv.AdminPreview)

	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/preview?type=nowplaying&id=%d&w=64&h=64", np.ID), nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w
	}

	if w := get(); w.Code != http.StatusOK {
		t.Fatalf("first preview: %d %s", w.Code, w.Body.String())
	}

	fail.Store(true)
	w := get()
	if w.Code != http.StatusOK {
		t.Fatalf("stale preview should be 200, got %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-LEDit-Stale") != "1" {
		t.Errorf("expected stale header, got %q", w.Header().Get("X-LEDit-Stale"))
	}

	key := fmt.Sprintf("nowplaying:%d", np.ID)
	sh, ok := Health.Snapshot()[key]
	if !ok {
		t.Fatalf("health key %q not recorded", key)
	}
	if StatusOf(sh) == "green" {
		t.Errorf("health should degrade after auth failure, got green")
	}
	if sh.ConsecutiveFails != 1 {
		t.Errorf("ConsecutiveFails = %d, want 1", sh.ConsecutiveFails)
	}
}
