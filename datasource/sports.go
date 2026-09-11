package datasource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ledit/render"
)

// Game states returned by a provider.
const (
	GameScheduled = "scheduled"
	GameLive      = "live"
	GameFinal     = "final"
)

const (
	defaultESPNURL = "https://site.api.espn.com/apis/site/v2/sports/%s/scoreboard"

	sportsDefaultMaxGames    = 4
	sportsMaxGames           = 8
	sportsDefaultLiveRefresh = 30
	sportsMinLiveRefresh     = 15
	sportsDefaultIdleRefresh = 300
	sportsMinIdleRefresh     = 60
)

// Game is the provider-independent normalized scoreboard entry.
type Game struct {
	ID        string
	League    string
	Start     time.Time
	Home      string
	Away      string
	HomeAbbr  string
	AwayAbbr  string
	HomeScore int
	AwayScore int
	State     string // GameScheduled | GameLive | GameFinal
	Period    string
	Clock     string
}

// SportsBoardConfig is the JSON `config` field: followed leagues/teams/fixtures
// plus board display preferences.
type SportsBoardConfig struct {
	Leagues    []string `json:"leagues"`
	Teams      []string `json:"teams"`
	Fixtures   []string `json:"fixtures"`
	MaxGames   int      `json:"max_games"`
	PreferLive bool     `json:"prefer_live"`
}

// SportsConfig is the runtime configuration derived from a Sports entity.
type SportsConfig struct {
	Provider           string
	Token              string
	URL                string
	Leagues            []string
	Teams              []string
	Fixtures           []string
	MaxGames           int
	PreferLive         bool
	LiveRefreshSeconds int
	IdleRefreshSeconds int
}

// SportsDS renders live/upcoming scores for followed leagues, teams, and
// fixtures. Token carries the provider API key; for legacy ESPN rows it also
// doubles as a single league slug when no leagues are configured. URL may
// contain %s substituted with the league slug.
type SportsDS struct {
	Token              string
	URL                string
	Provider           string
	Config             string
	LiveRefreshSeconds int
	IdleRefreshSeconds int
}

func sportsURL(base, league string) string {
	if strings.Contains(base, "%s") {
		return fmt.Sprintf(base, league)
	}
	return base
}

// ParseSportsConfig decodes the config JSON tolerantly: invalid JSON yields
// defaults rather than an error (admin validation rejects it up front).
func ParseSportsConfig(raw string) SportsBoardConfig {
	cfg := SportsBoardConfig{MaxGames: sportsDefaultMaxGames, PreferLive: true}
	trim := strings.TrimSpace(raw)
	if trim == "" {
		return cfg
	}
	var parsed struct {
		Leagues    []string `json:"leagues"`
		Teams      []string `json:"teams"`
		Fixtures   []string `json:"fixtures"`
		MaxGames   *int     `json:"max_games"`
		PreferLive *bool    `json:"prefer_live"`
	}
	if err := json.Unmarshal([]byte(trim), &parsed); err != nil {
		slog.Warn("invalid sports config, using defaults", "source", "sports", "error", err)
		return cfg
	}
	cfg.Leagues = cleanList(parsed.Leagues)
	cfg.Teams = cleanList(parsed.Teams)
	cfg.Fixtures = cleanList(parsed.Fixtures)
	if parsed.MaxGames != nil && *parsed.MaxGames > 0 {
		cfg.MaxGames = *parsed.MaxGames
	}
	if cfg.MaxGames > sportsMaxGames {
		cfg.MaxGames = sportsMaxGames
	}
	if parsed.PreferLive != nil {
		cfg.PreferLive = *parsed.PreferLive
	}
	return cfg
}

