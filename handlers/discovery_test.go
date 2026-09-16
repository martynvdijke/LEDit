package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent/devicesettings"
)

func init() { gin.SetMode(gin.TestMode) }

func resetState() {
	discoveryResetForTest()
	rateMu.Lock()
	rateBuckets = map[string][]time.Time{}
	rateMu.Unlock()
}

func newDiscoveryTestServer(t *testing.T) *Server {
	t.Helper()
	resetState()
	return newTestServerWithDB(t)
}

func TestDiscoveryEnrollCreatesDevice(t *testing.T) {
	srv := newDiscoveryTestServer(t)
	fp := "fp-enroll-1"
	discoveryUpsert(DiscoveredDevice{Fingerprint: fp, Nonce: "n1", Model: "m1", Address: "10.0.0.1:8080"})
	r := gin.New()
	r.POST("/api/device/discovery/:fingerprint/enroll", srv.AdminDeviceEnroll)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/device/discovery/"+fp+"/enroll", strings.NewReader("name=mydevice"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("enroll %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["fingerprint"] != fp {
		t.Fatalf("fingerprint mismatch %v", resp)
	}
	if strings.Contains(w.Body.String(), "token") && strings.Contains(w.Body.String(), resp["fingerprint"].(string)) {
		// ensure no token leak: response should not contain actual token value; check token field absent
		if _, has := resp["token"]; has {
			t.Fatalf("token leaked")
		}
	}
	ds, err := srv.DB.DeviceSettings.Query().Where(devicesettings.FingerprintEQ(fp)).Only(srv.Ctx)
	if err != nil {
		t.Fatalf("query %v", err)
	}
	if ds.ApprovedAt == nil {
		t.Fatal("ApprovedAt nil")
	}
	if ds.Token == "" {
		t.Fatal("token empty")
	}
	if ds.Name != "mydevice" {
		t.Fatalf("name %s", ds.Name)
	}
	// state approved
	dev, _ := discoveryEntry(fp)
	if dev.State != "approved" {
		t.Fatalf("state %s", dev.State)
	}
	// second enroll should be 409 and not rotate
	oldToken := ds.Token
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/device/discovery/"+fp+"/enroll", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != 409 {
		t.Fatalf("second enroll want 409 got %d %s", w2.Code, w2.Body.String())
	}
	ds2, _ := srv.DB.DeviceSettings.Query().Where(devicesettings.FingerprintEQ(fp)).Only(srv.Ctx)
	if ds2.Token != oldToken {
		t.Fatal("token rotated")
	}
	count, _ := srv.DB.DeviceSettings.Query().Where(devicesettings.FingerprintEQ(fp)).Count(srv.Ctx)
	if count != 1 {
		t.Fatalf("count %d", count)
	}
}

