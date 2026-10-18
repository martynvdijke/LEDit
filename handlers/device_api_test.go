package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newDeviceAPITestServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerWithDB(t)
}

// newDeviceAPIRouter mirrors the route registration in server.go without the
// role middleware: the middleware itself is covered by middleware tests.
func newDeviceAPIRouter(srv *Server) *gin.Engine {
	r := gin.New()
	r.GET("/api/devices", srv.APIDeviceList)
	r.GET("/api/playlists", srv.APIPlaylistList)
	r.GET("/api/scenes", srv.APISceneList)
	r.GET("/api/images", srv.APIImageList)
	r.POST("/api/devices/:id/brightness", srv.APIDeviceBrightness)
	r.POST("/api/devices/:id/select", srv.APIDeviceSelect)
	r.POST("/api/devices/:id/message", srv.APIDeviceMessage)
	return r
}

func doJSON(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestAPIDeviceListReportsFleet(t *testing.T) {
	srv := newDeviceAPITestServer(t)
	r := newDeviceAPIRouter(srv)

	grp := srv.DB.DeviceGroup.Create().SetName("Kitchen").SaveX(srv.Ctx)
	dev := srv.DB.DeviceSettings.Create().
		SetName("Matrix").
		SetWidth(64).SetHeight(32).
		SetEnabled(true).
		SetTransport("websocket").
		SetBrightnessOverride(42).
		SetGroupID(grp.ID).
		SaveX(srv.Ctx)

	w := doJSON(t, r, "GET", "/api/devices", "")
	if w.Code != 200 {
		t.Fatalf("devices list %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Devices []struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			Width        int    `json:"width"`
			Height       int    `json:"height"`
			Brightness   int    `json:"brightness"`
			Liveness     string `json:"liveness"`
			GroupName    string `json:"group_name"`
			ContentMode  string `json:"content_mode"`
			PlaylistName string `json:"playlist_name"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Devices) != 1 {
		t.Fatalf("want 1 device, got %d", len(resp.Devices))
	}
	d := resp.Devices[0]
	if d.ID != dev.ID || d.Name != "Matrix" || d.Width != 64 || d.Height != 32 {
		t.Fatalf("unexpected device payload %+v", d)
	}
	if d.Brightness != 42 {
		t.Fatalf("brightness = %d, want persisted override 42", d.Brightness)
	}
	if d.GroupName != "Kitchen" {
		t.Fatalf("group name = %q, want Kitchen", d.GroupName)
	}
	if d.Liveness != "never" {
		t.Fatalf("liveness = %q, want never for a device that never connected", d.Liveness)
	}
}

func TestAPIPlaylistAndSceneLists(t *testing.T) {
	srv := newDeviceAPITestServer(t)
	r := newDeviceAPIRouter(srv)

	srv.DB.Playlist.Create().SetName("Morning").SetEnabled(true).
		SetItems(`[{"source_type":"clock","source_id":1},{"source_type":"clock","source_id":2}]`).SaveX(srv.Ctx)
	srv.DB.Playlist.Create().SetName("Off").SetEnabled(false).SaveX(srv.Ctx)
	srv.DB.Scene.Create().SetName("Sunset").SetEnabled(true).SaveX(srv.Ctx)

	w := doJSON(t, r, "GET", "/api/playlists", "")
	if w.Code != 200 {
		t.Fatalf("playlists %d %s", w.Code, w.Body.String())
	}
	var pls struct {
		Playlists []struct {
			Name      string `json:"name"`
			Enabled   bool   `json:"enabled"`
			ItemCount int    `json:"item_count"`
		} `json:"playlists"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pls); err != nil {
		t.Fatalf("unmarshal playlists: %v", err)
	}
	if len(pls.Playlists) != 2 || pls.Playlists[0].ItemCount != 2 || pls.Playlists[1].Enabled {
		t.Fatalf("unexpected playlists %+v", pls.Playlists)
	}

	w = doJSON(t, r, "GET", "/api/scenes", "")
	if w.Code != 200 {
		t.Fatalf("scenes %d %s", w.Code, w.Body.String())
	}
	var scs struct {
		Scenes []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
			Active  bool   `json:"active"`
		} `json:"scenes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &scs); err != nil {
		t.Fatalf("unmarshal scenes: %v", err)
	}
	if len(scs.Scenes) != 1 || scs.Scenes[0].Name != "Sunset" || scs.Scenes[0].Active {
		t.Fatalf("unexpected scenes %+v", scs.Scenes)
	}
}

func TestAPIDeviceBrightnessValidatesAndPersists(t *testing.T) {
	srv := newDeviceAPITestServer(t)
	r := newDeviceAPIRouter(srv)
	dev := srv.DB.DeviceSettings.Create().SetName("Matrix").SetTransport("websocket").SetEnabled(true).SaveX(srv.Ctx)

	// Missing level.
	if w := doJSON(t, r, "POST", "/api/devices/1/brightness", `{}`); w.Code != 400 {
		t.Fatalf("missing level: want 400, got %d %s", w.Code, w.Body.String())
	}
	// Out of range.
	if w := doJSON(t, r, "POST", "/api/devices/1/brightness", `{"level":101}`); w.Code != 400 {
		t.Fatalf("level 101: want 400, got %d", w.Code)
	}
	// Unknown device.
	if w := doJSON(t, r, "POST", "/api/devices/999/brightness", `{"level":50}`); w.Code != 404 {
		t.Fatalf("unknown device: want 404, got %d", w.Code)
	}
	// Non-numeric id.
	if w := doJSON(t, r, "POST", "/api/devices/abc/brightness", `{"level":50}`); w.Code != 400 {
		t.Fatalf("bad id: want 400, got %d", w.Code)
	}

	w := doJSON(t, r, "POST", "/api/devices/"+itoa(dev.ID)+"/brightness", `{"level":30}`)
	if w.Code != 200 {
		t.Fatalf("set brightness %d %s", w.Code, w.Body.String())
	}
	got, err := srv.DB.DeviceSettings.Get(srv.Ctx, dev.ID)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if got.BrightnessOverride == nil || *got.BrightnessOverride != 30 {
		t.Fatalf("brightness override = %v, want 30", got.BrightnessOverride)
	}
}

func TestAPIDeviceSelectValidation(t *testing.T) {
	srv := newDeviceAPITestServer(t)
	r := newDeviceAPIRouter(srv)
	dev := srv.DB.DeviceSettings.Create().SetName("Matrix").SetTransport("websocket").SetEnabled(true).SaveX(srv.Ctx)
	pl := srv.DB.Playlist.Create().SetName("Morning").SetEnabled(true).SaveX(srv.Ctx)

	if w := doJSON(t, r, "POST", "/api/devices/1/select", `{}`); w.Code != 400 {
		t.Fatalf("missing target: want 400, got %d", w.Code)
	}
	if w := doJSON(t, r, "POST", "/api/devices/1/select", `{"target":"garbage"}`); w.Code != 400 {
		t.Fatalf("bad grammar: want 400, got %d", w.Code)
	}
	if w := doJSON(t, r, "POST", "/api/devices/999/select", `{"target":"playlist:1"}`); w.Code != 404 {
		t.Fatalf("unknown device: want 404, got %d", w.Code)
	}
	if w := doJSON(t, r, "POST", "/api/devices/1/select", `{"target":"playlist:999"}`); w.Code != 400 {
		t.Fatalf("unknown playlist: want 400, got %d", w.Code)
	}

	w := doJSON(t, r, "POST", "/api/devices/"+itoa(dev.ID)+"/select", `{"target":"playlist:`+itoa(pl.ID)+`"}`)
	if w.Code != 200 {
		t.Fatalf("select playlist %d %s", w.Code, w.Body.String())
	}
	got, err := srv.DB.DeviceSettings.Get(srv.Ctx, dev.ID)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if got.ContentMode != "playlist" || got.PlaylistID == nil || *got.PlaylistID != pl.ID {
		t.Fatalf("content binding = %q/%v, want playlist %d", got.ContentMode, got.PlaylistID, pl.ID)
	}
}

func TestAPIDeviceMessageValidation(t *testing.T) {
	srv := newDeviceAPITestServer(t)
	r := newDeviceAPIRouter(srv)
	srv.DB.DeviceSettings.Create().SetName("Matrix").SetTransport("websocket").SetEnabled(true).SaveX(srv.Ctx)

	if w := doJSON(t, r, "POST", "/api/devices/1/message", `{}`); w.Code != 400 {
		t.Fatalf("missing text: want 400, got %d", w.Code)
	}
	if w := doJSON(t, r, "POST", "/api/devices/999/message", `{"text":"hi"}`); w.Code != 404 {
		t.Fatalf("unknown device: want 404, got %d", w.Code)
	}
	// Offline device: accepted but silently a no-op (no live feed).
	if w := doJSON(t, r, "POST", "/api/devices/1/message", `{"text":"hi"}`); w.Code != 200 {
		t.Fatalf("offline message: want 200, got %d %s", w.Code, w.Body.String())
	}
	// Over-length text is rejected.
	long := strings.Repeat("x", 256)
	if w := doJSON(t, r, "POST", "/api/devices/1/message", `{"text":"`+long+`"}`); w.Code != 400 {
		t.Fatalf("over-length message: want 400, got %d", w.Code)
	}
}

func TestAPIImageList(t *testing.T) {
	srv := newDeviceAPITestServer(t)
	r := newDeviceAPIRouter(srv)
	srv.DB.Image.Create().SetPath("web/media/custom_images/a.png").SaveX(srv.Ctx)

	w := doJSON(t, r, "GET", "/api/images", "")
	if w.Code != 200 {
		t.Fatalf("images %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Images []struct {
			Path string `json:"path"`
		} `json:"images"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal images: %v", err)
	}
	if len(resp.Images) != 1 || resp.Images[0].Path == "" {
		t.Fatalf("unexpected images %+v", resp.Images)
	}
}
