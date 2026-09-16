package handlers

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/enttest"
	"ledit/ent/generalsettings"
	"ledit/render"
)

func newCompositionTestServer(t *testing.T) (*Server, *ent.Client) {
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

func clockRegions() string {
	return datasource.RegionsJSON([]datasource.Region{
		{ID: "left", Row: 0, Col: 0, SourceType: "clock", SourceID: 0},
	})
}

func TestCompositionFeedAndRegistryWiring(t *testing.T) {
	srv, client := newCompositionTestServer(t)

	enabled := client.Composition.Create().
		SetName("Main").SetMode("grid").SetRows(1).SetCols(2).
		SetRegions(clockRegions()).SetEnabled(true).SaveX(srv.Ctx)
	disabled := client.Composition.Create().
		SetName("Hidden").SetMode("grid").SetRows(1).SetCols(1).
		SetRegions(clockRegions()).SetEnabled(false).SaveX(srv.Ctx)
	if _, err := client.GeneralSettings.UpdateOneID(1).
		AddCompositions(enabled, disabled).Save(srv.Ctx); err != nil {
		t.Fatalf("add edges: %v", err)
	}

	gs, err := client.GeneralSettings.Query().Where(generalsettings.ID(1)).WithCompositions().Only(srv.Ctx)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	enabledKey := fmt.Sprintf("composition:%d", enabled.ID)
	disabledKey := fmt.Sprintf("composition:%d", disabled.ID)

	idx := buildSourceIndex(gs, srv.WSHub.aiConfig(srv.Ctx))
	if idx.byKey[enabledKey] == nil {
		t.Errorf("enabled composition %q missing from buildSourceIndex", enabledKey)
	}
	if idx.byKey[disabledKey] != nil {
		t.Errorf("disabled composition %q should not be indexed", disabledKey)
	}

	var feedKeys map[string]bool = map[string]bool{}
	for _, s := range srv.WSHub.loadSources(gs) {
		feedKeys[s.cacheKey] = true
	}
	if !feedKeys[enabledKey] {
		t.Errorf("enabled composition %q missing from feed", enabledKey)
	}
	if feedKeys[disabledKey] {
		t.Errorf("disabled composition %q should not be in feed", disabledKey)
	}

	// Binding options list both enabled and disabled (authoring catalog).
	rec := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(rec)
	ginCtx.Request = httptest.NewRequest("GET", "/admin/compositions/new", nil)
	opts := srv.bindingOptions(ginCtx)
	found := false
	for _, o := range opts["composition"] {
		if o.ID == enabled.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("bindingOptions[composition] missing enabled composition %d", enabled.ID)
	}

	// buildCompositorDS renders a valid frame through the registry.
	cds := srv.WSHub.buildCompositorDS(gs, enabled, 0)
	if cds == nil {
		t.Fatal("buildCompositorDS returned nil")
	}
	img, err := cds.GetPNG(64, 32)
	if err != nil {
		t.Fatalf("compositor GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode compositor frame: %v", err)
	}

	// Config change invalidates the last-known-good entry: a stored frame under
	// one signature is not served stale under a different signature.
	key := lkgCacheKey(fmt.Sprintf("composition:%d", enabled.ID), 64, 32)
	sig1 := datasourceConfigSig(cds)
	changed := *cds
	changed.Regions = []datasource.Region{{ID: "full", Row: 0, Col: 0, ColSpan: 2, SourceType: "clock", SourceID: 0}}
	sig2 := datasourceConfigSig(&changed)
	if sig1 == sig2 {
		t.Fatal("expected config signature to change when regions change")
	}
	if _, _, err := defaultLKG.GetPNG(key, sig1, func() (*render.RenderedImage, error) { return img, nil }); err != nil {
		t.Fatalf("seed LKG: %v", err)
	}
	if _, stale, err := defaultLKG.GetPNG(key, sig2, func() (*render.RenderedImage, error) { return nil, fmt.Errorf("render failed") }); err == nil || stale {
		t.Fatalf("expected miss (no stale serve) after config change, got stale=%v err=%v", stale, err)
	}

	// Overlay compositing stays a send-time concern applied on top of the frame.
	out, err := render.CompositeOverlayPNG(img.Data, render.OverlaySpec{
		Enabled: true, Position: "bottom", Height: 8, Text: "HI",
		Background: "#ff0000", Foreground: "#00ff00",
	}, time.Now())
	if err != nil {
		t.Fatalf("overlay composite: %v", err)
	}
	if bytes.Equal(out, img.Data) {
		t.Fatal("expected overlay to alter the composited frame")
	}
}
