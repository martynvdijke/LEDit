package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func newOTelSettingsRouter(srv *Server) *gin.Engine {
	r := gin.New()
	// Registered without auth middleware: this is the public contract.
	r.GET("/api/otel-settings", srv.PublicOTelSettings)
	return r
}

func getOTelSettings(t *testing.T, r *gin.Engine) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/otel-settings", nil)
	r.ServeHTTP(w, req)

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal %q: %v", w.Body.String(), err)
	}
	return w.Code, body
}

func TestPublicOTelSettingsUnauthenticated(t *testing.T) {
	srv := newTestServerWithDB(t)
	r := newOTelSettingsRouter(srv)

	srv.DB.LogSettings.Create().
		SetOtelEndpoint("https://otel.example.com").
		SetOtelProtocol("http").
		SetOtelEnabled(true).
		SaveX(srv.Ctx)

	code, body := getOTelSettings(t, r)
	if code != 200 {
		t.Fatalf("status %d, want 200", code)
	}
	if body["endpoint"] != "https://otel.example.com" {
		t.Fatalf("endpoint = %v", body["endpoint"])
	}
	for _, key := range []string{"enabled", "traces_enabled"} {
		if body[key] != true {
			t.Fatalf("%s = %v, want true", key, body[key])
		}
	}
	for _, key := range []string{"metrics_enabled", "logs_enabled"} {
		if body[key] != false {
			t.Fatalf("%s = %v, want false", key, body[key])
		}
	}
}

func TestPublicOTelSettingsDisabledWhenNoRow(t *testing.T) {
	srv := newTestServerWithDB(t)
	r := newOTelSettingsRouter(srv)

	code, body := getOTelSettings(t, r)
	if code != 200 {
		t.Fatalf("status %d, want 200", code)
	}
	if body["endpoint"] != "" {
		t.Fatalf("endpoint = %v, want empty", body["endpoint"])
	}
	for _, key := range []string{"enabled", "traces_enabled", "metrics_enabled", "logs_enabled"} {
		if body[key] != false {
			t.Fatalf("%s = %v, want false", key, body[key])
		}
	}
}

func TestPublicOTelSettingsDisabledWhenEndpointEmpty(t *testing.T) {
	srv := newTestServerWithDB(t)
	r := newOTelSettingsRouter(srv)

	srv.DB.LogSettings.Create().
		SetOtelEndpoint("   ").
		SetOtelEnabled(true).
		SaveX(srv.Ctx)

	code, body := getOTelSettings(t, r)
	if code != 200 {
		t.Fatalf("status %d, want 200", code)
	}
	if body["enabled"] != false || body["traces_enabled"] != false {
		t.Fatalf("empty endpoint must disable tracing: %v", body)
	}
}
