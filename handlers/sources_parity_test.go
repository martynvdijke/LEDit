package handlers

import (
	"context"
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

func TestCatalogParity(t *testing.T) {
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
	w := client.Weather.Create().SetToken("tok").SetURL("http://w").SaveX(ctx)
	s := client.Sonarr.Create().SetToken("tok").SetURL("http://s").SaveX(ctx)
	r := client.Radarr.Create().SetToken("tok").SetURL("http://r").SaveX(ctx)
	f := client.F1.Create().SetToken("tok").SetURL("http://f").SaveX(ctx)
	ha := client.HomeAssistant.Create().SetToken("tok").SetURL("http://ha").SaveX(ctx)
	u := client.Untappd.Create().SetToken("tok").SetURL("http://u").SaveX(ctx)
	cr := client.Crypto.Create().SetToken("tok").SetURL("http://c").SaveX(ctx)
	st := client.Stock.Create().SetToken("tok").SetURL("http://st").SaveX(ctx)
	rs := client.RssFeed.Create().SetName("rss1").SetURL("http://rss").SaveX(ctx)
	cal := client.Calendar.Create().SetName("cal1").SetURL("http://cal").SaveX(ctx)
	ts := client.TextSlide.Create().SetContent("hello").SetColor("#fff").SetBgColor("#000").SetFontSize(12).SaveX(ctx)
	gc := client.GoogleCalendar.Create().SetName("gc1").SetURL("http://gc").SaveX(ctx)
	nf := client.NewsFeed.Create().SetName("news1").SetURL("http://news").SaveX(ctx)
	ga := client.GenericAPI.Create().SetToken("tok").SetURL("http://api").SetConfig(`{"title":"MyAPI"}`).SaveX(ctx)
	im := client.Image.Create().SetPath("/tmp/img.png").SaveX(ctx)
	vi := client.Video.Create().SetPath("/tmp/vid.mp4").SaveX(ctx)
	cd := client.Countdown.Create().SetName("cd1").SetLabel("lbl").SetTargetTime(time.Now().Add(24 * time.Hour)).SetEnabled(true).SaveX(ctx)
	ai := client.AIDigest.Create().SetName("ai1").SetPrompt("prompt").SetSources(`[]`).SetTTLMinutes(10).SetEnabled(true).SaveX(ctx)
	ml := client.MatrixLayout.Create().SetName("ml1").SetRows(2).SetCols(2).SetGap(1).SetBackground("#000").SetBindings(`[]`).SetEnabled(true).SaveX(ctx)
	client.GeneralSettings.UpdateOneID(gs.ID).AddWeatherIDs(w.ID).AddSonarrIDs(s.ID).AddRadarrIDs(r.ID).AddF1IDs(f.ID).AddHomeAssistantIDs(ha.ID).AddUntappdIDs(u.ID).AddCryptoIDs(cr.ID).AddStockIDs(st.ID).AddRssFeedIDs(rs.ID).AddCalendarIDs(cal.ID).AddTextSlideIDs(ts.ID).AddGoogleCalendarIDs(gc.ID).AddNewsFeedIDs(nf.ID).AddGenericAPIIDs(ga.ID).AddImageIDs(im.ID).AddVideoIDs(vi.ID).AddCountdownIDs(cd.ID).AddAiDigestIDs(ai.ID).AddMatrixLayoutIDs(ml.ID).ExecX(ctx)

	gs, err = client.GeneralSettings.Query().
		WithSonarr().WithRadarr().WithF1().WithWeather().WithHomeAssistant().WithUntappd().
		WithCrypto().WithStocks().WithRssFeeds().WithCalendars().WithTextSlides().
		WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCountdowns().WithAiDigests().
		WithImages().WithVideos().
		Only(ctx)
	if err != nil {
		t.Fatalf("load gs: %v", err)
	}
	hub := &WSHub{Client: client}
	sources := hub.loadSources(gs)
	idx := buildSourceIndex(gs, hub.aiConfig(ctx))

	sourceKeys := map[string]bool{}
	for _, s := range sources {
		sourceKeys[s.cacheKey] = true
	}
	indexKeys := map[string]bool{}
	for k := range idx.byKey {
		indexKeys[k] = true
	}

	// Documented exclusions:
	// - loadSources handles "matrix" layouts via buildMatrixDS (composite datasource),
	//   while buildSourceIndex deliberately does not index matrix layouts individually;
	//   matrix is resolved via a special closure in buildMatrixDS. We exclude matrix
	//   keys from parity comparison.
	// - pixelart is present in KnownSourceTypes but is not part of the LED feed
	//   (separate pixelart subsystem) and is not indexed by either catalog.
	// - countdown disabled filtering differs: loadSources skips disabled countdowns,
	//   buildSourceIndex includes all countdowns (feed-level filtering). This test
	//   creates only enabled countdowns so no delta; no exclusion needed.
	for k := range sourceKeys {
		if strings.HasPrefix(k, "matrix:") {
			delete(sourceKeys, k)
		}
		if strings.HasPrefix(k, "pixelart:") {
			delete(sourceKeys, k)
		}
	}
	for k := range indexKeys {
		if strings.HasPrefix(k, "matrix:") {
			delete(indexKeys, k)
		}
		if strings.HasPrefix(k, "pixelart:") {
			delete(indexKeys, k)
		}
	}

	for k := range sourceKeys {
		if !indexKeys[k] {
			t.Errorf("loadSources key %q missing in buildSourceIndex", k)
		}
	}
	for k := range indexKeys {
		if !sourceKeys[k] {
			t.Errorf("buildSourceIndex key %q missing in loadSources", k)
		}
	}
	expectedCore := []string{"clock:0", "systemstats:0", "analog-clock:0", "matrix-rain:0"}
	for _, ek := range expectedCore {
		if !sourceKeys[ek] {
			t.Errorf("expected core key %q missing in loadSources", ek)
		}
		if !indexKeys[ek] {
			t.Errorf("expected core key %q missing in buildSourceIndex", ek)
		}
	}
	_ = datasource.IsValidSourceType
}
