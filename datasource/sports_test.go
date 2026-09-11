package datasource

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func withSportsFetch(t *testing.T, fn func(SportsConfig) ([]Game, error)) {
	t.Helper()
	prev := sportsFetchGames
	sportsFetchGames = fn
	clearSportsCache()
	t.Cleanup(func() {
		sportsFetchGames = prev
		clearSportsCache()
	})
}

func espnPayload(events ...string) []byte {
	return []byte(fmt.Sprintf(`{"events":[%s]}`, strings.Join(events, ",")))
}

const (
	espnLiveEvent = `{"id":"1","date":"2025-01-01T18:00:00Z","competitions":[{"competitors":[
		{"homeAway":"home","team":{"displayName":"Philadelphia Eagles","abbreviation":"PHI"},"score":"21"},
		{"homeAway":"away","team":{"displayName":"Dallas Cowboys","abbreviation":"DAL"},"score":14}
	],"status":{"period":3,"displayClock":"8:23","type":{"state":"in","shortDetail":"Q3 08:23"}}}]}`

	espnFinalEvent = `{"id":"2","date":"2024-12-31T18:00:00Z","competitions":[{"competitors":[
		{"homeAway":"home","team":{"displayName":"Boston Celtics","abbreviation":"BOS"},"score":"110"},
		{"homeAway":"away","team":{"displayName":"New York Knicks","abbreviation":"NYK"},"score":"99"}
	],"status":{"type":{"state":"post","completed":true,"shortDetail":"Final"}}}]}`

	espnScheduledEvent = `{"id":"3","date":"2999-01-01T00:30:00Z","competitions":[{"competitors":[
		{"homeAway":"home","team":{"displayName":"Los Angeles Lakers","abbreviation":"LAL"},"score":null},
		{"homeAway":"away","team":{"displayName":"Golden State Warriors","abbreviation":"GSW"}}
	],"status":{"type":{"state":"pre","shortDetail":"7:30 PM"}}}]}`
)

func TestParseESPNScoreboard_Normalization(t *testing.T) {
	games, err := parseESPNScoreboard(espnPayload(espnLiveEvent, espnFinalEvent, espnScheduledEvent))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(games) != 3 {
		t.Fatalf("games len %d want 3", len(games))
	}
	live := games[0]
	if live.State != GameLive || live.Period != "Q3" || live.Clock != "8:23" {
		t.Fatalf("live game = %+v", live)
	}
	if live.Home != "Philadelphia Eagles" || live.HomeAbbr != "PHI" || live.HomeScore != 21 || live.AwayScore != 14 {
		t.Fatalf("live teams/scores = %+v", live)
	}
	final := games[1]
	if final.State != GameFinal || final.HomeScore != 110 || final.AwayScore != 99 {
		t.Fatalf("final game = %+v", final)
	}
	sched := games[2]
	if sched.State != GameScheduled || !sched.Start.IsZero() == false {
		t.Fatalf("scheduled game = %+v", sched)
	}
	if sched.HomeScore != 0 || sched.AwayScore != 0 {
		t.Fatalf("scheduled scores should be zero: %+v", sched)
	}
}

func TestParseESPNScoreboard_MissingOptionalFields(t *testing.T) {
	payload := []byte(`{"events":[{"id":"9","competitions":[{"competitors":[
		{"homeAway":"home","team":{"name":"Homers"}},
		{"homeAway":"away","team":{"name":"Aways"}}
	],"status":{"type":{"state":"in","shortDetail":"H2"}}}]}]}`)
	games, err := parseESPNScoreboard(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("len %d want 1", len(games))
	}
	g := games[0]
	if g.Clock != "" || g.HomeScore != 0 || g.AwayScore != 0 {
		t.Fatalf("optional fields not tolerated: %+v", g)
	}
	if g.Home != "Homers" || g.HomeAbbr != "Homers" {
		t.Fatalf("name fallback failed: %+v", g)
	}
	if g.State != GameLive {
		t.Fatalf("state = %q want live", g.State)
	}
}

func TestParseESPNScoreboard_Malformed(t *testing.T) {
	if _, err := parseESPNScoreboard([]byte("not json")); err == nil {
		t.Fatal("expected error for malformed payload")
	}
}

