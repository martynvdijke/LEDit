package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/generalsettings"
)

// aiConfig loads the single-row AI settings as a datasource.AIConfig, or a
// zero config (rendering digests as placeholders) when not configured.
func (s *Server) aiConfig(ctx context.Context) datasource.AIConfig {
	ai, err := s.DB.AISettings.Query().Only(ctx)
	if err != nil {
		return datasource.AIConfig{}
	}
	return datasource.AIConfig{Provider: ai.Provider, Endpoint: ai.Endpoint, APIKey: ai.APIKey, Model: ai.Model}
}

// aiConfig is the WSHub variant of Server.aiConfig.
func (h *WSHub) aiConfig(ctx context.Context) datasource.AIConfig {
	ai, err := h.Client.AISettings.Query().Only(ctx)
	if err != nil {
		return datasource.AIConfig{}
	}
	return datasource.AIConfig{Provider: ai.Provider, Endpoint: ai.Endpoint, APIKey: ai.APIKey, Model: ai.Model}
}

// digestFeedURLs resolves referenced feed names (the JSON array on an AIDigest
// entity) to their configured URLs using the RSS and news feed edges. Names
// that match no configured feed are dropped.
func digestFeedURLs(settings *ent.GeneralSettings, names []string) []string {
	byName := map[string]string{}
	rss, _ := settings.Edges.RssFeedsOrErr()
	for _, r := range rss {
		byName[r.Name] = r.URL
	}
	news, _ := settings.Edges.NewsFeedsOrErr()
	for _, n := range news {
		byName[n.Name] = n.URL
	}
	var urls []string
	for _, n := range names {
		if u, ok := byName[n]; ok {
			urls = append(urls, u)
		}
	}
	return urls
}

// bindingOption is one selectable source in the matrix editor's per-cell
// selectors.
type bindingOption struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

