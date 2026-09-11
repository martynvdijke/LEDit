package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/enttest"
	"ledit/ent/sports"
)

const sportsLiveESPN = `{"events":[{"id":"1","date":"2025-01-01T18:00:00Z","competitions":[{"competitors":[{"homeAway":"home","team":{"displayName":"Eagles","abbreviation":"PHI"},"score":"21"},{"homeAway":"away","team":{"displayName":"Cowboys","abbreviation":"DAL"},"score":"14"}],"status":{"period":3,"displayClock":"8:23","type":{"state":"in","shortDetail":"Q3 08:23"}}}]}]}`

// TestResolveTarget_SportsLivePin verifies the documented event-rule
// integration: a sports source resolves to a StateProvider and a DisplayRule on
// {"path":"live"} pins the wall while a game is live.
func TestResolveTarget_SportsLivePin(t *testing.T) {
	client := newEventRuleTestDB(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sportsLiveESPN))
	}))
	defer srv.Close()

	sp := client.Sports.Create().
		SetProvider(sports.ProviderEspn).
		SetURL(srv.URL + "/%s/scoreboard").
		SetConfig(`{"leagues":["nfl"]}`).
		SaveX(ctx)
	gs := client.GeneralSettings.GetX(ctx, 1)
	client.GeneralSettings.UpdateOneID(gs.ID).AddSports(sp).ExecX(ctx)

	ds, ok := resolveTarget("sports", sp.ID, client)
	if !ok {
		t.Fatal("sports target not resolved")
	}
	provider, ok := ds.(datasource.StateProvider)
	if !ok {
		t.Fatal("SportsDS must implement StateProvider")
	}
	state, err := provider.CurrentState(ctx)
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if state["live"] != true {
		t.Fatalf("expected live true, got %+v", state)
	}

	ResetNonCapableLogged()
	fc := &FeedController{}
	joinController(fc)
	t.Cleanup(func() { leaveController(fc) })
	rule := client.DisplayRule.Create().
		SetName("live").
		SetEnabled(true).
		SetSourceType("sports").
		SetSourceID(sp.ID).
		SetCondition(`{"path":"live","operator":"eq","value":true}`).
		SetCheckIntervalSeconds(5).
		SetCooldownSeconds(0).
		SaveX(ctx)
	states := map[int]*ruleState{rule.ID: {rule: rule}}
	EvaluateRulesOnce(client, states)
	if _, _, ok := fc.IsPinned(); !ok {
		t.Fatal("expected wall pinned during a live game")
	}
}

func sportsTestServer(t *testing.T) (*Server, *http.Cookie) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	ctx := context.Background()
	if _, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	h, _ := bcrypt.GenerateFromPassword([]byte("ledit"), bcrypt.DefaultCost)
	if _, err := client.AdminSettings.Create().SetUsername("admin").SetPasswordHash(string(h)).Save(ctx); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	EnableAuth()
	srv := &Server{DB: client, Ctx: ctx, Router: gin.New()}
	tmpl := template.New("")
	template.Must(tmpl.New("sports_form.html").Parse("ok"))
	template.Must(tmpl.New("sports.html").Parse("ok"))
	srv.Router.SetHTMLTemplate(tmpl)
	srv.Router.POST("/login", srv.LoginAction)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader([]byte("username=admin&password=ledit")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.ServeHTTP(w, req)
	var sess *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			sess = c
			break
		}
	}
	if sess == nil {
		t.Fatalf("no session: %d %s", w.Code, w.Body.String())
	}
	admin := srv.Router.Group("/admin")
	admin.Use(AuthMiddleware())
	{
		admin.GET("/api/sports", srv.APISportsList)
		admin.GET("/api/sports/:id", srv.APISportsGet)
		admin.POST("/api/sports", srv.APISportsCreate)
		admin.PUT("/api/sports/:id", srv.APISportsUpdate)
		admin.DELETE("/api/sports/:id", srv.APISportsDelete)
		admin.GET("/sports", srv.AdminSportsList)
		admin.GET("/datasources/sports/new", srv.AdminSportsNew)
		admin.POST("/datasources/sports/new", srv.AdminSportsCreate)
		admin.GET("/datasources/sports/:id/edit", srv.AdminSportsEdit)
		admin.POST("/datasources/sports/:id/edit", srv.AdminSportsUpdate)
		admin.POST("/datasources/sports/:id/delete", srv.AdminSportsDelete)
	}
	return srv, sess
}