func TestFilterGames(t *testing.T) {
	games := []Game{
		{ID: "1", League: "nfl", Home: "Philadelphia Eagles", HomeAbbr: "PHI", Away: "Dallas Cowboys", AwayAbbr: "DAL", State: GameLive},
		{ID: "2", League: "nba", Home: "Boston Celtics", HomeAbbr: "BOS", Away: "New York Knicks", AwayAbbr: "NYK", State: GameFinal},
		{ID: "3", League: "nba", Home: "LA Lakers", HomeAbbr: "LAL", Away: "GSW", AwayAbbr: "GSW", State: GameScheduled},
	}
	// team abbreviation matches either side, case-insensitive
	got := filterGames(games, SportsConfig{Provider: "espn", Leagues: []string{"nfl"}, Teams: []string{"phi"}})
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("team filter = %+v", got)
	}
	// fixture id includes a game even without a team match
	got = filterGames(games, SportsConfig{Provider: "espn", Leagues: []string{"nba"}, Fixtures: []string{"3"}})
	if len(got) != 1 || got[0].ID != "3" {
		t.Fatalf("fixture filter = %+v", got)
	}
	// legacy: token acts as the league slug when leagues empty
	got = filterGames(games, SportsConfig{Provider: "espn", Token: "nba"})
	if len(got) != 2 {
		t.Fatalf("legacy league filter = %+v", got)
	}
	// league only, no team filter: all games in league
	got = filterGames(games, SportsConfig{Provider: "espn", Leagues: []string{"nba"}})
	if len(got) != 2 {
		t.Fatalf("league filter = %+v", got)
	}
}

func TestSportsTTL_LiveVsIdle(t *testing.T) {
	cfg := SportsConfig{LiveRefreshSeconds: 15, IdleRefreshSeconds: 300}
	live := []Game{{State: GameLive}}
	if got := sportsTTL(live, cfg); got != 15*time.Second {
		t.Fatalf("live ttl = %v", got)
	}
	idle := []Game{{State: GameScheduled}, {State: GameFinal}}
	if got := sportsTTL(idle, cfg); got != 300*time.Second {
		t.Fatalf("idle ttl = %v", got)
	}
}

func TestSportsConfig_FloorsAndDefaults(t *testing.T) {
	ds := &SportsDS{Provider: "", LiveRefreshSeconds: 5, IdleRefreshSeconds: 10}
	cfg := ds.config()
	if cfg.Provider != "espn" {
		t.Fatalf("provider = %q want espn", cfg.Provider)
	}
	if cfg.LiveRefreshSeconds != sportsDefaultLiveRefresh || cfg.IdleRefreshSeconds != sportsDefaultIdleRefresh {
		t.Fatalf("floors not applied: %+v", cfg)
	}
	ds = &SportsDS{Provider: "espn", LiveRefreshSeconds: 20, IdleRefreshSeconds: 90}
	cfg = ds.config()
	if cfg.LiveRefreshSeconds != 20 || cfg.IdleRefreshSeconds != 90 {
		t.Fatalf("valid intervals changed: %+v", cfg)
	}
}

func TestCachedGames_SingleFlight(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	withSportsFetch(t, func(cfg SportsConfig) ([]Game, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return []Game{{ID: "1", League: "nfl", State: GameLive}}, nil
	})

	ds := &SportsDS{Provider: "espn", Token: "nfl", Config: `{"leagues":["nfl"]}`}
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = ds.cachedGames()
		}(i)
	}
	// Give the goroutines time to observe the in-flight fetch before releasing.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d want 1", got)
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d erred: %v", i, err)
		}
	}
}

func TestCachedGames_LiveRefreshAfterTTLElapsed(t *testing.T) {
	var calls int32
	withSportsFetch(t, func(cfg SportsConfig) ([]Game, error) {
		atomic.AddInt32(&calls, 1)
		return []Game{{ID: "1", League: "nfl", State: GameLive}}, nil
	})
	ds := &SportsDS{Provider: "espn", Token: "nfl", Config: `{"leagues":["nfl"]}`, LiveRefreshSeconds: 15}
	if _, err := ds.cachedGames(); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if _, err := ds.cachedGames(); err != nil {
		t.Fatalf("cached fetch: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls within TTL = %d want 1", got)
	}
	// expire the entry then confirm a live game triggers a refetch.
	sportsCacheMu.Lock()
	for _, e := range sportsCache {
		e.fetchedAt = time.Now().Add(-time.Minute)
	}
	sportsCacheMu.Unlock()
	if _, err := ds.cachedGames(); err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls after TTL = %d want 2", got)
	}
}