// bindingOptions lists every configured source grouped by endpoint type so the
// matrix editor can offer per-cell selectors (including other matrix layouts).
func (s *Server) bindingOptions(c *gin.Context) map[string][]bindingOption {
	opts := map[string][]bindingOption{}
	settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).
		WithSonarr().WithRadarr().WithF1().WithWeather().WithHomeAssistant().WithUntappd().
		WithCrypto().WithStocks().WithRssFeeds().WithCalendars().WithTextSlides().
		WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCountdowns().WithAiDigests().WithTransits().WithUptimes().WithPiholes().WithGithubs().WithSports().WithSunmoons().WithJellyfins().WithQrcodes().Only(c.Request.Context())
	if err != nil || settings == nil {
		return opts
	}
	add := func(typ string, id int, label string) {
		// Flag sources currently in a red health state so the operator can
		// avoid wiring broken sources into matrix cells.
		if sh, ok := Health.Snapshot()[fmt.Sprintf("%s:%d", typ, id)]; ok && StatusOf(sh) == "red" {
			label += " ⚠"
		}
		opts[typ] = append(opts[typ], bindingOption{ID: id, Label: label})
	}

	// Built-in ambience modes are always bindable.
	add("clock", 0, "Clock")
	add("analog-clock", 0, "Analog Clock")
	add("matrix-rain", 0, "Matrix Rain")
	add("systemstats", 0, "System Stats")
	add("screensaver", 0, "Starfield")
	add("screensaver", 1, "DVD Bounce")
	add("screensaver", 2, "Matrix")
	add("screensaver", 3, "Plasma")

	sonarr, _ := settings.Edges.SonarrOrErr()
	for _, s := range sonarr {
		add("sonarr", s.ID, "Sonarr #"+strconv.Itoa(s.ID))
	}
	radarr, _ := settings.Edges.RadarrOrErr()
	for _, r := range radarr {
		add("radarr", r.ID, "Radarr #"+strconv.Itoa(r.ID))
	}
	f1s, _ := settings.Edges.F1OrErr()
	for _, f := range f1s {
		add("f1", f.ID, "F1 #"+strconv.Itoa(f.ID))
	}
	weather, _ := settings.Edges.WeatherOrErr()
	for _, w := range weather {
		add("weather", w.ID, "Weather #"+strconv.Itoa(w.ID))
	}
	ha, _ := settings.Edges.HomeAssistantOrErr()
	for _, h := range ha {
		add("homeassistant", h.ID, "HomeAssistant #"+strconv.Itoa(h.ID))
	}
	untappd, _ := settings.Edges.UntappdOrErr()
	for _, u := range untappd {
		add("untappd", u.ID, "Untappd #"+strconv.Itoa(u.ID))
	}
	crypto, _ := settings.Edges.CryptoOrErr()
	for _, cr := range crypto {
		add("crypto", cr.ID, "Crypto #"+strconv.Itoa(cr.ID))
	}
	stocks, _ := settings.Edges.StocksOrErr()
	for _, st := range stocks {
		add("stock", st.ID, "Stock #"+strconv.Itoa(st.ID))
	}
	rssFeeds, _ := settings.Edges.RssFeedsOrErr()
	for _, rs := range rssFeeds {
		add("rssfeed", rs.ID, "RSS: "+rs.Name)
	}
	calendars, _ := settings.Edges.CalendarsOrErr()
	for _, cl := range calendars {
		add("calendar", cl.ID, "Calendar: "+cl.Name)
	}
	textSlides, _ := settings.Edges.TextSlidesOrErr()
	for _, ts := range textSlides {
		add("textslides", ts.ID, "Text: "+truncateLabel(ts.Content, 20))
	}
	gcs, _ := settings.Edges.GoogleCalendarsOrErr()
	for _, gc := range gcs {
		add("googlecalendar", gc.ID, "Google Calendar: "+gc.Name)
	}
	newsFeeds, _ := settings.Edges.NewsFeedsOrErr()
	for _, nf := range newsFeeds {
		add("newsfeed", nf.ID, "News: "+nf.Name)
	}
	apis, _ := settings.Edges.GenericApisOrErr()
	for _, ga := range apis {
		label := datasource.GenericAPITitle(ga.Config)
		if label == "" {
			label = "Custom API #" + strconv.Itoa(ga.ID)
		}
		add("genericapi", ga.ID, "API: "+label)
	}
	layouts, _ := settings.Edges.MatrixLayoutsOrErr()
	for _, ml := range layouts {
		add("matrix", ml.ID, "Matrix: "+ml.Name)
	}
	countdowns, _ := settings.Edges.CountdownsOrErr()
	for _, cd := range countdowns {
		add("countdown", cd.ID, "Countdown: "+cd.Name)
	}
	digests, _ := settings.Edges.AiDigestsOrErr()
	for _, d := range digests {
		add("aidigest", d.ID, "AI: "+d.Name)
	}
	transits, _ := settings.Edges.TransitsOrErr()
	for _, t := range transits {
		add("transit", t.ID, "Transit #"+strconv.Itoa(t.ID))
	}
	uptimes, _ := settings.Edges.UptimesOrErr()
	for _, u := range uptimes {
		add("uptime", u.ID, "Uptime #"+strconv.Itoa(u.ID))
	}
	piholes, _ := settings.Edges.PiholesOrErr()
	for _, p := range piholes {
		add("pihole", p.ID, "Pi-hole #"+strconv.Itoa(p.ID))
	}
	githubs, _ := settings.Edges.GithubsOrErr()
	for _, g := range githubs {
		add("github", g.ID, "GitHub #"+strconv.Itoa(g.ID))
	}
	sportsItems, _ := settings.Edges.SportsOrErr()
	for _, sp := range sportsItems {
		add("sports", sp.ID, "Sports #"+strconv.Itoa(sp.ID))
	}
	sunmoons, _ := settings.Edges.SunmoonsOrErr()
	for _, sm := range sunmoons {
		add("sunmoon", sm.ID, "Sun/Moon #"+strconv.Itoa(sm.ID))
	}
	jellyfins, _ := settings.Edges.JellyfinsOrErr()
	for _, jf := range jellyfins {
		add("jellyfin", jf.ID, "Jellyfin #"+strconv.Itoa(jf.ID))
	}
	// Audio group — stylized visualizer, synced to tempo, not live FFT
	add("audio", 0, "Now Playing — Stylized visualizer — synced to tempo, not live FFT")
	add("audio", 1, "Audio Visualizer — Stylized visualizer — synced to tempo, not live FFT")
	qrcodes, _ := settings.Edges.QrcodesOrErr()
	for _, q := range qrcodes {
		label := q.Caption
		if label == "" {
			label = q.Content
		}
		add("qrcode", q.ID, "QR: "+truncateLabel(label, 20))
	}
	return opts
}