func sportsJSONRequest(srv *Server, method, path string, sess *http.Cookie, body any) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sess != nil {
		req.AddCookie(sess)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func validSportsBody() map[string]any {
	return map[string]any{
		"token":                "secret-key",
		"provider":             "espn",
		"config":               `{"leagues":["nfl"]}`,
		"live_refresh_seconds": 15,
		"idle_refresh_seconds": 60,
	}
}

func TestAPISportsCreate201(t *testing.T) {
	srv, sess := sportsTestServer(t)
	w := sportsJSONRequest(srv, http.MethodPost, "/admin/api/sports", sess, validSportsBody())
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d body %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if jsonID(created) == "" || jsonID(created) == "0" {
		t.Fatalf("no id in %s", w.Body.String())
	}
}

func TestAPISportsInvalidProvider400(t *testing.T) {
	srv, sess := sportsTestServer(t)
	body := validSportsBody()
	body["provider"] = "bogus"
	w := sportsJSONRequest(srv, http.MethodPost, "/admin/api/sports", sess, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestAPISportsInvalidConfig400(t *testing.T) {
	srv, sess := sportsTestServer(t)
	for _, cfg := range []string{`{`, `{}`} {
		body := validSportsBody()
		body["config"] = cfg
		w := sportsJSONRequest(srv, http.MethodPost, "/admin/api/sports", sess, body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("config %q expected 400 got %d %s", cfg, w.Code, w.Body.String())
		}
	}
}

func TestAPISportsIntervalsTooLow400(t *testing.T) {
	srv, sess := sportsTestServer(t)
	body := validSportsBody()
	body["live_refresh_seconds"] = 5
	w := sportsJSONRequest(srv, http.MethodPost, "/admin/api/sports", sess, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("live floor expected 400 got %d %s", w.Code, w.Body.String())
	}
	body = validSportsBody()
	body["idle_refresh_seconds"] = 30
	w = sportsJSONRequest(srv, http.MethodPost, "/admin/api/sports", sess, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("idle floor expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestAPISportsListOmitsToken(t *testing.T) {
	srv, sess := sportsTestServer(t)
	if w := sportsJSONRequest(srv, http.MethodPost, "/admin/api/sports", sess, validSportsBody()); w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	w := sportsJSONRequest(srv, http.MethodGet, "/admin/api/sports", sess, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret-key") || strings.Contains(w.Body.String(), "Token") {
		t.Fatalf("list leaked API key: %s", w.Body.String())
	}
}

func TestAPISportsUpdateKeepsKeyAndDelete(t *testing.T) {
	srv, sess := sportsTestServer(t)
	w := sportsJSONRequest(srv, http.MethodPost, "/admin/api/sports", sess, validSportsBody())
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id := jsonID(created)

	upd := map[string]any{"provider": "espn", "config": `{"leagues":["nba"]}`}
	w = sportsJSONRequest(srv, http.MethodPut, "/admin/api/sports/"+id, sess, upd)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "secret-key") {
		t.Fatalf("update should keep existing key: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "nba") {
		t.Fatalf("update did not apply config: %s", w.Body.String())
	}

	w = sportsJSONRequest(srv, http.MethodDelete, "/admin/api/sports/"+id, sess, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	w = sportsJSONRequest(srv, http.MethodGet, "/admin/api/sports/"+id, sess, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete got %d", w.Code)
	}
}

func TestAPISportsUnauthenticated(t *testing.T) {
	srv, _ := sportsTestServer(t)
	w := sportsJSONRequest(srv, http.MethodPost, "/admin/api/sports", nil, validSportsBody())
	if w.Code != http.StatusFound && w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 302 or 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestAdminSportsNewFormRenders(t *testing.T) {
	srv, sess := sportsTestServer(t)
	w := sportsJSONRequest(srv, http.MethodGet, "/admin/datasources/sports/new", sess, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("new form: %d", w.Code)
	}
}

func TestAdminSportsFormTemplateFields(t *testing.T) {
	data, err := os.ReadFile("../web/templates/admin/sports_form.html")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	html := string(data)
	for _, want := range []string{
		`name="token"`, `name="url"`, `name="provider"`, `name="config"`,
		`name="live_refresh_seconds"`, `name="idle_refresh_seconds"`,
		`/admin/datasources/sports`, "leagues", "teams", "fixtures",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("sports_form.html missing %q", want)
		}
	}
}

func TestAdminSportsFormTemplateExecutes(t *testing.T) {
	tmpl := template.New("root")
	template.Must(tmpl.New("sidebar").Parse(`{{define "sidebar"}}sidebar{{end}}`))
	tmpl, err := tmpl.ParseFiles("../web/templates/admin/sports_form.html")
	if err != nil {
		t.Fatalf("parse sports_form.html: %v", err)
	}
	cases := []gin.H{
		{},
		{"obj": sportsInput{Provider: "espn", Config: `{"leagues":["nfl"]}`, LiveRefreshSeconds: 30, IdleRefreshSeconds: 300}, "edit": true, "id": 5},
		{"obj": &ent.Sports{ID: 1, Provider: sports.ProviderEspn, Config: `{"teams":["PHI"]}`, LiveRefreshSeconds: 30, IdleRefreshSeconds: 300}, "edit": true, "id": 1},
	}
	for i, data := range cases {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "sports_form.html", data); err != nil {
			t.Fatalf("execute case %d (%T): %v", i, data["obj"], err)
		}
	}
}

func TestAdminSportsListTemplateExecutes(t *testing.T) {
	tmpl := template.New("root")
	template.Must(tmpl.New("sidebar").Parse(`{{define "sidebar"}}sidebar{{end}}`))
	tmpl, err := tmpl.ParseFiles("../web/templates/admin/sports.html")
	if err != nil {
		t.Fatalf("parse sports.html: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "sports.html", gin.H{"sports": []*ent.Sports{{ID: 1, Provider: sports.ProviderEspn, Config: `{"leagues":["nfl"]}`}}}); err != nil {
		t.Fatalf("execute sports.html: %v", err)
	}
}