// ValidateSportsConfig enforces the admin/API schema for the config field.
func ValidateSportsConfig(raw string) error {
	trim := strings.TrimSpace(raw)
	if trim == "" {
		return fmt.Errorf("config must be a JSON object listing leagues, teams, or fixtures")
	}
	var parsed struct {
		Leagues  []string `json:"leagues"`
		Teams    []string `json:"teams"`
		Fixtures []string `json:"fixtures"`
		MaxGames *int     `json:"max_games"`
	}
	if err := json.Unmarshal([]byte(trim), &parsed); err != nil {
		return fmt.Errorf("config must be valid JSON: %v", err)
	}
	if len(cleanList(parsed.Leagues)) == 0 && len(cleanList(parsed.Teams)) == 0 && len(cleanList(parsed.Fixtures)) == 0 {
		return fmt.Errorf("config must list at least one league, team, or fixture")
	}
	if parsed.MaxGames != nil && (*parsed.MaxGames < 1 || *parsed.MaxGames > sportsMaxGames) {
		return fmt.Errorf("config max_games must be between 1 and %d", sportsMaxGames)
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func cleanList(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		v := strings.TrimSpace(s)
		if v == "" {
			continue
		}
		key := strings.ToLower(v)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	return out
}

// config normalizes the datasource fields, applying the documented defaults
// and refresh floors.
func (s *SportsDS) config() SportsConfig {
	board := ParseSportsConfig(s.Config)
	provider := strings.TrimSpace(s.Provider)
	if provider == "" {
		provider = "espn"
	}
	live := s.LiveRefreshSeconds
	if live < sportsMinLiveRefresh {
		live = sportsDefaultLiveRefresh
	}
	idle := s.IdleRefreshSeconds
	if idle < sportsMinIdleRefresh {
		idle = sportsDefaultIdleRefresh
	}
	return SportsConfig{
		Provider:           provider,
		Token:              s.Token,
		URL:                s.URL,
		Leagues:            board.Leagues,
		Teams:              board.Teams,
		Fixtures:           board.Fixtures,
		MaxGames:           board.MaxGames,
		PreferLive:         board.PreferLive,
		LiveRefreshSeconds: live,
		IdleRefreshSeconds: idle,
	}
}

// effectiveLeagues returns the configured leagues, falling back to the legacy
// token-as-league behavior for ESPN rows created before `config` existed.
func effectiveLeagues(cfg SportsConfig) []string {
	leagues := cleanList(cfg.Leagues)
	if len(leagues) == 0 && strings.EqualFold(cfg.Provider, "espn") {
		if t := strings.TrimSpace(cfg.Token); t != "" {
			leagues = []string{t}
		}
	}
	return leagues
}

// --- Providers ---

type sportsProviderFunc func(cfg SportsConfig) ([]Game, error)

var sportsProviders = map[string]sportsProviderFunc{
	"espn": fetchESPNScores,
}

// sportsFetchGames is the provider dispatch seam; tests override it.
var sportsFetchGames = func(cfg SportsConfig) ([]Game, error) {
	p, ok := sportsProviders[strings.ToLower(cfg.Provider)]
	if !ok {
		return nil, fmt.Errorf("unsupported sports provider %q", cfg.Provider)
	}
	return p(cfg)
}

// fetchESPNScores queries the ESPN scoreboard endpoint once per configured
// league and merges the normalized games.
func fetchESPNScores(cfg SportsConfig) ([]Game, error) {
	leagues := effectiveLeagues(cfg)
	if len(leagues) == 0 {
		return nil, fmt.Errorf("no leagues configured for the espn provider")
	}
	base := strings.TrimSpace(cfg.URL)
	if base == "" {
		base = defaultESPNURL
	}
	var games []Game
	var firstErr error
	for _, league := range leagues {
		body, err := apiGet(sportsURL(base, league), "", nil)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		parsed, err := parseESPNScoreboard(body)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for i := range parsed {
			if parsed[i].League == "" {
				parsed[i].League = league
			}
		}
		games = append(games, parsed...)
	}
	if len(games) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return games, nil
}

type espnStatus struct {
	Period       int    `json:"period"`
	DisplayClock string `json:"displayClock"`
	Type         struct {
		State       string `json:"state"`
		Completed   bool   `json:"completed"`
		ShortDetail string `json:"shortDetail"`
	} `json:"type"`
}

// parseESPNScoreboard normalizes an ESPN scoreboard payload. Parsing is
// tolerant: missing scores, period, clock, or abbreviations degrade to zero or
// empty rather than failing.
func parseESPNScoreboard(body []byte) ([]Game, error) {
	var resp struct {
		Events []struct {
			ID           string `json:"id"`
			Date         string `json:"date"`
			Competitions []struct {
				Competitors []struct {
					HomeAway string          `json:"homeAway"`
					Score    json.RawMessage `json:"score"`
					Team     struct {
						DisplayName      string `json:"displayName"`
						ShortDisplayName string `json:"shortDisplayName"`
						Name             string `json:"name"`
						Abbreviation     string `json:"abbreviation"`
					} `json:"team"`
				} `json:"competitors"`
				Status *espnStatus `json:"status"`
			} `json:"competitions"`
			Status *espnStatus `json:"status"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("espn response is not valid JSON: %w", err)
	}
	games := make([]Game, 0, len(resp.Events))
	for _, ev := range resp.Events {
		if len(ev.Competitions) == 0 {
			continue
		}
		comp := ev.Competitions[0]
		g := Game{ID: ev.ID}
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(ev.Date)); err == nil {
			g.Start = t
		}
		for _, c := range comp.Competitors {
			name := firstNonEmpty(c.Team.DisplayName, c.Team.ShortDisplayName, c.Team.Name, c.Team.Abbreviation)
			abbr := firstNonEmpty(c.Team.Abbreviation, c.Team.ShortDisplayName, c.Team.Name)
			switch c.HomeAway {
			case "home":
				g.Home, g.HomeAbbr, g.HomeScore = name, abbr, flexibleScoreInt(c.Score)
			case "away":
				g.Away, g.AwayAbbr, g.AwayScore = name, abbr, flexibleScoreInt(c.Score)
			}
		}
		st := comp.Status
		if st == nil {
			st = ev.Status
		}
		g.State = espnState(st)
		g.Period, g.Clock = periodClock(st)
		games = append(games, g)
	}
	return games, nil
}

func espnState(st *espnStatus) string {
	if st == nil {
		return GameScheduled
	}
	switch strings.ToLower(strings.TrimSpace(st.Type.State)) {
	case "in":
		return GameLive
	case "post":
		return GameFinal
	case "pre":
		return GameScheduled
	}
	if st.Type.Completed || strings.Contains(strings.ToLower(st.Type.ShortDetail), "final") {
		return GameFinal
	}
	if _, _, ok := livePeriodClock(st); ok {
		return GameLive
	}
	return GameScheduled
}

// periodClock derives a display period and clock from an ESPN status, e.g.
// shortDetail "Q3 08:23" yields period "Q3" and clock "08:23".
func periodClock(st *espnStatus) (string, string) {
	if st == nil {
		return "", ""
	}
	clock := strings.TrimSpace(st.DisplayClock)
	detail := strings.TrimSpace(st.Type.ShortDetail)
	for _, tok := range strings.Fields(detail) {
		if isClockToken(tok) {
			if clock == "" {
				clock = tok
			}
			break
		}
	}
	period := ""
	for _, tok := range strings.Fields(detail) {
		if isClockToken(tok) {
			continue
		}
		low := strings.ToLower(tok)
		if low == "final" || low == "final/ot" || low == "delayed" || low == "postponed" {
			continue
		}
		period = tok
		break
	}
	return period, clock
}

func livePeriodClock(st *espnStatus) (string, string, bool) {
	period, clock := periodClock(st)
	detail := strings.ToUpper(st.Type.ShortDetail)
	if clock == "" || strings.Contains(detail, "AM") || strings.Contains(detail, "PM") {
		return "", "", false
	}
	return period, clock, true
}

func isClockToken(tok string) bool {
	if !strings.Contains(tok, ":") || tok == "" {
		return false
	}
	return tok[0] >= '0' && tok[0] <= '9'
}

func flexibleScoreInt(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return int(f)
		}
		return 0
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int(f)
	}
	return 0
}

// --- Filtering ---

func lowerSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, s := range items {
		out[strings.ToLower(strings.TrimSpace(s))] = true
	}
	return out
}

// filterGames applies the league, team, and fixture filters. A game is kept
// when its league is followed and either no team filter is set, a competitor
// matches (name or abbreviation, case-insensitive), or its id is a configured
// fixture.
func filterGames(games []Game, cfg SportsConfig) []Game {
	leagues := lowerSet(effectiveLeagues(cfg))
	teams := lowerSet(cleanList(cfg.Teams))
	fixtures := lowerSet(cleanList(cfg.Fixtures))
	out := make([]Game, 0, len(games))
	for _, g := range games {
		if len(leagues) > 0 && !leagues[strings.ToLower(g.League)] {
			continue
		}
		if fixtures[strings.ToLower(g.ID)] {
			out = append(out, g)
			continue
		}
		if len(teams) == 0 {
			if len(fixtures) > 0 {
				continue
			}
			out = append(out, g)
			continue
		}
		if teams[strings.ToLower(g.Home)] || teams[strings.ToLower(g.HomeAbbr)] ||
			teams[strings.ToLower(g.Away)] || teams[strings.ToLower(g.AwayAbbr)] {
			out = append(out, g)
		}
	}
	return out
}

// --- Adaptive TTL cache with single-flight ---

type sportsCacheEntry struct {
	games     []Game
	fetchedAt time.Time
	hasData   bool
	err       error
	fetching  bool
	done      chan struct{}
}

var (
	sportsCacheMu sync.Mutex
	sportsCache   = map[string]*sportsCacheEntry{}
)

func sportsCacheKey(cfg SportsConfig) string {
	h := sha256.Sum256([]byte(strings.Join([]string{
		cfg.Provider, cfg.Token, cfg.URL,
		strings.Join(cleanList(cfg.Leagues), ","),
		strings.Join(cleanList(cfg.Teams), ","),
		strings.Join(cleanList(cfg.Fixtures), ","),
		strconv.Itoa(cfg.MaxGames), strconv.FormatBool(cfg.PreferLive),
	}, "\x00")))
	return fmt.Sprintf("%x", h)
}

func clearSportsCache() {
	sportsCacheMu.Lock()
	sportsCache = map[string]*sportsCacheEntry{}
	sportsCacheMu.Unlock()
}

func gamesLive(games []Game) bool {
	for _, g := range games {
		if g.State == GameLive {
			return true
		}
	}
	return false
}

func sportsTTL(games []Game, cfg SportsConfig) time.Duration {
	if gamesLive(games) {
		return time.Duration(cfg.LiveRefreshSeconds) * time.Second
	}
	return time.Duration(cfg.IdleRefreshSeconds) * time.Second
}

// cachedGames returns the normalized, filtered games for the datasource config
// using the shared adaptive cache. A successful fetch (even empty) is cached
// for the live or idle TTL; concurrent callers share one upstream request; a
// failure retains prior games when available and otherwise returns the error.
func (s *SportsDS) cachedGames() ([]Game, error) {
	cfg := s.config()
	key := sportsCacheKey(cfg)

	sportsCacheMu.Lock()
	e := sportsCache[key]
	if e == nil {
		e = &sportsCacheEntry{}
		sportsCache[key] = e
	}
	if e.hasData && time.Since(e.fetchedAt) < sportsTTL(e.games, cfg) {
		games := e.games
		sportsCacheMu.Unlock()
		return games, nil
	}
	if e.fetching {
		done := e.done
		sportsCacheMu.Unlock()
		<-done
		sportsCacheMu.Lock()
		games, has, fetchErr := e.games, e.hasData, e.err
		sportsCacheMu.Unlock()
		if has {
			return games, nil
		}
		return nil, fetchErr
	}
	e.fetching = true
	e.done = make(chan struct{})
	done := e.done
	sportsCacheMu.Unlock()

	games, err := sportsFetchGames(cfg)
	if err == nil {
		games = filterGames(games, cfg)
	}

	sportsCacheMu.Lock()
	e.fetching = false
	e.err = err
	if err == nil {
		e.games = games
		e.hasData = true
		e.fetchedAt = time.Now()
	}
	close(done)
	has := e.hasData
	prior := e.games
	sportsCacheMu.Unlock()

	if err == nil {
		return games, nil
	}
	if has {
		return prior, nil
	}
	return nil, err
}

// --- Rendering ---

func displayTeam(abbr, name string) string {
	if strings.TrimSpace(abbr) != "" {
		return strings.TrimSpace(abbr)
	}
	return strings.TrimSpace(name)
}

func scoreLine(g Game) string {
	home := displayTeam(g.HomeAbbr, g.Home)
	away := displayTeam(g.AwayAbbr, g.Away)
	if g.State == GameScheduled {
		return fmt.Sprintf("%s vs %s", home, away)
	}
	return fmt.Sprintf("%s %d - %s %d", home, g.HomeScore, away, g.AwayScore)
}

func statusLine(g Game) string {
	switch g.State {
	case GameLive:
		parts := []string{"LIVE"}
		if g.Period != "" {
			parts = append(parts, g.Period)
		}
		if g.Clock != "" {
			parts = append(parts, g.Clock)
		}
		return strings.Join(parts, " ")
	case GameFinal:
		return "FINAL"
	default:
		if !g.Start.IsZero() {
			return g.Start.Local().Format("3:04 PM")
		}
		return "SCHEDULED"
	}
}

func fixtureLabel(g Game) string {
	return fmt.Sprintf("%s vs %s", displayTeam(g.HomeAbbr, g.Home), displayTeam(g.AwayAbbr, g.Away))
}

// sortGames orders live games first, then scheduled, then final; within a
// state the earliest start comes first.
func sortGames(games []Game) []Game {
	out := make([]Game, len(games))
	copy(out, games)
	rank := func(g Game) int {
		switch g.State {
		case GameLive:
			return 0
		case GameScheduled:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		if out[i].Start.IsZero() {
			return false
		}
		if out[j].Start.IsZero() {
			return true
		}
		return out[i].Start.Before(out[j].Start)
	})
	return out
}

func nextScheduled(games []Game) (Game, bool) {
	var best Game
	found := false
	for _, g := range games {
		if g.State != GameScheduled {
			continue
		}
		if !found || best.Start.IsZero() || (!g.Start.IsZero() && g.Start.Before(best.Start)) {
			best = g
			found = true
		}
	}
	return best, found
}

// BuildSportsRows orders, caps, and formats games into {score, status} pairs.
func BuildSportsRows(games []Game, cfg SportsConfig, now time.Time) [][2]string {
	_ = now
	sorted := sortGames(games)
	max := cfg.MaxGames
	if max < 1 || max > sportsMaxGames {
		max = sportsDefaultMaxGames
	}
	if len(sorted) > max {
		sorted = sorted[:max]
	}
	rows := make([][2]string, 0, len(sorted))
	for _, g := range sorted {
		rows = append(rows, [2]string{truncateRunes(scoreLine(g), 28), truncateRunes(statusLine(g), 28)})
	}
	return rows
}

// sportsRenderData builds the RenderDict payload: one score line plus a status
// line per game, a NEXT fixture line when nothing is live, and a placeholder
// when a successful fetch yielded no matching games.
func sportsRenderData(games []Game, cfg SportsConfig, now time.Time) map[string]string {
	if len(games) == 0 {
		return map[string]string{"r1": "no games"}
	}
	data := map[string]string{}
	idx := 1
	for _, r := range BuildSportsRows(games, cfg, now) {
		data[fmt.Sprintf("r%d", idx)] = strings.TrimSpace(r[0])
		idx++
		data[fmt.Sprintf("r%d", idx)] = strings.TrimSpace(r[1])
		idx++
	}
	if !gamesLive(games) {
		if g, ok := nextScheduled(games); ok {
			line := "NEXT: " + fixtureLabel(g)
			if !g.Start.IsZero() {
				line += " " + g.Start.Local().Format("3:04 PM")
			}
			data[fmt.Sprintf("r%d", idx)] = truncateRunes(line, 28)
		}
	}
	return data
}

func (s *SportsDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	games, err := s.cachedGames()
	if err != nil {
		slog.Warn("sports fetch failed with no cached games", "source", "sports", "provider", s.Provider, "error", err)
		return nil, err
	}
	data := sportsRenderData(games, s.config(), time.Now())
	theme := DefaultTheme()
	theme.Title = "SPORTS"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	slog.Info("sports data rendered", "source", "sports", "games", len(games))
	return img, nil
}

// CurrentState implements datasource.StateProvider, exposing the current live
// game (or the next scheduled fixture when idle) from the shared cache without
// an extra upstream fetch.
func (s *SportsDS) CurrentState(ctx context.Context) (map[string]any, error) {
	_ = ctx
	games, err := s.cachedGames()
	if err != nil {
		return nil, err
	}
	out := map[string]any{"live": false}
	sorted := sortGames(games)
	var current *Game
	for i := range sorted {
		if sorted[i].State == GameLive {
			current = &sorted[i]
			break
		}
	}
	if current == nil && len(sorted) > 0 {
		current = &sorted[0]
	}
	if current != nil {
		out["state"] = current.State
		out["period"] = current.Period
		out["clock"] = current.Clock
		out["home"] = current.Home
		out["away"] = current.Away
		out["home_score"] = current.HomeScore
		out["away_score"] = current.AwayScore
		out["live"] = current.State == GameLive
	}
	if !out["live"].(bool) {
		if g, ok := nextScheduled(games); ok {
			next := map[string]any{"name": fixtureLabel(g)}
			if !g.Start.IsZero() {
				next["time"] = g.Start.Format(time.RFC3339)
			}
			out["next_fixture"] = next
		}
	}
	return out, nil
}
