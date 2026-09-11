package handlers

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/http"
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
)

func newTransitIntegrationServer(t *testing.T) (*Server, *ent.Client) {
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

func TestTransitIntegrationFeedPlaylistMatrix(t *testing.T) {
	srv, client := newTransitIntegrationServer(t)

	dep := func(line, dest string, in time.Duration) string {
		return fmt.Sprintf(`{"line":%q,"destination":%q,"time":%q}`, line, dest, time.Now().Add(in).UTC().Format(time.RFC3339))
	}
	body := fmt.Sprintf(`{"departures":[%s,%s,%s]}`,
		dep("S7", "Potsdam", 5*time.Minute),
		dep("U2", "Ruhleben", 6*time.Minute),
		dep("U1", "Warschauer", 20*time.Minute),
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer upstream.Close()

	tr := client.Transit.Create().
		SetToken("900000003201").
		SetURL(upstream.URL + "/%s").
		SetProvider("custom").
		SetMaxDepartures(4).
		SetRouteFilter("S7, U1").
		SetWalkTimeMin(3).
		SetTimezone("UTC").
		SetTimeMode("minutes").
		SaveX(srv.Ctx)
	if _, err := client.GeneralSettings.UpdateOneID(1).AddTransits(tr).Save(srv.Ctx); err != nil {
		t.Fatalf("add edge: %v", err)
	}
	wantKey := fmt.Sprintf("transit:%d", tr.ID)

	load := func() *ent.GeneralSettings {
		t.Helper()
		gs, err := client.GeneralSettings.Query().Where(generalsettings.ID(1)).WithTransits().WithMatrixLayouts().Only(srv.Ctx)
		if err != nil {
			t.Fatalf("load settings: %v", err)
		}
		return gs
	}

	// Global feed carries the configured fields.
	var feedSrc datasource.Datasource
	for _, s := range srv.WSHub.loadSources(load()) {
		if s.cacheKey == wantKey {
			feedSrc = s.Source
		}
	}
	tds, ok := feedSrc.(*datasource.TransitDS)
	if !ok {
		t.Fatal("transit source not found in global feed")
	}
	if tds.Provider != "custom" || tds.Token != "900000003201" || tds.RouteFilter != "S7, U1" || tds.WalkTimeMin != 3 {
		t.Errorf("feed transit options = %+v", tds)
	}

	// Device playlist resolves the transit source in authored order.
	pl := client.Playlist.Create().
		SetName("transit playlist").
		SetItems(fmt.Sprintf(`[{"source_type":"transit","source_id":%d}]`, tr.ID)).
		SetEnabled(true).
		SaveX(srv.Ctx)
	dev := client.DeviceSettings.Create().
		SetName("Transit Device").SetToken("transit-token").SetEnabled(true).
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
		t.Error("playlist feed should include the transit source")
	}

	// Matrix cell binding renders the transit PNG (route filter drops U2).
	ml := client.MatrixLayout.Create().
		SetName("Transit grid").SetRows(1).SetCols(2).SetGap(1).SetBackground("#282a36").
		SetBindings(fmt.Sprintf(`[{"row":0,"col":0,"source_type":"transit","source_id":%d}]`, tr.ID)).
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

func TestTransitPreviewRendersPNG(t *testing.T) {
	srv, client := newTransitIntegrationServer(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"departures":[{"line":"S7","destination":"Potsdam","time":"`+time.Now().Add(5*time.Minute).UTC().Format(time.RFC3339)+`"}]}`)
	}))
	defer upstream.Close()

	tr := client.Transit.Create().
		SetToken("900000003201").SetURL(upstream.URL).SetProvider("custom").
		SetTimezone("UTC").SetMaxDepartures(4).SetTimeMode("minutes").
		SaveX(srv.Ctx)
	client.GeneralSettings.UpdateOneID(1).AddTransits(tr).SaveX(srv.Ctx)
	srv.Router.GET("/admin/preview", srv.AdminPreview)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/preview?type=transit&id=%d&w=64&h=64", tr.ID), nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatalf("content-type = %q", ct)
	}
	if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Fatalf("preview PNG decode: %v", err)
	}
}
