package datasource

import (
	"bytes"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func makeSportsEvent(homeAbbr, homeScore, awayAbbr, awayScore, shortDetail string, scoreQuoted bool) string {
	hs := homeScore
	as := awayScore
	if scoreQuoted {
		hs = fmt.Sprintf("%q", hs)
		as = fmt.Sprintf("%q", as)
	}
	status := ""
	if shortDetail != "" {
		status = fmt.Sprintf(`,"status":{"type":{"shortDetail":%q}}`, shortDetail)
	}
	// Include extra competitor to test filter when needed via separate test
	return fmt.Sprintf(`{"competitions":[{"competitors":[{"homeAway":"home","team":{"abbreviation":%q},"score":%s},{"homeAway":"away","team":{"abbreviation":%q},"score":%s}]%s}]}`, homeAbbr, hs, awayAbbr, as, status)
}

func TestBuildSportsRows_ScoreAsString(t *testing.T) {
	body := []byte(fmt.Sprintf(`{"events":[%s]}`, makeSportsEvent("PHI", "21", "DAL", "14", "Q3 08:23", true)))
	rows, err := BuildSportsRows(body)
	if err != nil {
		t.Fatalf("BuildSportsRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len %d want 1", len(rows))
	}
	if rows[0][0] != "PHI 21 DAL 14" {
		t.Fatalf("row1 = %q want %q", rows[0][0], "PHI 21 DAL 14")
	}
	if rows[0][1] != "Q3 08:23" {
		t.Fatalf("row2 = %q want %q", rows[0][1], "Q3 08:23")
	}
}

func TestBuildSportsRows_ScoreAsNumber(t *testing.T) {
	body := []byte(fmt.Sprintf(`{"events":[%s]}`, makeSportsEvent("PHI", "21", "DAL", "14", "Final", false)))
	rows, err := BuildSportsRows(body)
	if err != nil {
		t.Fatalf("BuildSportsRows: %v", err)
	}
	if rows[0][0] != "PHI 21 DAL 14" {
		t.Fatalf("row1 = %q want PHI 21 DAL 14", rows[0][0])
	}
	if rows[0][1] != "Final" {
		t.Fatalf("row2 = %q want Final", rows[0][1])
	}
}

func TestBuildSportsRows_MissingStatus(t *testing.T) {
	body := []byte(fmt.Sprintf(`{"events":[%s]}`, makeSportsEvent("PHI", "7", "DAL", "3", "", true)))
	rows, err := BuildSportsRows(body)
	if err != nil {
		t.Fatalf("BuildSportsRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len %d want 1", len(rows))
	}
	if rows[0][0] != "PHI 7 DAL 3" {
		t.Fatalf("row1 %q", rows[0][0])
	}
	if rows[0][1] != "" {
		t.Fatalf("row2 should be empty when status missing, got %q", rows[0][1])
	}
}

func TestBuildSportsRows_FilterExtraCompetitors(t *testing.T) {
	body := []byte(`{"events":[{"competitions":[{"competitors":[
		{"homeAway":"home","team":{"abbreviation":"PHI"},"score":"10"},
		{"homeAway":"away","team":{"abbreviation":"DAL"},"score":"7"},
		{"homeAway":"extra","team":{"abbreviation":"XXX"},"score":"99"}
	],"status":{"type":{"shortDetail":"Q1 10:00"}}}]}]}`)
	rows, err := BuildSportsRows(body)
	if err != nil {
		t.Fatalf("BuildSportsRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len %d want 1", len(rows))
	}
	if rows[0][0] != "PHI 10 DAL 7" {
		t.Fatalf("row1 = %q want PHI 10 DAL 7", rows[0][0])
	}
}

func TestBuildSportsRows_CapAt4(t *testing.T) {
	var events []string
	for i := 0; i < 6; i++ {
		events = append(events, makeSportsEvent(fmt.Sprintf("H%d", i), "1", fmt.Sprintf("A%d", i), "2", "Final", true))
	}
	body := []byte(fmt.Sprintf(`{"events":[%s]}`, strings.Join(events, ",")))
	rows, err := BuildSportsRows(body)
	if err != nil {
		t.Fatalf("BuildSportsRows: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("rows len %d want 4 (cap)", len(rows))
	}
}

func TestSports_URLSubstitution(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(fmt.Sprintf(`{"events":[%s]}`, makeSportsEvent("PHI", "21", "DAL", "14", "Q3 08:23", true))))
	}))
	defer srv.Close()

	ds := &SportsDS{Token: "nfl", URL: srv.URL + "/%s/scoreboard"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if !strings.Contains(gotPath, "nfl") {
		t.Fatalf("path %q should contain league slug nfl", gotPath)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestSports_SuccessRender(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fmt.Sprintf(`{"events":[%s]}`, makeSportsEvent("PHI", "21", "DAL", "14", "Q3 08:23", true))))
	}))
	defer srv.Close()
	ds := &SportsDS{Token: "nba", URL: srv.URL + "/%s/board"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if img == nil || len(img.Data) == 0 {
		t.Fatal("nil image")
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("png decode: %v", err)
	}
}

func TestSports_EmptyEventsFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"events":[]}`))
	}))
	defer srv.Close()
	ds := &SportsDS{Token: "nfl", URL: srv.URL + "/%s/scoreboard"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG empty should fallback not error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("fallback decode: %v", err)
	}
}

func TestSports_NetworkErrorFallback(t *testing.T) {
	// Use invalid URL to trigger apiGet error
	ds := &SportsDS{Token: "nfl", URL: "http://127.0.0.1:1/%s/scoreboard"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG network error should fallback not error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("fallback decode: %v", err)
	}
}

func TestSports_MalformedJSONFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	ds := &SportsDS{Token: "nfl", URL: srv.URL + "/%s/scoreboard"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG malformed should fallback: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestSports_Truncation(t *testing.T) {
	long := strings.Repeat("A", 40)
	body := []byte(fmt.Sprintf(`{"events":[%s]}`, makeSportsEvent(long, "21", long, "14", long, true)))
	rows, err := BuildSportsRows(body)
	if err != nil {
		t.Fatalf("BuildSportsRows: %v", err)
	}
	if len(rows[0][0]) > 28 {
		t.Fatalf("row1 len %d >28", len(rows[0][0]))
	}
	if len(rows[0][1]) > 28 {
		t.Fatalf("row2 len %d >28", len(rows[0][1]))
	}
}

func TestSports_TableDrivenScores(t *testing.T) {
	tests := []struct {
		name      string
		homeScore string
		awayScore string
		quoted    bool
		wantRow1  string
	}{
		{"string scores", "21", "14", true, "PHI 21 DAL 14"},
		{"number scores", "21", "14", false, "PHI 21 DAL 14"},
		{"zero scores", "0", "0", true, "PHI 0 DAL 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"events":[%s]}`, makeSportsEvent("PHI", tc.homeScore, "DAL", tc.awayScore, "Final", tc.quoted)))
			rows, err := BuildSportsRows(body)
			if err != nil {
				t.Fatalf("BuildSportsRows: %v", err)
			}
			if rows[0][0] != tc.wantRow1 {
				t.Fatalf("row1 = %q want %q", rows[0][0], tc.wantRow1)
			}
		})
	}
}
