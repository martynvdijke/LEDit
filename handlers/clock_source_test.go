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
)

func TestClockResolve(t *testing.T) {
	dummy := &ent.GeneralSettings{}
	idx := buildSourceIndex(dummy, datasource.AIConfig{})

	src, name, err := idx.Resolve("clock", 0)
	if err != nil {
		t.Fatalf("Resolve(clock,0) error: %v", err)
	}
	if name != "Clock" {
		t.Fatalf("expected name Clock, got %q", name)
	}
	if _, ok := src.(*datasource.ClockDS); !ok {
		t.Fatalf("expected *datasource.ClockDS, got %T", src)
	}

	_, _, err = idx.Resolve("clock", 7)
	if err == nil {
		t.Fatalf("expected error for clock:7, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}

	_, _, err = idx.Resolve("clock", 5)
	if err == nil {
		t.Fatalf("expected error for clock:5, got nil")
	}
}

func TestLoadSourcesIncludesClock(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	defer client.Close()
	ctx := context.Background()
	gs, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx)
	if err != nil {
		t.Fatalf("seed gs: %v", err)
	}
	gs, err = client.GeneralSettings.Query().WithSonarr().WithRadarr().WithF1().WithWeather().WithHomeAssistant().WithUntappd().WithCrypto().WithStocks().WithRssFeeds().WithCalendars().WithTextSlides().WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCountdowns().WithAiDigests().WithImages().WithVideos().Only(ctx)
	if err != nil {
		t.Fatalf("load gs: %v", err)
	}
	hub := &WSHub{Client: client}
	sources := hub.loadSources(gs)
	found := false
	var foundName string
	for _, s := range sources {
		if s.cacheKey == "clock:0" {
			found = true
			foundName = s.Name
			if _, ok := s.Source.(*datasource.ClockDS); !ok {
				t.Fatalf("expected *ClockDS for clock:0, got %T", s.Source)
			}
			break
		}
	}
	if !found {
		t.Fatalf("loadSources missing clock:0; keys: %v", func() []string {
			var ks []string
			for _, s := range sources {
				ks = append(ks, s.cacheKey)
			}
			return ks
		}())
	}
	if foundName != "Clock" {
		t.Fatalf("expected name Clock, got %q", foundName)
	}
	// Ensure clock is first (prepend)
	if len(sources) > 0 && sources[0].cacheKey != "clock:0" {
		t.Fatalf("expected clock:0 first in loadSources, got %q", sources[0].cacheKey)
	}
}

func TestBindingOptionsIncludesClock(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	defer client.Close()
	ctx := context.Background()
	gs, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx)
	if err != nil {
		t.Fatalf("seed gs: %v", err)
	}
	_ = gs
	srv := &Server{DB: client}
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	ginCtx.Request = req
	opts := srv.bindingOptions(ginCtx)
	clockOpts, ok := opts["clock"]
	if !ok || len(clockOpts) == 0 {
		t.Fatalf("bindingOptions missing clock entry: %v", opts)
	}
	found := false
	for _, o := range clockOpts {
		if o.ID == 0 && o.Label == "Clock" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("clock binding option not found or mismatched: %v", clockOpts)
	}
}

func TestClockDSGetPNG(t *testing.T) {
	ds := &datasource.ClockDS{}
	start := time.Now()
	img, err := ds.GetPNG(64, 64)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("GetPNG error: %v", err)
	}
	if img == nil || len(img.Data) == 0 {
		t.Fatalf("empty image data")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("GetPNG took too long: %v", elapsed)
	}
	// Decodable PNG
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("png decode failed: %v", err)
	}
	if img.Format != "PNG" && img.Format != "" {
		// allow empty but ideally PNG
	}
}
