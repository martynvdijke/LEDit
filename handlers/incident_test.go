package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"

	"ledit/ent"
	"ledit/ent/enttest"
)

// incidentTestServer builds a router with the incident routes wired exactly as
// server.go does, over a per-test in-memory database.
func incidentTestServer(t *testing.T) (*Server, *ent.Client) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() {
		client.Close()
		setIncidentCache(nil)
	})
	ctx := context.Background()
	setIncidentCache(nil)

	r := gin.New()
	srv := &Server{DB: client, Ctx: ctx, Router: r}
	api := r.Group("/api")
	api.POST("/incident", srv.WebhookAuthMiddleware(), srv.APIIncidentIngest)
	api.GET("/incidents", srv.APIIncidentList)
	api.POST("/incidents/:id/resolve", srv.APIIncidentResolve)
	return srv, client
}

func setIncidentCache(active []*ent.Incident) {
	incidentCache.mu.Lock()
	incidentCache.active = active
	incidentCache.mu.Unlock()
}

func postIncident(t *testing.T, srv *Server, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/incident", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	srv.Router.ServeHTTP(w, req)
	return w
}

func decodeCount(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var resp struct {
		ActiveCount int `json:"active_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", w.Body.String(), err)
	}
	return resp.ActiveCount
}

func TestIncidentIngestGenericLifecycle(t *testing.T) {
	srv, _ := incidentTestServer(t)

	w := postIncident(t, srv, `{"title":"DB down","message":"primary unreachable","severity":"critical"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
	}
	if n := decodeCount(t, w); n != 1 {
		t.Fatalf("active_count = %d, want 1", n)
	}
	active := ActiveIncidents()
	if len(active) != 1 || active[0].Severity != "critical" {
		t.Fatalf("unexpected active incidents: %+v", active)
	}

	// Re-firing the same fingerprint refreshes, it does not duplicate.
	if w := postIncident(t, srv, `{"title":"DB down","message":"primary unreachable"}`, nil); decodeCount(t, w) != 1 {
		t.Fatalf("refire duplicated incident: %s", w.Body.String())
	}

	// A resolved status with the same title/message resolves the fingerprint.
	if w := postIncident(t, srv, `{"title":"DB down","message":"primary unreachable","status":"resolved"}`, nil); decodeCount(t, w) != 0 {
		t.Fatalf("resolve failed: %s", w.Body.String())
	}
	if n := len(ActiveIncidents()); n != 0 {
		t.Fatalf("still %d active after resolve", n)
	}
}

func TestIncidentIngestAlertmanagerExpansionAndResolve(t *testing.T) {
	srv, _ := incidentTestServer(t)
	body := `{"alerts":[
		{"status":"firing","fingerprint":"fp-critical","labels":{"alertname":"DiskFull","severity":"critical"},"annotations":{"summary":"disk 98%"}},
		{"status":"firing","fingerprint":"fp-warning","labels":{"alertname":"HighLatency","severity":"warning"},"annotations":{"description":"p99 > 2s"}}
	]}`
	if w := postIncident(t, srv, body, nil); w.Code != http.StatusOK || decodeCount(t, w) != 2 {
		t.Fatalf("alertmanager expansion: %d %s", w.Code, w.Body.String())
	}
	// Severity comes from labels; the critical alert sorts first.
	active := ActiveIncidents()
	if active[0].Fingerprint != "fp-critical" || active[0].Title != "DiskFull" {
		t.Fatalf("unexpected ordering: %+v", active)
	}

	resolve := `{"alerts":[{"status":"resolved","fingerprint":"fp-critical","labels":{"alertname":"DiskFull"}}]}`
	if w := postIncident(t, srv, resolve, nil); decodeCount(t, w) != 1 {
		t.Fatalf("alertmanager resolve: %s", w.Body.String())
	}
	if active := ActiveIncidents(); len(active) != 1 || active[0].Fingerprint != "fp-warning" {
		t.Fatalf("wrong survivor: %+v", active)
	}
}

func TestIncidentIngestTTLBounds(t *testing.T) {
	srv, _ := incidentTestServer(t)
	cases := []struct {
		ttl  int
		want int
	}{
		{10, http.StatusBadRequest},
		{100000, http.StatusBadRequest},
		{60, http.StatusOK},
		{86400, http.StatusOK},
	}
	for _, tc := range cases {
		body := fmt.Sprintf(`{"title":"t","message":"m","ttl_seconds":%d}`, tc.ttl)
		if w := postIncident(t, srv, body, nil); w.Code != tc.want {
			t.Fatalf("ttl %d: got %d want %d (%s)", tc.ttl, w.Code, tc.want, w.Body.String())
		}
	}
}

func TestIncidentExpiryPrunedOnRead(t *testing.T) {
	_, client := incidentTestServer(t)
	ctx := context.Background()
	client.Incident.Create().
		SetFingerprint("expired").
		SetTitle("t").SetMessage("m").SetSeverity("warning").SetSource("test").
		SetActive(true).
		SetCreatedAt(time.Now().Add(-time.Hour)).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		SaveX(ctx)
	reloadIncidents(client)
	if n := len(ActiveIncidents()); n != 0 {
		t.Fatalf("expired incident still active: %d", n)
	}
}

func TestIncidentSceneSeverityAndMore(t *testing.T) {
	_, client := incidentTestServer(t)
	ctx := context.Background()
	mk := func(fp, sev string) *ent.Incident {
		return client.Incident.Create().
			SetFingerprint(fp).SetTitle(fp).SetMessage("m").SetSeverity(sev).SetSource("test").
			SetActive(true).SetCreatedAt(time.Now()).SetExpiresAt(time.Now().Add(time.Hour)).
			SaveX(ctx)
	}
	mk("one", "info")
	mk("two", "critical")
	reloadIncidents(client)

	scene, ok := CurrentIncidentScene()
	if !ok || scene.Severity != "critical" || scene.Title != "two" {
		t.Fatalf("unexpected scene: %+v ok=%v", scene, ok)
	}
	if scene.More != 1 {
		t.Fatalf("More = %d, want 1", scene.More)
	}
}

func TestIncidentIngestWebhookAuth(t *testing.T) {
	srv, client := incidentTestServer(t)
	client.WebhookSettings.Create().SetAPIKey("secret123").SetDefaultTTL(30).SaveX(context.Background())

	if w := postIncident(t, srv, `{"title":"t","message":"m"}`, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing key: got %d want 401", w.Code)
	}
	if w := postIncident(t, srv, `{"title":"t","message":"m"}`, map[string]string{"X-API-Key": "secret123"}); w.Code != http.StatusOK {
		t.Fatalf("valid key: got %d want 200 (%s)", w.Code, w.Body.String())
	}
}

func TestIncidentResolveAPI(t *testing.T) {
	srv, _ := incidentTestServer(t)
	postIncident(t, srv, `{"title":"t","message":"m","severity":"critical"}`, nil)
	active := ActiveIncidents()
	if len(active) != 1 {
		t.Fatalf("setup failed: %d active", len(active))
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/incidents/%d/resolve", active[0].ID), nil)
	srv.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve: %d %s", w.Code, w.Body.String())
	}
	if n := len(ActiveIncidents()); n != 0 {
		t.Fatalf("still %d active", n)
	}
}

func TestIncidentFeedTakeoverThenRotation(t *testing.T) {
	setIncidentCache(nil)
	clearNotifHistory()
	url := overlayTestServer(t, feedConn{})

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	now := time.Now()
	setIncidentCache([]*ent.Incident{{
		ID: 1, Fingerprint: "feed", Title: "OUTAGE", Message: "core down",
		Severity: "critical", Source: "test", Active: true,
		CreatedAt: now, ExpiresAt: now.Add(time.Minute),
	}})

	// The notification tier sits above incidents, so a leftover notification
	// from a sibling test could arrive first; scan until the incident frame.
	msg := readSourceUntil(t, conn, "INCIDENT")
	if msg["image"] == "" {
		t.Fatal("incident frame has no image")
	}

	// Clear the incident; the next slot must fall back to the source rotation.
	setIncidentCache(nil)
	for i := 0; i < 5; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read rotation frame: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if m["source"] != "INCIDENT" {
			return
		}
	}
	t.Fatal("feed never returned to rotation after clearing the incident")
}

// readSourceUntil reads frames until one has the wanted source.
func readSourceUntil(t *testing.T, conn *websocket.Conn, source string) map[string]any {
	t.Helper()
	for i := 0; i < 5; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read %s frame: %v", source, err)
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if msg["source"] == source {
			return msg
		}
	}
	t.Fatalf("no frame with source %s", source)
	return nil
}
