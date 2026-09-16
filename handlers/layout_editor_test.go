package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"ledit/datasource"
	"ledit/ent/composition"
)

func TestValidateRegionConfig(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		w, h    int
		rows    int
		cols    int
		gap     int
		pad     int
		wantErr bool
	}{
		{"empty ok", "[]", 64, 64, 2, 2, 2, 0, false},
		{"malformed", "{not json", 64, 64, 2, 2, 2, 0, true},
		{"valid absolute", `[{"id":"a","x":0,"y":0,"w":32,"h":64,"source_type":"clock","source_id":0}]`, 64, 64, 2, 2, 2, 0, false},
		{"out of bounds", `[{"id":"a","x":40,"y":0,"w":32,"h":64}]`, 64, 64, 2, 2, 2, 0, true},
		{"too small", `[{"id":"a","x":0,"y":0,"w":1,"h":64}]`, 64, 64, 2, 2, 2, 0, true},
		{"missing dimension", `[{"id":"a","x":0,"y":0,"w":32,"h":0}]`, 64, 64, 2, 2, 2, 0, true},
		{"negative position", `[{"id":"a","x":-1,"y":0,"w":32,"h":32}]`, 64, 64, 2, 2, 2, 0, true},
		{"unknown source type", `[{"id":"a","x":0,"y":0,"w":32,"h":32,"source_type":"nope","source_id":1}]`, 64, 64, 2, 2, 2, 0, true},
		{"composition child ok", `[{"id":"a","x":0,"y":0,"w":32,"h":32,"source_type":"composition","source_id":1}]`, 64, 64, 2, 2, 2, 0, false},
		{"bad theme", `[{"id":"a","x":0,"y":0,"w":32,"h":32,"theme":{"accent":"nope"}}]`, 64, 64, 2, 2, 2, 0, true},
		{"negative inset", `[{"id":"a","x":0,"y":0,"w":32,"h":32,"inset":-2}]`, 64, 64, 2, 2, 2, 0, true},
		{"gap too big", `[]`, 64, 64, 2, 2, 65, 0, true},
		{"padding too big", `[]`, 64, 64, 2, 2, 2, 65, true},
		{"grid ok", `[{"id":"a","row":1,"col":1}]`, 64, 64, 2, 2, 2, 0, false},
		{"grid out of range", `[{"id":"a","row":2,"col":0}]`, 64, 64, 2, 2, 2, 0, true},
		{"grid span out of range", `[{"id":"a","row":1,"col":1,"row_span":2}]`, 64, 64, 2, 2, 2, 0, true},
		{"unknown canvas skips bounds", `[{"id":"a","x":500,"y":0,"w":32,"h":32}]`, 0, 0, 2, 2, 2, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRegionConfig(tc.raw, tc.w, tc.h, tc.rows, tc.cols, tc.gap, tc.pad)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestRegionsToAbsolute(t *testing.T) {
	in := []datasource.Region{
		{ID: "a", Row: 0, Col: 0},
		{ID: "b", Row: 0, Col: 1},
		{ID: "c", X: 5, Y: 5, W: 10, H: 10},
	}
	out := regionsToAbsolute(in, 1, 2, 0, 0, 64, 32)
	if out[0].W != 32 || out[0].H != 32 || out[0].X != 0 || out[0].Y != 0 {
		t.Fatalf("region a = %+v", out[0])
	}
	if out[1].X != 32 || out[1].W != 32 {
		t.Fatalf("region b = %+v", out[1])
	}
	if out[2].X != 5 || out[2].W != 10 {
		t.Fatalf("absolute region mutated: %+v", out[2])
	}
	// input slice must not be mutated
	if in[0].W != 0 {
		t.Fatalf("input mutated: %+v", in[0])
	}
}

func layoutForm(name, regions string) string {
	v := url.Values{}
	v.Set("name", name)
	v.Set("mode", "absolute")
	v.Set("rows", "1")
	v.Set("cols", "1")
	v.Set("gap", "0")
	v.Set("padding", "0")
	v.Set("background", "#282a36")
	v.Set("enabled", "on")
	v.Set("ttl_seconds", "0")
	v.Set("canvas_w", "64")
	v.Set("canvas_h", "64")
	v.Set("regions", regions)
	return v.Encode()
}

func adminForm(t *testing.T, srv *Server, cookie *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

const validLayoutRegions = `[{"id":"a","x":0,"y":0,"w":32,"h":32,"source_type":"clock","source_id":0}]`

func TestLayoutLifecycle(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)

	if w := adminForm(t, srv, cookie, http.MethodPost, "/admin/layouts/new", layoutForm("Wall", validLayoutRegions)); w.Code != http.StatusFound {
		t.Fatalf("create: code=%d body=%s", w.Code, w.Body.String())
	}
	layouts := srv.DB.Composition.Query().Where(composition.NameEQ("Wall")).AllX(srv.Ctx)
	if len(layouts) != 1 {
		t.Fatalf("expected 1 layout, got %d", len(layouts))
	}
	id := layouts[0].ID

	// list renders the created layout
	if w := adminForm(t, srv, cookie, http.MethodGet, "/admin/layouts", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Wall") {
		t.Fatalf("list: code=%d", w.Code)
	}

	// invalid save persists nothing
	bad := layoutForm("Bad", `[{"id":"x","x":0,"y":0,"w":999,"h":999,"source_type":"clock","source_id":0}]`)
	if w := adminForm(t, srv, cookie, http.MethodPost, "/admin/layouts/new", bad); w.Code != http.StatusFound {
		t.Fatalf("invalid create code=%d", w.Code)
	}
	if n := srv.DB.Composition.Query().Where(composition.NameEQ("Bad")).CountX(srv.Ctx); n != 0 {
		t.Fatalf("invalid save persisted %d rows", n)
	}

	// update
	edit := layoutForm("Wall v2", validLayoutRegions)
	if w := adminForm(t, srv, cookie, http.MethodPost, "/admin/layouts/"+strconv.Itoa(id)+"/edit", edit); w.Code != http.StatusFound {
		t.Fatalf("update code=%d", w.Code)
	}
	if got := srv.DB.Composition.GetX(srv.Ctx, id).Name; got != "Wall v2" {
		t.Fatalf("update name=%q", got)
	}

	// delete
	if w := adminForm(t, srv, cookie, http.MethodPost, "/admin/layouts/"+strconv.Itoa(id)+"/delete", ""); w.Code != http.StatusFound {
		t.Fatalf("delete code=%d", w.Code)
	}
	if n := srv.DB.Composition.Query().Where(composition.ID(id)).CountX(srv.Ctx); n != 0 {
		t.Fatalf("layout not deleted")
	}
}

func TestLayoutPreviewIsolation(t *testing.T) {
	srv := newBackupTestServer(t)
	cookie := loginBackup(t, srv)

	dev := srv.DB.DeviceSettings.Create().SetName("Panel").SetIP("127.0.0.1").SaveX(srv.Ctx)
	before := srv.DB.DeviceSettings.GetX(srv.Ctx, dev.ID)
	if srv.DB.GeneralSettings.Query().CountX(srv.Ctx) == 0 {
		srv.DB.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).SaveX(srv.Ctx)
	}

	body := layoutForm("Preview", `[{"id":"a","x":0,"y":0,"w":64,"h":64,"source_type":"clock","source_id":0}]`) + "&w=64&h=64"
	w := adminForm(t, srv, cookie, http.MethodPost, "/admin/preview/layout", body)
	if w.Code != http.StatusOK {
		t.Fatalf("preview code=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type=%q", ct)
	}

	after := srv.DB.DeviceSettings.GetX(srv.Ctx, dev.ID)
	if after.FramesServed != before.FramesServed {
		t.Fatalf("frames_served mutated: %d -> %d", before.FramesServed, after.FramesServed)
	}
	if (after.LastSeenAt == nil) != (before.LastSeenAt == nil) {
		t.Fatalf("last_seen_at mutated")
	}

	// malformed region JSON is rejected before rendering
	bad := layoutForm("Preview", "{not json") + "&w=64&h=64"
	if w := adminForm(t, srv, cookie, http.MethodPost, "/admin/preview/layout", bad); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed preview code=%d", w.Code)
	}
}

