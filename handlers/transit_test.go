package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"ledit/ent"
	"ledit/ent/enttest"
)

func transitTestServer(t *testing.T) (*Server, *http.Cookie) {
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
	template.Must(tmpl.New("transit_form.html").Parse("ok"))
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
		admin.GET("/api/transit", srv.APITransitList)
		admin.GET("/api/transit/:id", srv.APITransitGet)
		admin.POST("/api/transit", srv.APITransitCreate)
		admin.PUT("/api/transit/:id", srv.APITransitUpdate)
		admin.DELETE("/api/transit/:id", srv.APITransitDelete)
		admin.GET("/datasources/transit/new", srv.AdminTransitNew)
		admin.POST("/datasources/transit/new", srv.AdminTransitCreate)
		admin.GET("/datasources/transit/:id/edit", srv.AdminTransitEdit)
		admin.POST("/datasources/transit/:id/edit", srv.AdminTransitUpdate)
		admin.POST("/datasources/transit/:id/delete", srv.AdminTransitDelete)
	}
	return srv, sess
}

func transitJSONRequest(srv *Server, method, path string, sess *http.Cookie, body any) *httptest.ResponseRecorder {
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

func createTransit(t *testing.T, srv *Server, sess *http.Cookie, body map[string]any) map[string]any {
	t.Helper()
	w := transitJSONRequest(srv, http.MethodPost, "/admin/api/transit", sess, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201 got %d body %s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return created
}

func TestAPITransitCreateDefaults201(t *testing.T) {
	srv, sess := transitTestServer(t)
	created := createTransit(t, srv, sess, map[string]any{"token": "900000003201"})
	id := jsonID(created)
	if id == "" || id == "0" {
		t.Fatalf("no id in %s", created)
	}

	w := transitJSONRequest(srv, http.MethodGet, "/admin/api/transit/"+id, sess, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["provider"] != "vbb" {
		t.Errorf("provider = %v want vbb", got["provider"])
	}
	if got["max_departures"] != float64(4) {
		t.Errorf("max_departures = %v want 4", got["max_departures"])
	}
	if got["walk_time_min"] != nil && got["walk_time_min"] != float64(0) {
		t.Errorf("walk_time_min = %v want 0", got["walk_time_min"])
	}
	if got["time_mode"] != "minutes" {
		t.Errorf("time_mode = %v want minutes", got["time_mode"])
	}
	if got["timezone"] != "Europe/Berlin" {
		t.Errorf("timezone = %v want Europe/Berlin", got["timezone"])
	}
}

func TestAPITransitCreateCustomProviderAndKey(t *testing.T) {
	srv, sess := transitTestServer(t)
	created := createTransit(t, srv, sess, map[string]any{
		"token":          "9300",
		"url":            "https://transit.land/api/v2/rest/stops/%s/departures",
		"api_key":        "secret-key",
		"provider":       "transitland",
		"max_departures": 6,
		"route_filter":   "S7, U1",
		"walk_time_min":  3,
		"timezone":       "Europe/Paris",
		"time_mode":      "clock",
	})
	id := jsonID(created)

	w := transitJSONRequest(srv, http.MethodGet, "/admin/api/transit/"+id, sess, nil)
	body := w.Body.String()
	for _, want := range []string{"transitland", "secret-key", "S7, U1", "Europe/Paris", "clock"} {
		if !strings.Contains(body, want) {
			t.Errorf("get response missing %q: %s", want, body)
		}
	}
}

func TestAPITransitInvalid400(t *testing.T) {
	srv, sess := transitTestServer(t)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing stop id", map[string]any{}},
		{"max too high", map[string]any{"token": "1", "max_departures": 9}},
		{"max too low", map[string]any{"token": "1", "max_departures": 0}},
		{"walk negative", map[string]any{"token": "1", "walk_time_min": -1}},
		{"walk too high", map[string]any{"token": "1", "walk_time_min": 61}},
		{"bad timezone", map[string]any{"token": "1", "timezone": "Mars/Olympus"}},
		{"bad provider", map[string]any{"token": "1", "provider": "bogus"}},
		{"bad time mode", map[string]any{"token": "1", "time_mode": "fuzzy"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := transitJSONRequest(srv, http.MethodPost, "/admin/api/transit", sess, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAPITransitUnauthenticated(t *testing.T) {
	srv, _ := transitTestServer(t)
	w := transitJSONRequest(srv, http.MethodPost, "/admin/api/transit", nil, map[string]any{"token": "1"})
	if w.Code != http.StatusFound && w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 302 or 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestAPITransitUpdateKeepsKeyAndDelete(t *testing.T) {
	srv, sess := transitTestServer(t)
	created := createTransit(t, srv, sess, map[string]any{
		"token": "100", "api_key": "keep-me", "provider": "custom", "url": "http://old.test/%s",
	})
	id := jsonID(created)

	// Update without api_key: it must be preserved.
	w := transitJSONRequest(srv, http.MethodPut, "/admin/api/transit/"+id, sess, map[string]any{
		"token": "100", "provider": "custom", "url": "http://new.test/%s", "timezone": "UTC",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "keep-me") {
		t.Fatalf("api key was not preserved: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "http://new.test/%s") {
		t.Fatalf("url not updated: %s", w.Body.String())
	}

	w = transitJSONRequest(srv, http.MethodDelete, "/admin/api/transit/"+id, sess, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	w = transitJSONRequest(srv, http.MethodGet, "/admin/api/transit/"+id, sess, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete got %d", w.Code)
	}
}

func TestAdminTransitFormCreateAndValidation(t *testing.T) {
	srv, sess := transitTestServer(t)

	// New form renders.
	req := httptest.NewRequest(http.MethodGet, "/admin/datasources/transit/new", nil)
	req.AddCookie(sess)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("new form: %d %s", w.Code, w.Body.String())
	}

	// Valid form create with defaults redirects and persists defaults.
	form := url.Values{"token": {"900000003201"}, "provider": {"vbb"}, "max_departures": {"4"}, "walk_time_min": {"0"}, "timezone": {"Europe/Berlin"}, "time_mode": {"minutes"}}
	req = httptest.NewRequest(http.MethodPost, "/admin/datasources/transit/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sess)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("form create: expected 302 got %d %s", w.Code, w.Body.String())
	}
	row := srv.DB.Transit.Query().FirstX(srv.Ctx)
	if row.Provider != "vbb" || row.MaxDepartures != 4 || row.WalkTimeMin != 0 || row.TimeMode != "minutes" || row.Timezone != "Europe/Berlin" {
		t.Fatalf("defaults not persisted: %+v", row)
	}

	// Missing stop id is rejected with 400.
	bad := url.Values{"provider": {"vbb"}, "max_departures": {"4"}, "timezone": {"Europe/Berlin"}, "time_mode": {"minutes"}}
	req = httptest.NewRequest(http.MethodPost, "/admin/datasources/transit/new", strings.NewReader(bad.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(sess)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid form: expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestAdminTransitFormTemplateFields(t *testing.T) {
	data, err := os.ReadFile("../web/templates/admin/transit_form.html")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	html := string(data)
	for _, want := range []string{
		`name="token"`, `name="url"`, `name="api_key"`, `name="provider"`,
		`name="max_departures"`, `name="route_filter"`, `name="walk_time_min"`,
		`name="timezone"`, `name="time_mode"`,
		`/admin/datasources/transit`, `data-live-preview-form`, `data-preview-type="transit"`,
		`data-live-preview-img`, `Europe/Berlin`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("transit_form.html missing %q", want)
		}
	}
}

func TestAdminTransitFormTemplateExecutes(t *testing.T) {
	tmpl := template.New("root")
	template.Must(tmpl.New("sidebar").Parse(`{{define "sidebar"}}sidebar{{end}}`))
	tmpl, err := tmpl.ParseFiles("../web/templates/admin/transit_form.html")
	if err != nil {
		t.Fatalf("parse transit_form.html: %v", err)
	}
	for _, obj := range []any{
		nil,
		transitInput{Token: "900", URL: "http://x/%s", Provider: "transitland", MaxDepartures: 6, WalkTimeMin: 3, Timezone: "Europe/Paris", TimeMode: "clock"},
	} {
		var buf bytes.Buffer
		data := gin.H{}
		if obj != nil {
			data["obj"] = obj
		}
		if err := tmpl.ExecuteTemplate(&buf, "transit_form.html", data); err != nil {
			t.Fatalf("execute transit_form.html (%T): %v", obj, err)
		}
	}
}