func truncateLabel(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// genericAPILabel returns the configured title of a Custom API source, or ""
// when unset.
func genericAPILabel(config string) string {
	return datasource.GenericAPITitle(config)
}

// sourceIndex is a lookup of every configured datasource keyed by
// "<endpoint>:<id>", shared by the feed (matrix nesting) and the on-demand
// preview endpoint so both resolve sources identically.
type sourceIndex struct {
	byKey map[string]datasource.Datasource
	names map[string]string
}

// buildSourceIndex indexes all datasource edges of GeneralSettings using the
// same endpoint keys as dsRegistry / admin routes. aiCfg supplies the LLM
// config for AI digest sources (zero config renders them as placeholders).
func buildSourceIndex(settings *ent.GeneralSettings, aiCfg datasource.AIConfig) *sourceIndex {
	idx := &sourceIndex{byKey: map[string]datasource.Datasource{}, names: map[string]string{}}
	key := func(typ string, id int) string { return fmt.Sprintf("%s:%d", typ, id) }

	// Built-in ambience modes (always available, no config).
	idx.byKey[key("clock", 0)] = &datasource.ClockDS{}
	idx.names[key("clock", 0)] = "Clock"
	idx.byKey[key("analog-clock", 0)] = &datasource.AnalogClockDS{}
	idx.names[key("analog-clock", 0)] = "Analog Clock"
	idx.byKey[key("matrix-rain", 0)] = &datasource.MatrixRainDS{}
	idx.names[key("matrix-rain", 0)] = "Matrix Rain"
	idx.byKey[key("systemstats", 0)] = &datasource.SystemStatsDS{}
	idx.names[key("systemstats", 0)] = "System Stats"
	for i, v := range []string{"starfield", "dvd", "matrix", "plasma"} {
		idx.byKey[key("screensaver", i)] = &datasource.ScreensaverDS{Variant: v}
		idx.names[key("screensaver", i)] = "Screensaver: " + v
	}

	sonarr, _ := settings.Edges.SonarrOrErr()
	for _, s := range sonarr {
		idx.byKey[key("sonarr", s.ID)] = &datasource.SonarrDS{Token: s.Token, URL: s.URL}
		idx.names[key("sonarr", s.ID)] = "Sonarr"
	}
	radarr, _ := settings.Edges.RadarrOrErr()
	for _, r := range radarr {
		idx.byKey[key("radarr", r.ID)] = &datasource.RadarrDS{Token: r.Token, URL: r.URL}
		idx.names[key("radarr", r.ID)] = "Radarr"
	}
	f1s, _ := settings.Edges.F1OrErr()
	for _, f := range f1s {
		idx.byKey[key("f1", f.ID)] = &datasource.F1DS{Token: f.Token, URL: f.URL}
		idx.names[key("f1", f.ID)] = "F1"
	}
	weather, _ := settings.Edges.WeatherOrErr()
	for _, w := range weather {
		idx.byKey[key("weather", w.ID)] = &datasource.WeatherDS{Token: w.Token, URL: w.URL}
		idx.names[key("weather", w.ID)] = "Weather"
	}
	ha, _ := settings.Edges.HomeAssistantOrErr()
	for _, h := range ha {
		idx.byKey[key("homeassistant", h.ID)] = &datasource.HomeAssistantDS{Token: h.Token, URL: h.URL}
		idx.names[key("homeassistant", h.ID)] = "HomeAssistant"
	}
	untappd, _ := settings.Edges.UntappdOrErr()
	for _, u := range untappd {
		idx.byKey[key("untappd", u.ID)] = &datasource.UntappdDS{Token: u.Token, URL: u.URL}
		idx.names[key("untappd", u.ID)] = "Untappd"
	}
	images, _ := settings.Edges.ImagesOrErr()
	for _, img := range images {
		idx.byKey[key("images", img.ID)] = &datasource.ImageDS{Path: img.Path}
		idx.names[key("images", img.ID)] = "Image"
	}
	videos, _ := settings.Edges.VideosOrErr()
	for _, vid := range videos {
		idx.byKey[key("videos", vid.ID)] = &datasource.VideoDS{Path: vid.Path}
		idx.names[key("videos", vid.ID)] = "Video"
	}
	crypto, _ := settings.Edges.CryptoOrErr()
	for _, cr := range crypto {
		idx.byKey[key("crypto", cr.ID)] = &datasource.CryptoDS{Token: cr.Token, URL: cr.URL}
		idx.names[key("crypto", cr.ID)] = "Crypto"
	}
	stocks, _ := settings.Edges.StocksOrErr()
	for _, st := range stocks {
		idx.byKey[key("stock", st.ID)] = &datasource.StockDS{Token: st.Token, URL: st.URL}
		idx.names[key("stock", st.ID)] = "Stock"
	}
	rssFeeds, _ := settings.Edges.RssFeedsOrErr()
	for _, rs := range rssFeeds {
		idx.byKey[key("rssfeed", rs.ID)] = &datasource.RssFeedDS{URL: rs.URL, Name: rs.Name}
		idx.names[key("rssfeed", rs.ID)] = "RSS: " + rs.Name
	}
	calendars, _ := settings.Edges.CalendarsOrErr()
	for _, cl := range calendars {
		idx.byKey[key("calendar", cl.ID)] = &datasource.CalendarDS{URL: cl.URL, Name: cl.Name}
		idx.names[key("calendar", cl.ID)] = "Calendar: " + cl.Name
	}
	textSlides, _ := settings.Edges.TextSlidesOrErr()
	for _, ts := range textSlides {
		idx.byKey[key("textslides", ts.ID)] = &datasource.TextSlideDS{Content: ts.Content, Color: ts.Color, BgColor: ts.BgColor, FontSize: ts.FontSize}
		idx.names[key("textslides", ts.ID)] = "Text: " + ts.Content
	}
	gcs, _ := settings.Edges.GoogleCalendarsOrErr()
	for _, gc := range gcs {
		idx.byKey[key("googlecalendar", gc.ID)] = &datasource.GoogleCalendarDS{URL: gc.URL, Name: gc.Name}
		idx.names[key("googlecalendar", gc.ID)] = "Google Calendar: " + gc.Name
	}
	newsFeeds, _ := settings.Edges.NewsFeedsOrErr()
	for _, nf := range newsFeeds {
		idx.byKey[key("newsfeed", nf.ID)] = &datasource.NewsDS{URL: nf.URL, Name: nf.Name}
		idx.names[key("newsfeed", nf.ID)] = "News: " + nf.Name
	}
	apis, _ := settings.Edges.GenericApisOrErr()
	for _, ga := range apis {
		label := datasource.GenericAPITitle(ga.Config)
		if label == "" {
			label = "Custom API"
		}
		idx.byKey[key("genericapi", ga.ID)] = &datasource.GenericAPIDS{Token: ga.Token, URL: ga.URL, Config: ga.Config}
		idx.names[key("genericapi", ga.ID)] = "API: " + label
	}
	countdowns, _ := settings.Edges.CountdownsOrErr()
	for _, cd := range countdowns {
		idx.byKey[key("countdown", cd.ID)] = &datasource.CountdownDS{Name: cd.Name, Label: cd.Label, Target: cd.TargetTime}
		idx.names[key("countdown", cd.ID)] = "Countdown: " + cd.Name
	}
	digests, _ := settings.Edges.AiDigestsOrErr()
	for _, d := range digests {
		if !d.Enabled {
			continue
		}
		idx.byKey[key("aidigest", d.ID)] = &datasource.AIDigestDS{
			ID:       d.ID,
			Name:     d.Name,
			Prompt:   d.Prompt,
			FeedURLs: digestFeedURLs(settings, datasource.ParseDigestSources(d.Sources)),
			TTL:      time.Duration(d.TTLMinutes) * time.Minute,
			Config:   aiCfg,
		}
		idx.names[key("aidigest", d.ID)] = "AI: " + d.Name
	}
	transits, _ := settings.Edges.TransitsOrErr()
	for _, t := range transits {
		idx.byKey[key("transit", t.ID)] = &datasource.TransitDS{Token: t.Token, URL: t.URL}
		idx.names[key("transit", t.ID)] = "Transit"
	}
	uptimes, _ := settings.Edges.UptimesOrErr()
	for _, u := range uptimes {
		idx.byKey[key("uptime", u.ID)] = &datasource.UptimeDS{URL: u.URL, Config: u.Config}
		idx.names[key("uptime", u.ID)] = "Uptime"
	}
	piholes, _ := settings.Edges.PiholesOrErr()
	for _, p := range piholes {
		idx.byKey[key("pihole", p.ID)] = &datasource.PiHoleDS{Token: p.Token, URL: p.URL}
		idx.names[key("pihole", p.ID)] = "Pi-hole"
	}
	githubs, _ := settings.Edges.GithubsOrErr()
	for _, g := range githubs {
		idx.byKey[key("github", g.ID)] = &datasource.GitHubDS{Token: g.Token, URL: g.URL}
		idx.names[key("github", g.ID)] = "GitHub"
	}
	sportsItems, _ := settings.Edges.SportsOrErr()
	for _, sp := range sportsItems {
		idx.byKey[key("sports", sp.ID)] = &datasource.SportsDS{Token: sp.Token, URL: sp.URL}
		idx.names[key("sports", sp.ID)] = "Sports"
	}
	sunmoons, _ := settings.Edges.SunmoonsOrErr()
	for _, sm := range sunmoons {
		idx.byKey[key("sunmoon", sm.ID)] = &datasource.SunMoonDS{Token: sm.Token, URL: sm.URL}
		idx.names[key("sunmoon", sm.ID)] = "Sun/Moon"
	}
	jellyfins, _ := settings.Edges.JellyfinsOrErr()
	for _, jf := range jellyfins {
		idx.byKey[key("jellyfin", jf.ID)] = &datasource.JellyfinDS{Token: jf.Token, URL: jf.URL}
		idx.names[key("jellyfin", jf.ID)] = "Jellyfin"
	}
	qrcodes, _ := settings.Edges.QrcodesOrErr()
	for _, q := range qrcodes {
		idx.byKey[key("qrcode", q.ID)] = &datasource.QRSource{
			Content: q.Content, Mode: string(q.Mode), WifiSSID: q.WifiSsid, WifiPassword: q.WifiPassword, WifiAuth: string(q.WifiAuth), Caption: q.Caption, ErrorCorrection: string(q.ErrorCorrection), QuietZone: q.QuietZone,
		}
		idx.names[key("qrcode", q.ID)] = "QR Code"
	}
	idx.byKey[key("audio", 0)] = &datasource.AudioNowPlayingDS{}
	idx.names[key("audio", 0)] = "Now Playing"
	idx.byKey[key("audio", 1)] = &datasource.VisualizerDS{Mode: "bars"}
	idx.names[key("audio", 1)] = "Audio Visualizer"
	return idx
}

// Resolve looks up a datasource by endpoint type and DB id.
func (idx *sourceIndex) Resolve(sourceType string, sourceID int) (datasource.Datasource, string, error) {
	if idx == nil {
		return nil, "", fmt.Errorf("no sources indexed")
	}
	key := fmt.Sprintf("%s:%d", sourceType, sourceID)
	src, ok := idx.byKey[key]
	if !ok {
		return nil, "", fmt.Errorf("datasource %q not found", key)
	}
	return src, idx.names[key], nil
}

// buildMatrixDS constructs a MatrixDS for a layout whose bindings resolve
// against the current source index. Nested "matrix" bindings recurse with a
// depth cap to guard against cycles. Returns nil when the layout is unusable.
func (h *WSHub) buildMatrixDS(settings *ent.GeneralSettings, ml *ent.MatrixLayout, depth int) *datasource.MatrixDS {
	if depth > 2 {
		slog.Warn("matrix nesting too deep, skipping layout", "layout", ml.Name, "depth", depth)
		return nil
	}
	idx := buildSourceIndex(settings, h.aiConfig(context.Background()))
	mds := &datasource.MatrixDS{
		Name:       ml.Name,
		Rows:       ml.Rows,
		Cols:       ml.Cols,
		Gap:        ml.Gap,
		Background: ml.Background,
		Bindings:   datasource.ParseBindings(ml.Bindings),
		Depth:      depth,
	}
	mds.Resolve = func(sourceType string, sourceID int) (datasource.Datasource, string, error) {
		if sourceType == "matrix" {
			nested, err := h.Client.MatrixLayout.Get(context.Background(), sourceID)
			if err != nil {
				return nil, "", err
			}
			inner := h.buildMatrixDS(settings, nested, depth+1)
			if inner == nil {
				return nil, "", fmt.Errorf("matrix nesting too deep")
			}
			return inner, nested.Name, nil
		}
		return idx.Resolve(sourceType, sourceID)
	}
	return mds
}
