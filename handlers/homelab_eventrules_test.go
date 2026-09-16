package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/enttest"
)

func TestHomelabEventRuleResolve(t *testing.T) {
	orig := RuleTargetResolver
	RuleTargetResolver = nil
	t.Cleanup(func() { RuleTargetResolver = orig })
	ResetNonCapableLogged()

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
		t.Fatalf("seed gs: %v", err)
	}

	// Stub for UptimeKuma CurrentState: serves heartbeat with one DOWN, one UP.
	// Also serves manifest (not needed for CurrentState but avoids 404 noise).
	slug := "myslug"
	manifestBody := `{"publicGroupList":[{"monitorList":[{"id":1,"name":"A"},{"id":2,"name":"B"}]}]}`
	heartbeatBody := `{"heartbeatList":{"1":[{"status":0}],"2":[{"status":1}]},"uptimeList":{"1_24":0.5,"2_24":0.99}}`
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status-page/"+slug, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(manifestBody))
	})
	mux.HandleFunc("/api/status-page/heartbeat/"+slug, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(heartbeatBody))
	})
	stub := httptest.NewServer(mux)
	t.Cleanup(func() { stub.Close() })

	// Seed one row of each of the six entities.
	im := client.Immich.Create().SetURL("http://immich.local").SetToken("immich-tok").SetConfig(`{}`).SaveX(ctx)
	qb := client.Qbittorrent.Create().SetToken("u:p").SetURL("http://qb.local").SaveX(ctx)
	sb := client.Sabnzbd.Create().SetToken("sabkey").SetURL("http://sab.local/api").SaveX(ctx)
	ov := client.Overseerr.Create().SetToken("ovtok").SetURL("http://ov.local").SaveX(ctx)
	uk := client.UptimeKuma.Create().SetToken(slug).SetURL(stub.URL).SaveX(ctx)
	st := client.Speedtest.Create().SetToken("sttok").SetURL("http://st.local").SaveX(ctx)

	// Attach all six to GeneralSettings ID 1.
	gs := client.GeneralSettings.GetX(ctx, 1)
	if _, err := client.GeneralSettings.UpdateOne(gs).
		AddImmichs(im).
		AddQbittorrents(qb).
		AddSabnzbd(sb).
		AddOverseerrs(ov).
		AddUptimeKumas(uk).
		AddSpeedtests(st).
		Save(ctx); err != nil {
		t.Fatalf("add edges: %v", err)
	}

	cases := []struct {
		typ  string
		id   int
		want string
	}{
		{"immich", im.ID, "*datasource.ImmichDS"},
		{"qbittorrent", qb.ID, "*datasource.QBittorrentDS"},
		{"sabnzbd", sb.ID, "*datasource.SabnzbdDS"},
		{"overseerr", ov.ID, "*datasource.OverseerrDS"},
		{"uptimekuma", uk.ID, "*datasource.UptimeKumaDS"},
		{"speedtest", st.ID, "*datasource.SpeedtestDS"},
	}
	for _, tc := range cases {
		ds, ok := resolveTarget(tc.typ, tc.id, client)
		if !ok || ds == nil {
			t.Fatalf("resolveTarget %s:%d not ok", tc.typ, tc.id)
		}
		got := fmt.Sprintf("%T", ds)
		if got != tc.want {
			t.Fatalf("resolveTarget %s:%d = %s want %s", tc.typ, tc.id, got, tc.want)
		}
		if _, ok := ds.(datasource.StateProvider); !ok {
			t.Fatalf("%s:%d does not implement StateProvider", tc.typ, tc.id)
		}
		// Type-specific assertion for extra confidence.
		switch tc.typ {
		case "immich":
			if _, ok := ds.(*datasource.ImmichDS); !ok {
				t.Fatalf("want *ImmichDS got %T", ds)
			}
		case "qbittorrent":
			if _, ok := ds.(*datasource.QBittorrentDS); !ok {
				t.Fatalf("want *QBittorrentDS got %T", ds)
			}
		case "sabnzbd":
			if _, ok := ds.(*datasource.SabnzbdDS); !ok {
				t.Fatalf("want *SabnzbdDS got %T", ds)
			}
		case "overseerr":
			if _, ok := ds.(*datasource.OverseerrDS); !ok {
				t.Fatalf("want *OverseerrDS got %T", ds)
			}
		case "uptimekuma":
			if _, ok := ds.(*datasource.UptimeKumaDS); !ok {
				t.Fatalf("want *UptimeKumaDS got %T", ds)
			}
		case "speedtest":
			if _, ok := ds.(*datasource.SpeedtestDS); !ok {
				t.Fatalf("want *SpeedtestDS got %T", ds)
			}
		}
	}

	// End-to-end condition check via UptimeKumaDS CurrentState + pure condition evaluation.
	ds, ok := resolveTarget("uptimekuma", uk.ID, client)
	if !ok {
		t.Fatalf("resolve uptimekuma failed")
	}
	sp, ok := ds.(datasource.StateProvider)
	if !ok {
		t.Fatalf("not StateProvider")
	}
	state, err := sp.CurrentState(ctx)
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	// CurrentState must report down > 0 (one DOWN heartbeat).
	downVal, hasDown := state["down"]
	if !hasDown {
		t.Fatalf("state missing 'down' key: %v", state)
	}
	downInt := 0
	switch v := downVal.(type) {
	case int:
		downInt = v
	case float64:
		downInt = int(v)
	default:
		t.Fatalf("down type %T value %v", downVal, downVal)
	}
	if downInt == 0 {
		t.Fatalf("expected down > 0, got state %v", state)
	}
	if state["status"] == "UNKNOWN" {
		t.Fatalf("expected non-UNKNOWN status, got %v", state)
	}

	// Pure condition evaluation: "down gt 0" should be true for this state.
	cond, err := datasource.ParseCondition(`{"path":"down","operator":"gt","value":0}`)
	if err != nil {
		t.Fatalf("ParseCondition: %v", err)
	}
	if !datasource.Evaluate(state, cond) {
		t.Fatalf("Evaluate down gt 0 should be true, state=%v cond=%v", state, cond)
	}
	// Negative: down gt 10 should be false.
	cond2, _ := datasource.ParseCondition(`{"path":"down","operator":"gt","value":10}`)
	if datasource.Evaluate(state, cond2) {
		t.Fatalf("Evaluate down gt 10 should be false, state=%v", state)
	}
}
