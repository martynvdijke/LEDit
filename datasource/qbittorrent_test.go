package datasource

import (
	"bytes"
	"context"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func qBittorrentTestServer(t *testing.T, loginBody *string, cookieCheck func(r *http.Request)) *httptest.Server {
	t.Helper()
	var loginSeen bool
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			loginSeen = true
			b, _ := io.ReadAll(r.Body)
			if loginBody != nil {
				*loginBody = string(b)
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-sid", Path: "/"})
			w.Write([]byte("Ok."))
		case "/api/v2/transfer/info":
			if cookieCheck != nil {
				cookieCheck(r)
			}
			if !loginSeen {
				// ensure login happened but don't fail
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"dl_info_speed":123456,"up_info_speed":65432,"dl_info_data":1000,"up_info_data":2000,"connection_status":"connected"}`))
		case "/api/v2/torrents/info":
			if cookieCheck != nil {
				cookieCheck(r)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"name":"Ubuntu ISO Very Long Name That Exceeds Limit","progress":0.42,"state":"downloading","dlspeed":1000},{"name":"Second Torrent","progress":0.9,"state":"pausedDL","dlspeed":0}]`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestQBittorrentSuccessRender(t *testing.T) {
	srv := qBittorrentTestServer(t, nil, nil)
	defer srv.Close()
	ds := &QBittorrentDS{Token: "user:pass", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestQBittorrentLoginBody(t *testing.T) {
	var body string
	srv := qBittorrentTestServer(t, &body, nil)
	defer srv.Close()
	ds := &QBittorrentDS{Token: "myuser:mypass", URL: srv.URL}
	_, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if !strings.Contains(body, "username=myuser") || !strings.Contains(body, "password=mypass") {
		t.Fatalf("login body %q should contain username/myuser and password/mypass", body)
	}
}

func TestQBittorrentCookieReuse(t *testing.T) {
	var sawCookie bool
	srv := qBittorrentTestServer(t, nil, func(r *http.Request) {
		for _, c := range r.Cookies() {
			if c.Name == "SID" && c.Value == "test-sid" {
				sawCookie = true
			}
		}
		// also check header
		if strings.Contains(r.Header.Get("Cookie"), "SID=test-sid") {
			sawCookie = true
		}
	})
	defer srv.Close()
	ds := &QBittorrentDS{Token: "u:p", URL: srv.URL}
	_, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if !sawCookie {
		t.Fatalf("second request should carry SID cookie")
	}
}

func TestQBittorrentFailedLoginFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			w.Write([]byte("Fails."))
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	ds := &QBittorrentDS{Token: "u:p", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG fallback should not error: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode fallback: %v", err)
	}
}

func TestQBittorrentMalformedTokenFallback(t *testing.T) {
	ds := &QBittorrentDS{Token: "nocolon", URL: "http://example.com"}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestQBittorrent5xxFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "x"})
			w.Write([]byte("Ok."))
			return
		}
		http.Error(w, "err", 500)
	}))
	defer srv.Close()
	ds := &QBittorrentDS{Token: "u:p", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestQBittorrentMalformedJSONFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "sid"})
			w.Write([]byte("Ok."))
			return
		}
		w.Write([]byte("not json"))
	}))
	defer srv.Close()
	ds := &QBittorrentDS{Token: "u:p", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestQBittorrentIdleQueue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "sid"})
			w.Write([]byte("Ok."))
		case "/api/v2/transfer/info":
			w.Write([]byte(`{"dl_info_speed":0,"up_info_speed":0,"dl_info_data":0,"up_info_data":0,"connection_status":"connected"}`))
		case "/api/v2/torrents/info":
			w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	ds := &QBittorrentDS{Token: "u:p", URL: srv.URL}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestQBittorrentCurrentState(t *testing.T) {
	srv := qBittorrentTestServer(t, nil, nil)
	defer srv.Close()
	ds := &QBittorrentDS{Token: "u:p", URL: srv.URL}
	m, err := ds.CurrentState(context.Background())
	if err != nil {
		t.Fatalf("CurrentState: %v", err)
	}
	if m["dl_speed"] != 123456 {
		t.Fatalf("dl_speed %v want 123456", m["dl_speed"])
	}
	if m["total"] != 2 {
		t.Fatalf("total %v want 2", m["total"])
	}
	if m["active"] != 1 {
		t.Fatalf("active %v want 1", m["active"])
	}
}