func TestCachedGames_FailureRetainsPriorGames(t *testing.T) {
	ok := true
	withSportsFetch(t, func(cfg SportsConfig) ([]Game, error) {
		if !ok {
			return nil, fmt.Errorf("boom")
		}
		return []Game{{ID: "1", League: "nfl", State: GameFinal, HomeAbbr: "PHI", AwayAbbr: "DAL"}}, nil
	})
	ds := &SportsDS{Provider: "espn", Token: "nfl", Config: `{"leagues":["nfl"]}`}
	if _, err := ds.cachedGames(); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	ok = false
	sportsCacheMu.Lock()
	for _, e := range sportsCache {
		e.fetchedAt = time.Now().Add(-time.Hour)
	}
	sportsCacheMu.Unlock()
	games, err := ds.cachedGames()
	if err != nil {
		t.Fatalf("prior games should be served: %v", err)
	}
	if len(games) != 1 || games[0].ID != "1" {
		t.Fatalf("prior games = %+v", games)
	}
}

func TestCachedGames_FirstRunFailureErrors(t *testing.T) {
	withSportsFetch(t, func(cfg SportsConfig) ([]Game, error) { return nil, fmt.Errorf("boom") })
	ds := &SportsDS{Provider: "espn", Token: "nfl", Config: `{"leagues":["nfl"]}`}
	if _, err := ds.cachedGames(); err == nil {
		t.Fatal("expected error when no games were ever normalized")
	}
}

func TestBuildSportsRows_OrderAndCap(t *testing.T) {
	cfg := SportsConfig{MaxGames: 4}
	games := []Game{
		{ID: "f", State: GameFinal, HomeAbbr: "A", AwayAbbr: "B", HomeScore: 1, AwayScore: 2, Start: time.Now().Add(-3 * time.Hour)},
		{ID: "s", State: GameScheduled, HomeAbbr: "C", AwayAbbr: "D", Start: time.Now().Add(time.Hour)},
		{ID: "l", State: GameLive, HomeAbbr: "E", AwayAbbr: "F", HomeScore: 3, AwayScore: 4},
		{ID: "f2", State: GameFinal, HomeAbbr: "G", AwayAbbr: "H", HomeScore: 5, AwayScore: 6},
		{ID: "s2", State: GameScheduled, HomeAbbr: "I", AwayAbbr: "J", Start: time.Now().Add(2 * time.Hour)},
	}
	rows := BuildSportsRows(games, cfg, time.Now())
	if len(rows) != 4 {
		t.Fatalf("rows = %d want 4", len(rows))
	}
	if !strings.HasPrefix(rows[0][1], "LIVE") {
		t.Fatalf("first row should be live: %+v", rows[0])
	}
	if rows[1][1] == "LIVE" || rows[1][1] == "FINAL" {
		t.Fatalf("second row should be scheduled: %+v", rows[1])
	}
	if rows[0][0] != "E 3 - F 4" {
		t.Fatalf("live score line = %q", rows[0][0])
	}
}

func TestSportsRenderData(t *testing.T) {
	now := time.Now()
	// no games placeholder
	data := sportsRenderData(nil, SportsConfig{MaxGames: 4}, now)
	if data["r1"] != "no games" {
		t.Fatalf("no-games placeholder = %v", data)
	}
	// final game status line
	games := []Game{{State: GameFinal, HomeAbbr: "PHI", AwayAbbr: "DAL", HomeScore: 21, AwayScore: 14}}
	data = sportsRenderData(games, SportsConfig{MaxGames: 4}, now)
	if data["r2"] != "FINAL" {
		t.Fatalf("final status = %v", data)
	}
	// live with period/clock and no NEXT line
	games = []Game{{State: GameLive, HomeAbbr: "PHI", AwayAbbr: "DAL", HomeScore: 21, AwayScore: 14, Period: "Q3", Clock: "8:23"}}
	data = sportsRenderData(games, SportsConfig{MaxGames: 4}, now)
	if data["r2"] != "LIVE Q3 8:23" {
		t.Fatalf("live status = %v", data)
	}
	for k, v := range data {
		if strings.HasPrefix(v, "NEXT:") {
			t.Fatalf("unexpected NEXT while live: %s=%s", k, v)
		}
	}
	// idle with a future fixture -> NEXT line
	games = []Game{{State: GameScheduled, HomeAbbr: "LAL", AwayAbbr: "GSW", Start: now.Add(2 * time.Hour)}}
	data = sportsRenderData(games, SportsConfig{MaxGames: 4}, now)
	foundNext := false
	for _, v := range data {
		if strings.HasPrefix(v, "NEXT:") {
			foundNext = true
		}
	}
	if !foundNext {
		t.Fatalf("expected NEXT line: %v", data)
	}
}

