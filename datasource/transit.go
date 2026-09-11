package datasource

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"ledit/render"
)

// TransitDeparture is the normalized departure shape every provider adapter
// produces. Time carries its embedded offset.
type TransitDeparture struct {
	Line        string
	Destination string
	Time        time.Time
}

// TransitConfig is the runtime configuration derived from a Transit entity.
// The zero-valued provider/time/mode fields are normalized in TransitDS.config.
type TransitConfig struct {
	StopID        string
	URL           string
	APIKey        string
	Provider      string
	MaxDepartures int
	RouteFilter   string
	WalkTimeMin   int
	Timezone      string
	TimeMode      string
}

// TransitDS renders upcoming departures for one stop. Token is the legacy
// stop-id field name.
type TransitDS struct {
	Token         string
	URL           string
	APIKey        string
	Provider      string
	MaxDepartures int
	RouteFilter   string
	WalkTimeMin   int
	Timezone      string
	TimeMode      string

	// now is injected for determinism in tests; defaults to time.Now.
	now func() time.Time
}

func (t *TransitDS) nowTime() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// config normalizes the datasource fields, applying the documented defaults.
func (t *TransitDS) config() TransitConfig {
	provider := strings.TrimSpace(t.Provider)
	if provider == "" {
		provider = "vbb"
	}
	max := t.MaxDepartures
	if max < 1 || max > 8 {
		max = 4
	}
	walk := t.WalkTimeMin
	if walk < 0 {
		walk = 0
	}
	tz := strings.TrimSpace(t.Timezone)
	if tz == "" {
		tz = "Europe/Berlin"
	}
	mode := strings.TrimSpace(t.TimeMode)
	if mode != "clock" {
		mode = "minutes"
	}
	return TransitConfig{
		StopID:        t.Token,
		URL:           t.URL,
		APIKey:        t.APIKey,
		Provider:      provider,
		MaxDepartures: max,
		RouteFilter:   t.RouteFilter,
		WalkTimeMin:   walk,
		Timezone:      tz,
		TimeMode:      mode,
	}
}

// providerDefaultURL is the endpoint used when no URL is configured.
func providerDefaultURL(provider string) string {
	switch provider {
	case "transitland":
		return "https://transit.land/api/v2/rest/stops/%s/departures"
	case "511":
		return "https://api.511.org/transit/StopMonitoring?MonitoringRef=%s"
	case "vbb":
		return "https://v6.vbb.transport.rest/stops/%s/departures"
	default:
		return ""
	}
}

// resolveTransitURL selects the endpoint and substitutes %s with the stop id
// when present; otherwise the URL is used verbatim.
func resolveTransitURL(cfg TransitConfig) (string, error) {
	u := strings.TrimSpace(cfg.URL)
	if u == "" {
		u = providerDefaultURL(cfg.Provider)
	}
	if u == "" {
		return "", fmt.Errorf("no departures URL configured")
	}
	return strings.ReplaceAll(u, "%s", cfg.StopID), nil
}

func (t *TransitDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	data, fallback := t.transitRenderData(t.config(), t.nowTime())
	if fallback != "" {
		return fallbackTransit(width, height, fallback), nil
	}
	img, err := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	slog.Info("transit data rendered", "source", "transit", "rows", len(data)-1)
	return img, nil
}

// transitRenderData fetches and formats upcoming departures. It returns the
// RenderDict payload on success, or a non-empty fallback message ("unavailable"
// or "no departures") when the feed should degrade gracefully. Splitting this
// from GetPNG keeps the fallback-state choice directly testable.
func (t *TransitDS) transitRenderData(cfg TransitConfig, now time.Time) (map[string]string, string) {
	rawURL, err := resolveTransitURL(cfg)
	if err != nil {
		slog.Warn("transit URL not configured, using fallback", "source", "transit", "provider", cfg.Provider)
		return nil, "unavailable"
	}

	slog.Info("fetching transit data", "source", "transit", "provider", cfg.Provider, "stop", cfg.StopID)
	body, err := t.fetch(rawURL, cfg)
	if err != nil {
		slog.Warn("transit API call failed, using fallback", "source", "transit", "provider", cfg.Provider, "error", err)
		return nil, "unavailable"
	}

	departures, err := ParseTransitDepartures(body, cfg.Provider)
	if err != nil {
		slog.Warn("transit parse failed, using fallback", "source", "transit", "error", err)
		return nil, "unavailable"
	}

	rows, err := BuildTransitRows(departures, cfg, now)
	if err != nil {
		slog.Warn("transit timing failed, using fallback", "source", "transit", "error", err)
		return nil, "unavailable"
	}
	if len(rows) == 0 {
		slog.Info("transit no upcoming departures, using fallback", "source", "transit")
		return nil, "no departures"
	}

	data := map[string]string{"title": "TRANSIT"}
	for i, r := range rows {
		data[fmt.Sprintf("r%d", i+1)] = strings.TrimSpace(r[0] + " " + r[1])
	}
	return data, ""
}

