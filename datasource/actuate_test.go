package datasource

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHomeAssistantActuate(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		var gotMethod, gotPath, gotAuth, gotCT string
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			gotCT = r.Header.Get("Content-Type")
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			if r.Header.Get("X-API-Key") != "" {
				t.Errorf("unexpected X-API-Key header %q", r.Header.Get("X-API-Key"))
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()
		ds := &HomeAssistantDS{Token: "t", URL: srv.URL}
		if err := ds.Actuate(t.Context(), ActionCallService, map[string]string{
			"domain": "light", "service": "turn_on", "entity_id": "light.kitchen",
		}); err != nil {
			t.Fatalf("Actuate: %v", err)
		}
		if gotMethod != "POST" {
			t.Errorf("method %q", gotMethod)
		}
		if gotPath != "/api/services/light/turn_on" {
			t.Errorf("path %q", gotPath)
		}
		if gotAuth != "Bearer t" {
			t.Errorf("auth %q", gotAuth)
		}
		if gotCT != "application/json" {
			t.Errorf("content-type %q", gotCT)
		}
		if gotBody["entity_id"] != "light.kitchen" {
			t.Errorf("body %v", gotBody)
		}
	})
	t.Run("merged data", func(t *testing.T) {
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			w.WriteHeader(200)
		}))
		defer srv.Close()
		ds := &HomeAssistantDS{Token: "t", URL: srv.URL}
		if err := ds.Actuate(t.Context(), ActionCallService, map[string]string{
			"domain": "light", "service": "turn_on", "entity_id": "light.kitchen",
			"data": `{"brightness":255,"entity_id":"should-not-win"}`,
		}); err != nil {
			t.Fatalf("Actuate: %v", err)
		}
		if gotBody["entity_id"] != "light.kitchen" {
			t.Errorf("entity_id should win, got %v", gotBody["entity_id"])
		}
		// json numbers decoded as float64
		if gotBody["brightness"] != float64(255) {
			t.Errorf("brightness %v", gotBody["brightness"])
		}
	})
	t.Run("missing params", func(t *testing.T) {
		ds := &HomeAssistantDS{Token: "t", URL: "http://x"}
		if err := ds.Actuate(t.Context(), ActionCallService, map[string]string{"domain": "light"}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("unsupported action", func(t *testing.T) {
		ds := &HomeAssistantDS{Token: "t", URL: "http://x"}
		if err := ds.Actuate(t.Context(), ActionPause, map[string]string{}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("domain/service injection rejected", func(t *testing.T) {
		count := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			w.WriteHeader(200)
		}))
		defer srv.Close()
		ds := &HomeAssistantDS{Token: "t", URL: srv.URL}
		for _, p := range []map[string]string{
			{"domain": "light/../config", "service": "turn_on", "entity_id": "x"},
			{"domain": "light", "service": "turn_on/../../x", "entity_id": "x"},
		} {
			if err := ds.Actuate(t.Context(), ActionCallService, p); err == nil {
				t.Fatalf("expected error for %v", p)
			}
		}
		if count != 0 {
			t.Errorf("expected zero requests, got %d", count)
		}
	})
}

func TestQBittorrentActuate(t *testing.T) {
	var loginSeen bool
	var pauseMethod, pausePath, pauseBody string
	var pauseCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			loginSeen = true
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), "username=admin") {
				t.Errorf("login body %q", string(b))
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "abc", Path: "/"})
			w.WriteHeader(200)
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/pause":
			pauseMethod = r.Method
			pausePath = r.URL.Path
			pauseCT = r.Header.Get("Content-Type")
			b, _ := io.ReadAll(r.Body)
			pauseBody = string(b)
			// check cookie
			c, err := r.Cookie("SID")
			if err != nil || c.Value != "abc" {
				t.Errorf("missing SID cookie %v", r.Cookies())
			}
			w.WriteHeader(200)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	ds := &QBittorrentDS{Token: "admin:secret", URL: srv.URL}
	if err := ds.Actuate(t.Context(), ActionPause, map[string]string{}); err != nil {
		t.Fatalf("Actuate: %v", err)
	}
	if !loginSeen {
		t.Fatal("login not seen")
	}
	if pauseMethod != "POST" {
		t.Errorf("method %q", pauseMethod)
	}
	if pausePath != "/api/v2/torrents/pause" {
		t.Errorf("path %q", pausePath)
	}
	if pauseBody != "hashes=all" {
		t.Errorf("body %q", pauseBody)
	}
	if pauseCT != "application/x-www-form-urlencoded" {
		t.Errorf("ct %q", pauseCT)
	}
}