func TestLayoutBackupRoundTrip(t *testing.T) {
	srv := newBackupTestServer(t)
	regions := `[{"id":"a","x":0,"y":0,"w":32,"h":32,"source_type":"clock","source_id":0},{"id":"b","x":32,"y":0,"w":32,"h":32,"border":true,"theme":{"accent":"#ff0000"}}]`
	obj := srv.DB.Composition.Create().SetName("Wall").SetMode("absolute").
		SetRows(1).SetCols(2).SetGap(1).SetPadding(0).SetBackground("#101010").
		SetEnabled(true).SetRegions(regions).SaveX(srv.Ctx)

	bundle, err := srv.ExportBundle(false, false)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if n := len(bundle.Entities["composition"]); n != 1 {
		t.Fatalf("exported %d compositions", n)
	}

	if err := srv.DB.Composition.DeleteOneID(obj.ID).Exec(srv.Ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	srv.ImportBundle(bundle, false)

	list := srv.DB.Composition.Query().AllX(srv.Ctx)
	if len(list) != 1 {
		t.Fatalf("restored %d compositions", len(list))
	}
	got := list[0]
	if got.Name != obj.Name || got.Mode != obj.Mode || got.Rows != obj.Rows || got.Cols != obj.Cols ||
		got.Gap != obj.Gap || got.Padding != obj.Padding || got.Background != obj.Background || got.Enabled != obj.Enabled {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if !reflect.DeepEqual(datasource.ParseRegions(got.Regions), datasource.ParseRegions(obj.Regions)) {
		t.Fatalf("regions mismatch:\n got %s\nwant %s", got.Regions, obj.Regions)
	}
}