func TestDiscoveryProvisionReturnsTokenOnce(t *testing.T) {
	srv := newDiscoveryTestServer(t)
	fp := "fp-prov-1"
	nonce := "nonce123"
	// use loopback so ClientIP matches
	discoveryUpsert(DiscoveredDevice{Fingerprint: fp, Nonce: nonce, Address: "127.0.0.1:9000"})
	// enroll device
	srv.DB.DeviceSettings.Create().SetName("d1").SetFingerprint(fp).SetApprovedAt(time.Now()).SetToken("tok123").SaveX(srv.Ctx)
	discoveryMarkApproved(fp)
	// provision handler directly
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/provision?fingerprint="+fp+"&nonce="+nonce, nil)
	c.Request.RemoteAddr = "127.0.0.1:1234"
	srv.DeviceProvision(c)
	if w.Code != 200 {
		t.Fatalf("provision %d %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["token"] != "tok123" {
		t.Fatalf("token mismatch %v", resp)
	}
	if _, has := resp["server"]; !has {
		t.Fatal("server missing")
	}
	// replay same nonce should return no token (204 or no token)
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest("GET", "/provision?fingerprint="+fp+"&nonce="+nonce, nil)
	c2.Request.RemoteAddr = "127.0.0.1:1234"
	srv.DeviceProvision(c2)
	if w2.Code == 200 {
		var r2 map[string]any
		_ = json.Unmarshal(w2.Body.Bytes(), &r2)
		if _, has := r2["token"]; has && r2["token"] != "" {
			t.Fatalf("replay should not return token %v", r2)
		}
		// 204 is also acceptable
		if w2.Body.Len() != 0 && r2["token"] != nil {
			t.Fatalf("replay token present")
		}
	}
	if w2.Code != 204 && w2.Code != 200 {
		t.Fatalf("replay code %d", w2.Code)
	}
	// Wrong nonce
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest("GET", "/provision?fingerprint="+fp+"&nonce=wrong", nil)
	c3.Request.RemoteAddr = "127.0.0.1:1234"
	// need to re-upsert nonce since previous invalidated
	discoveryUpsert(DiscoveredDevice{Fingerprint: fp, Nonce: "newnonce", Address: "127.0.0.1:9000", State: "approved"})
	w3b := httptest.NewRecorder()
	c3b, _ := gin.CreateTestContext(w3b)
	c3b.Request = httptest.NewRequest("GET", "/provision?fingerprint="+fp+"&nonce=wrong2", nil)
	c3b.Request.RemoteAddr = "127.0.0.1:1234"
	srv.DeviceProvision(c3b)
	if w3b.Code != 204 {
		// may be 204
		if w3b.Code == 200 {
			var r map[string]any
			_ = json.Unmarshal(w3b.Body.Bytes(), &r)
			if _, has := r["token"]; has && r["token"] != "" {
				t.Fatal("wrong nonce should not give token")
			}
		}
	}
	_ = w3
}

func TestDiscoveryProvisionUnapprovedNoToken(t *testing.T) {
	srv := newDiscoveryTestServer(t)
	fp := "fp-unapproved"
	nonce := "n-unapproved"
	discoveryUpsert(DiscoveredDevice{Fingerprint: fp, Nonce: nonce, Address: "127.0.0.1:9000"})
	srv.DB.DeviceSettings.Create().SetName("d2").SetFingerprint(fp).SetToken("tok999").SaveX(srv.Ctx) // no ApprovedAt
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/provision?fingerprint="+fp+"&nonce="+nonce, nil)
	c.Request.RemoteAddr = "127.0.0.1:1234"
	srv.DeviceProvision(c)
	if w.Code != 204 {
		var r map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		if _, has := r["token"]; has && r["token"] != "" {
			t.Fatal("unapproved should not return token")
		}
		if w.Code != 200 && w.Code != 204 {
			t.Fatalf("code %d", w.Code)
		}
	}
}

func TestDiscoveryCancel(t *testing.T) {
	srv := newDiscoveryTestServer(t)
	fp := "fp-cancel"
	discoveryUpsert(DiscoveredDevice{Fingerprint: fp, Nonce: "n", Address: "1.2.3.4:8080"})
	srv.DB.GeneralSettings.Create().SetTimeout(10).SetRandom(false).SetWidth(64).SetHeight(64).SetHolidays("[]").SetTransitionStyle("none").SetTransitionMs(500).SetOrderingMode("random").SetAdaptiveHalfLifeDays(7).SetAdaptiveWindowDays(14).SetAdaptiveFloor(0.05).SetAdaptiveEpsilon(0.15).SaveX(srv.Ctx)
	// enroll
	r := gin.New()
	r.POST("/api/device/discovery/:fingerprint/enroll", srv.AdminDeviceEnroll)
	r.POST("/api/device/discovery/:fingerprint/cancel", srv.AdminDeviceEnrollmentCancel)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/device/discovery/"+fp+"/enroll", nil)
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("enroll %d %s", w.Code, w.Body.String())
	}
	// cancel unclaimed should succeed
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/device/discovery/"+fp+"/cancel", nil)
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("cancel %d %s", w2.Code, w2.Body.String())
	}
	exists, _ := srv.DB.DeviceSettings.Query().Where(devicesettings.FingerprintEQ(fp)).Exist(srv.Ctx)
	if exists {
		t.Fatal("device not deleted")
	}
	dev, _ := discoveryEntry(fp)
	if dev.State != "pending" {
		t.Fatalf("state %s", dev.State)
	}
	// enroll again then set LastSeenAt (claimed) -> cancel should 409
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest("POST", "/api/device/discovery/"+fp+"/enroll", nil)
	r.ServeHTTP(w3, req3)
	if w3.Code != 200 {
		t.Fatalf("re-enroll %d %s", w3.Code, w3.Body.String())
	}
	ds, _ := srv.DB.DeviceSettings.Query().Where(devicesettings.FingerprintEQ(fp)).Only(srv.Ctx)
	now := time.Now()
	_, _ = srv.DB.DeviceSettings.UpdateOne(ds).SetLastSeenAt(now).Save(srv.Ctx)
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest("POST", "/api/device/discovery/"+fp+"/cancel", nil)
	r.ServeHTTP(w4, req4)
	if w4.Code != 409 {
		t.Fatalf("cancel after claim want 409 got %d %s", w4.Code, w4.Body.String())
	}
}

func TestDiscoveryProvisionRateLimit(t *testing.T) {
	srv := newDiscoveryTestServer(t)
	fp := "fp-rate"
	nonce := "rate-nonce"
	discoveryUpsert(DiscoveredDevice{Fingerprint: fp, Nonce: nonce, Address: "10.9.9.9:8080"})
	srv.DB.DeviceSettings.Create().SetName("d-rate").SetFingerprint(fp).SetApprovedAt(time.Now()).SetToken("tok-rate").SaveX(srv.Ctx)
	// exceed 30 provision calls from same IP
	hit429 := false
	for i := 0; i < 35; i++ {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		// use same IP
		req := httptest.NewRequest("GET", "/provision?fingerprint="+fp+"&nonce=wrong"+strings.Repeat("x", i), nil)
		req.RemoteAddr = "10.9.9.9:1234"
		c.Request = req
		srv.DeviceProvision(c)
		if w.Code == 429 {
			hit429 = true
			break
		}
	}
	if !hit429 {
		t.Fatal("expected 429 rate limit")
	}
}

func TestDiscoveryAdminDiscoveryJSON(t *testing.T) {
	srv := newDiscoveryTestServer(t)
	discoveryUpsert(DiscoveredDevice{Fingerprint: "fp-json", Nonce: "n", Address: "1.1.1.1:8080"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/device/discovery", nil)
	srv.AdminDeviceDiscovery(c)
	if w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	var resp map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["devices"]; !ok {
		t.Fatal("devices missing")
	}
	if strings.Contains(w.Body.String(), "token") {
		// ensure not leaking token field
		var parsed map[string][]map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &parsed)
		for _, d := range parsed["devices"] {
			if _, has := d["token"]; has {
				t.Fatal("token leaked in discovery")
			}
		}
	}
}