func TestOverseerrActuate(t *testing.T) {
	var gotMethod, gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-API-Key")
		w.WriteHeader(200)
	}))
	defer srv.Close()
	ds := &OverseerrDS{Token: "ovk", URL: srv.URL}
	if err := ds.Actuate(t.Context(), ActionApprove, map[string]string{"request_id": "5"}); err != nil {
		t.Fatalf("Actuate: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method %q", gotMethod)
	}
	if gotPath != "/api/v1/request/5/approve" {
		t.Errorf("path %q", gotPath)
	}
	if gotKey != "ovk" {
		t.Errorf("key %q", gotKey)
	}
	t.Run("invalid request_id", func(t *testing.T) {
		ds2 := &OverseerrDS{Token: "ovk", URL: srv.URL}
		if err := ds2.Actuate(t.Context(), ActionApprove, map[string]string{"request_id": "abc"}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestGenericAPIActuate(t *testing.T) {
	t.Run("POST join", func(t *testing.T) {
		var gotMethod, gotPath string
		var gotBody string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(200)
		}))
		defer srv.Close()
		ds := &GenericAPIDS{Token: "tok", URL: srv.URL, Config: `{"headers":{"X-Custom":"1"}}`}
		if err := ds.Actuate(t.Context(), ActionRequest, map[string]string{"method": "POST", "path": "api/do", "body": `{"x":1}`}); err != nil {
			t.Fatalf("Actuate: %v", err)
		}
		if gotMethod != "POST" {
			t.Errorf("method %q", gotMethod)
		}
		if gotPath != "/api/do" {
			t.Errorf("path %q", gotPath)
		}
		if gotBody != `{"x":1}` {
			t.Errorf("body %q", gotBody)
		}
	})
	t.Run("path traversal rejected", func(t *testing.T) {
		count := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			w.WriteHeader(200)
		}))
		defer srv.Close()
		ds := &GenericAPIDS{Token: "tok", URL: srv.URL}
		err := ds.Actuate(t.Context(), ActionRequest, map[string]string{"method": "POST", "path": "../secret"})
		if err == nil {
			t.Fatal("expected error")
		}
		if count != 0 {
			t.Errorf("expected zero requests, got %d", count)
		}
		// also test :// and //
		if err := ds.Actuate(t.Context(), ActionRequest, map[string]string{"method": "POST", "path": "https://evil.com"}); err == nil {
			t.Fatal("expected error for ://")
		}
		if err := ds.Actuate(t.Context(), ActionRequest, map[string]string{"method": "POST", "path": "//evil"}); err == nil {
			t.Fatal("expected error for //")
		}
		// encoded traversal must also be rejected
		if err := ds.Actuate(t.Context(), ActionRequest, map[string]string{"method": "POST", "path": "/a/%2e%2e/%2e%2e/secret"}); err == nil {
			t.Fatal("expected error for encoded ..")
		}
		if count != 0 {
			t.Errorf("expected zero requests after all rejects, got %d", count)
		}
	})
	t.Run("explicit authorization wins over token", func(t *testing.T) {
		var gotKey, gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotKey = r.Header.Get("X-API-Key")
			gotAuth = r.Header.Get("Authorization")
			w.WriteHeader(200)
		}))
		defer srv.Close()
		ds := &GenericAPIDS{Token: "tok", URL: srv.URL, Config: `{"headers":{"Authorization":"Bearer real"}}`}
		if err := ds.Actuate(t.Context(), ActionRequest, map[string]string{"method": "POST", "path": "/x"}); err != nil {
			t.Fatalf("Actuate: %v", err)
		}
		if gotAuth != "Bearer real" {
			t.Errorf("auth %q", gotAuth)
		}
		if gotKey != "" {
			t.Errorf("X-API-Key should be suppressed, got %q", gotKey)
		}
	})
}