// fetch sends the request, carrying the API key as a header for most providers
// or as a query parameter for those that require it. The key is never logged.
func (t *TransitDS) fetch(rawURL string, cfg TransitConfig) ([]byte, error) {
	if cfg.APIKey == "" {
		return apiGet(rawURL, "", nil)
	}
	switch cfg.Provider {
	case "511":
		return apiGet(withQuery(rawURL, "api_key", cfg.APIKey), "", nil)
	case "transitland":
		return apiGet(withQuery(rawURL, "apikey", cfg.APIKey), "", nil)
	default:
		return apiGet(rawURL, cfg.APIKey, nil)
	}
}

func withQuery(rawURL, key, value string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}

func fallbackTransit(width, height int, message string) *render.RenderedImage {
	data := map[string]string{
		"title": "TRANSIT",
		"r1":    message,
	}
	img, _ := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}

// ParseTransitDepartures decodes an agency JSON response into the normalized
// departure shape. Malformed JSON returns an error; an unrecognized (but
// valid) shape yields no departures so the caller renders a no-departures
// fallback rather than breaking the feed.
func ParseTransitDepartures(body []byte, provider string) ([]TransitDeparture, error) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("transit response is not valid JSON: %w", err)
	}
	items := findDepartureItems(root, provider)
	departures := make([]TransitDeparture, 0, len(items))
	for _, m := range items {
		if d, ok := extractDeparture(m); ok {
			departures = append(departures, d)
		}
	}
	return departures, nil
}

// findDepartureItems locates the departure objects in a provider response.
func findDepartureItems(root any, provider string) []map[string]any {
	switch provider {
	case "transitland":
		if stops := asSlice(root, "stops"); stops != nil {
			var out []map[string]any
			for _, s := range stops {
				out = append(out, asMapSlice(asMap(s)["departures"])...)
			}
			return out
		}
		return asMapSlice(asSlice(root, "departures"))
	case "511":
		sd := asMap(root)
		if sd == nil {
			return nil
		}
		deliveries := asMapSlice(asMap(sd["ServiceDelivery"])["StopMonitoringDelivery"])
		var out []map[string]any
		for _, d := range deliveries {
			for _, v := range asMapSlice(d["MonitoredStopVisit"]) {
				if mvj := asMap(v["MonitoredVehicleJourney"]); mvj != nil {
					out = append(out, mvj)
				}
			}
		}
		return out
	default:
		if arr := asSlice(root, "departures"); arr != nil {
			return asMapSlice(arr)
		}
		if arr, ok := root.([]any); ok {
			return asMapSlice(arr)
		}
		return nil
	}
}