func TestGetPNG_FirstRunFailureErrors(t *testing.T) {
	withSportsFetch(t, func(cfg SportsConfig) ([]Game, error) { return nil, fmt.Errorf("boom") })
	ds := &SportsDS{Provider: "espn", Token: "nfl", Config: `{"leagues":["nfl"]}`}
	if _, err := ds.GetPNG(64, 64); err == nil {
		t.Fatal("expected error on first-run failure")
	}
}

func TestGetPNG_LaterFailureServesLastGood(t *testing.T) {
	ok := true
	withSportsFetch(t, func(cfg SportsConfig) ([]Game, error) {
		if !ok {
			return nil, fmt.Errorf("boom")
		}
		return []Game{{ID: "1", League: "nfl", State: GameFinal, HomeAbbr: "PHI", AwayAbbr: "DAL", HomeScore: 21, AwayScore: 14}}, nil
	})
	ds := &SportsDS{Provider: "espn", Token: "nfl", Config: `{"leagues":["nfl"]}`}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ok = false
	sportsCacheMu.Lock()
	for _, e := range sportsCache {
		e.fetchedAt = time.Now().Add(-time.Hour)
	}
	sportsCacheMu.Unlock()
	img, err = ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("later failure should render last good: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode last good: %v", err)
	}
}

func TestFetchESPNScores_MultiLeagueAndURL(t *testing.T) {
	var mu sync.Mutex
	leagues := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		leagues = append(leagues, strings.Trim(r.URL.Path, "/"))
		mu.Unlock()
		w.Write(espnPayload(espnLiveEvent))
	}))
	defer srv.Close()

	ds := &SportsDS{
		Provider: "espn",
		URL:      srv.URL + "/%s/scoreboard",
		Config:   `{"leagues":["nba","nfl"]}`,
	}
	games, err := fetchESPNScores(ds.config())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(games) != 2 {
		t.Fatalf("games = %d want 2", len(games))
	}
	if len(leagues) != 2 || !strings.Contains(strings.Join(leagues, ","), "nba") || !strings.Contains(strings.Join(leagues, ","), "nfl") {
		t.Fatalf("leagues queried = %v", leagues)
	}
}

func TestFetchESPNScores_LegacyTokenLeague(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "nba") {
			t.Errorf("path %q should contain legacy league nba", r.URL.Path)
		}
		w.Write(espnPayload(espnFinalEvent))
	}))
	defer srv.Close()
	ds := &SportsDS{Provider: "espn", Token: "nba", URL: srv.URL + "/%s/scoreboard"}
	games, err := fetchESPNScores(ds.config())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(games) != 1 || games[0].League != "nba" {
		t.Fatalf("legacy league fetch = %+v", games)
	}
}

func TestValidateSportsConfig(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"valid league", `{"leagues":["nfl"]}`, false},
		{"valid team", `{"teams":["PHI"]}`, false},
		{"valid fixture", `{"fixtures":["401"]}`, false},
		{"empty", ``, true},
		{"invalid json", `{`, true},
		{"no filters", `{"max_games":2}`, true},
		{"max games too high", `{"leagues":["nfl"],"max_games":99}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSportsConfig(tc.raw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestCurrentState_LiveAndIdle(t *testing.T) {
	live := []Game{{ID: "1", League: "nfl", State: GameLive, Home: "Eagles", Away: "Cowboys", HomeScore: 21, AwayScore: 14, Period: "Q3", Clock: "8:23"}}
	withSportsFetch(t, func(cfg SportsConfig) ([]Game, error) { return live, nil })
	ds := &SportsDS{Provider: "espn", Token: "nfl", Config: `{"leagues":["nfl"]}`}
	state, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if state["live"] != true || state["home_score"] != 21 || state["period"] != "Q3" {
		t.Fatalf("live state = %+v", state)
	}

	idle := []Game{{ID: "2", League: "nfl", State: GameScheduled, Home: "Lakers", Away: "Warriors", Start: time.Now().Add(3 * time.Hour)}}
	withSportsFetch(t, func(cfg SportsConfig) ([]Game, error) { return idle, nil })
	state, err = ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState idle: %v", err)
	}
	if state["live"] != false {
		t.Fatalf("idle live = %v", state["live"])
	}
	if _, ok := state["next_fixture"]; !ok {
		t.Fatalf("idle state missing next_fixture: %+v", state)
	}
}

func TestCurrentState_FirstRunFailureErrors(t *testing.T) {
	withSportsFetch(t, func(cfg SportsConfig) ([]Game, error) { return nil, fmt.Errorf("boom") })
	ds := &SportsDS{Provider: "espn", Token: "nfl", Config: `{"leagues":["nfl"]}`}
	if _, err := ds.CurrentState(context.Background()); err == nil {
		t.Fatal("expected error when no data is available")
	}
}
