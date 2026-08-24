package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func qrcodeTestServer(t *testing.T) (*Server, *http.Cookie) {
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
	// seed
	if _, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// admin
	if _, err := client.AdminSettings.Query().First(ctx); err != nil {
		h, _ := bcrypt.GenerateFromPassword([]byte("ledit"), bcrypt.DefaultCost)
		if _, err := client.AdminSettings.Create().SetUsername("admin").SetPasswordHash(string(h)).Save(ctx); err != nil {
			t.Fatalf("create admin: %v", err)
		}
	}
	EnableAuth()
	srv := &Server{DB: client, Ctx: ctx, Router: gin.New()}
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
	// admin routes
	admin := srv.Router.Group("/admin")
	admin.Use(AuthMiddleware())
	{
		admin.GET("/api/qrcodes", srv.APIQrcodeList)
		admin.GET("/api/qrcodes/:id", srv.APIQrcodeGet)
		admin.POST("/api/qrcodes", srv.APIQrcodeCreate)
		admin.PUT("/api/qrcodes/:id", srv.APIQrcodeUpdate)
		admin.DELETE("/api/qrcodes/:id", srv.APIQrcodeDelete)
	}
	return srv, sess
}

func qrcodeJSONRequest(srv *Server, method, path string, sess *http.Cookie, body any) *httptest.ResponseRecorder {
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

func TestAPIQrcodeCreateText201(t *testing.T) {
	srv, sess := qrcodeTestServer(t)
	body := map[string]any{"content": "hello world", "mode": "text", "error_correction": "M", "quiet_zone": 4}
	w := qrcodeJSONRequest(srv, http.MethodPost, "/admin/api/qrcodes", sess, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d body %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["ID"] == nil && resp["id"] == nil {
		t.Fatalf("expected id in response %s", w.Body.String())
	}
}

func TestAPIQrcodeCreateWifi201(t *testing.T) {
	srv, sess := qrcodeTestServer(t)
	body := map[string]any{"content": "wifi", "mode": "wifi", "wifi_ssid": "MyWifi", "wifi_password": "secret123", "wifi_auth": "WPA", "error_correction": "M", "quiet_zone": 4}
	w := qrcodeJSONRequest(srv, http.MethodPost, "/admin/api/qrcodes", sess, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 got %d %s", w.Code, w.Body.String())
	}
	// verify payload via GET
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	id := fmt.Sprintf("%.0f", created["ID"])
	if id == "%!f(<nil>)" || id == "0" {
		// try id field
		if v, ok := created["id"]; ok {
			id = fmt.Sprintf("%v", v)
		}
	}
	// fallback: list
	if id == "" || id == "0" || strings.Contains(id, "%!") {
		w2 := qrcodeJSONRequest(srv, http.MethodGet, "/admin/api/qrcodes", sess, nil)
		var list []map[string]any
		_ = json.Unmarshal(w2.Body.Bytes(), &list)
		if len(list) > 0 {
			id = fmt.Sprintf("%v", list[0]["ID"])
			if id == "<nil>" {
				id = fmt.Sprintf("%v", list[0]["id"])
			}
		}
	}
	if id != "" && id != "<nil>" && id != "0" {
		w3 := qrcodeJSONRequest(srv, http.MethodGet, "/admin/api/qrcodes/"+id, sess, nil)
		if w3.Code != http.StatusOK {
			t.Fatalf("GET wifi qr: %d %s", w3.Code, w3.Body.String())
		}
		if !strings.Contains(w3.Body.String(), "WIFI:T:WPA") {
			t.Fatalf("payload not wifi formatted: %s", w3.Body.String())
		}
	}
}

func TestAPIQrcodeEmptyContent400(t *testing.T) {
	srv, sess := qrcodeTestServer(t)
	body := map[string]any{"content": "", "mode": "text", "error_correction": "M", "quiet_zone": 4}
	w := qrcodeJSONRequest(srv, http.MethodPost, "/admin/api/qrcodes", sess, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestAPIQrcodeWifiWithoutSSID400(t *testing.T) {
	srv, sess := qrcodeTestServer(t)
	body := map[string]any{"content": "x", "mode": "wifi", "wifi_ssid": "", "wifi_auth": "WPA", "wifi_password": "pw", "error_correction": "M", "quiet_zone": 4}
	w := qrcodeJSONRequest(srv, http.MethodPost, "/admin/api/qrcodes", sess, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestAPIQrcodeWPAWithoutPassword400(t *testing.T) {
	srv, sess := qrcodeTestServer(t)
	body := map[string]any{"content": "x", "mode": "wifi", "wifi_ssid": "MyWifi", "wifi_auth": "WPA", "wifi_password": "", "error_correction": "M", "quiet_zone": 4}
	w := qrcodeJSONRequest(srv, http.MethodPost, "/admin/api/qrcodes", sess, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestAPIQrcodeContentTooLongForECCH400(t *testing.T) {
	srv, sess := qrcodeTestServer(t)
	long := strings.Repeat("€", 500)
	body := map[string]any{"content": long, "mode": "text", "error_correction": "H", "quiet_zone": 4}
	w := qrcodeJSONRequest(srv, http.MethodPost, "/admin/api/qrcodes", sess, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for ECC H too long, got %d %s", w.Code, w.Body.String())
	}
}

func TestAPIQrcodeUnauthenticated401(t *testing.T) {
	srv, _ := qrcodeTestServer(t)
	body := map[string]any{"content": "hello", "mode": "text", "error_correction": "M", "quiet_zone": 4}
	w := qrcodeJSONRequest(srv, http.MethodPost, "/admin/api/qrcodes", nil, body)
	// AuthMiddleware redirects to /login (302) when unauthenticated; accept 302 or 401
	if w.Code != http.StatusFound && w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 302 or 401 got %d %s", w.Code, w.Body.String())
	}
}

func TestAPIQrcodeUpdateDeleteRoundTrip(t *testing.T) {
	srv, sess := qrcodeTestServer(t)
	body := map[string]any{"content": "initial", "mode": "text", "error_correction": "M", "quiet_zone": 4}
	w := qrcodeJSONRequest(srv, http.MethodPost, "/admin/api/qrcodes", sess, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	var id string
	if v, ok := created["ID"]; ok && v != nil {
		id = fmt.Sprintf("%v", v)
	} else if v, ok := created["id"]; ok {
		id = fmt.Sprintf("%v", v)
	}
	if id == "" || id == "<nil>" {
		// get from list
		w2 := qrcodeJSONRequest(srv, http.MethodGet, "/admin/api/qrcodes", sess, nil)
		var list []map[string]any
		_ = json.Unmarshal(w2.Body.Bytes(), &list)
		if len(list) == 0 {
			t.Fatal("no list items")
		}
		id = fmt.Sprintf("%v", list[0]["ID"])
		if id == "<nil>" {
			id = fmt.Sprintf("%v", list[0]["id"])
		}
	}
	// update
	upd := map[string]any{"content": "updated", "mode": "text", "error_correction": "M", "quiet_zone": 4}
	w = qrcodeJSONRequest(srv, http.MethodPut, "/admin/api/qrcodes/"+id, sess, upd)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	// get and verify
	w = qrcodeJSONRequest(srv, http.MethodGet, "/admin/api/qrcodes/"+id, sess, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get after update: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "updated") {
		t.Fatalf("expected updated content, got %s", w.Body.String())
	}
	// delete
	w = qrcodeJSONRequest(srv, http.MethodDelete, "/admin/api/qrcodes/"+id, sess, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	w = qrcodeJSONRequest(srv, http.MethodGet, "/admin/api/qrcodes/"+id, sess, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete got %d", w.Code)
	}
}