// extractDeparture normalizes one provider-specific departure object, skipping
// rows whose line/destination/time cannot be recovered.
func extractDeparture(m map[string]any) (TransitDeparture, bool) {
	line := firstText(m, "line", "route", "route_short_name", "route_long_name", "line_name", "LineRef", "PublishedLineName")
	dest := firstText(m, "destination", "destination_name", "headsign", "trip_headsign", "stop_headsign", "direction", "DestinationName")
	if dest == "" {
		dest = firstText(asMap(m["trip"]), "trip_headsign", "headsign", "destination", "direction")
	}
	if dest == "" {
		dest = firstText(asMap(m["MonitoredCall"]), "DestinationDisplay", "DestinationName", "StopPointName")
	}
	tm, ok := firstTime(m, "when", "plannedWhen", "departure_time", "departure", "time",
		"expected_departure", "ExpectedDepartureTime", "AimedDepartureTime", "best_departure_estimate")
	if !ok {
		tm, ok = firstTime(asMap(m["MonitoredCall"]), "ExpectedDepartureTime", "AimedDepartureTime", "ExpectedArrivalTime", "AimedArrivalTime")
	}
	if !ok {
		return TransitDeparture{}, false
	}
	return TransitDeparture{Line: line, Destination: dest, Time: tm}, true
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asMapSlice(v any) []map[string]any {
	arr, _ := v.([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		if m := asMap(e); m != nil {
			out = append(out, m)
		}
	}
	return out
}

func asSlice(root any, key string) []any {
	m := asMap(root)
	if m == nil {
		return nil
	}
	arr, _ := m[key].([]any)
	return arr
}

// firstText returns the first non-empty string found at keys, resolving nested
// objects (e.g. VBB line.name or SIRI {"value": ...}) to their text.
func firstText(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := textValue(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func textValue(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]any:
		return firstText(t, "name", "short_name", "shortName", "long_name", "longName",
			"route_short_name", "route_long_name", "text", "value", "LineName")
	}
	return ""
}

func firstTime(m map[string]any, keys ...string) (time.Time, bool) {
	if m == nil {
		return time.Time{}, false
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if t, ok := parseTransitAnyTime(v); ok {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func parseTransitAnyTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case string:
		tm, err := parseTransitTime(t)
		return tm, err == nil
	case float64:
		return unixToTime(t)
	case map[string]any:
		for _, k := range []string{"estimated", "expected", "actual", "scheduled", "aimed", "value", "time"} {
			if tv, ok := t[k]; ok {
				if tm, ok := parseTransitAnyTime(tv); ok {
					return tm, true
				}
			}
		}
	}
	return time.Time{}, false
}

func unixToTime(f float64) (time.Time, bool) {
	if f <= 0 {
		return time.Time{}, false
	}
	sec := int64(f)
	if sec > 1e12 {
		return time.UnixMilli(sec), true
	}
	return time.Unix(sec, 0), true
}

// BuildTransitRows filters, sorts, caps, and formats departures relative to the
// agency timezone and the configured walk-time offset. It is pure with an
// injected now so timezone/offset behavior is deterministic in tests.
// Each returned row is { "LINE DEST" (<=28 chars), timing }.
func BuildTransitRows(departures []TransitDeparture, cfg TransitConfig, now time.Time) ([][2]string, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", cfg.Timezone, err)
	}

	max := cfg.MaxDepartures
	if max < 1 || max > 8 {
		max = 4
	}
	effectiveNow := now.Add(time.Duration(cfg.WalkTimeMin) * time.Minute)
	filter := parseRouteFilter(cfg.RouteFilter)

	future := make([]TransitDeparture, 0, len(departures))
	for _, d := range departures {
		if !d.Time.After(effectiveNow) {
			continue
		}
		if len(filter) > 0 && !filter[strings.ToLower(strings.TrimSpace(d.Line))] {
			continue
		}
		future = append(future, d)
	}
	sort.SliceStable(future, func(i, j int) bool { return future[i].Time.Before(future[j].Time) })
	if len(future) > max {
		future = future[:max]
	}

	rows := make([][2]string, 0, len(future))
	for _, d := range future {
		label := strings.TrimSpace(strings.TrimSpace(d.Line) + " " + strings.TrimSpace(d.Destination))
		if label == "" {
			label = "?"
		}
		label = truncateRunes(label, 28)

		var timing string
		if cfg.TimeMode == "clock" {
			timing = d.Time.In(loc).Format("15:04")
		} else {
			mins := int(math.Round(d.Time.Sub(effectiveNow).Minutes()))
			if mins <= 1 {
				timing = "NOW"
			} else {
				timing = fmt.Sprintf("%d min", mins)
			}
		}
		rows = append(rows, [2]string{label, timing})
	}
	return rows, nil
}

// parseRouteFilter splits a comma-separated filter into a lowercase set.
func parseRouteFilter(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func parseTransitTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
