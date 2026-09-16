package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"ledit/ent"
	"ledit/ent/firmwarerelease"
)

func newFirmwareTestServer(t *testing.T) *Server {
	t.Helper()
	return newBackupTestServer(t)
}

func TestFirmwareCohortEligibleDeterministic(t *testing.T) {
	a := firmwareCohortEligible("fp-abc", 25)
	b := firmwareCohortEligible("fp-abc", 25)
	if a != b {
		t.Fatalf("not deterministic")
	}
}

func TestFirmwareCohortStability(t *testing.T) {
	// Find a fingerprint eligible at 25; it must remain eligible at 50
	var chosen string
	for i := 0; i < 1000; i++ {
		fp := fmt.Sprintf("fp-%d", i)
		if firmwareCohortEligible(fp, 25) {
			chosen = fp
			break
		}
	}
	if chosen == "" {
		t.Fatalf("no eligible fp found")
	}
	if !firmwareCohortEligible(chosen, 50) {
		t.Fatalf("eligible at 25 should still be eligible at 50")
	}
}

func TestResolveFirmwarePaused(t *testing.T) {
	rel := &ent.FirmwareRelease{Version: "2.0.0", Enabled: true}
	settings := &ent.FirmwareSettings{Paused: true, TargetVersion: "2.0.0", RolloutPercent: 100, Channel: "stable"}
	dev := &ent.DeviceSettings{FirmwareVersion: "1.0.0", Fingerprint: "fp"}
	d := resolveFirmware(rel, settings, dev)
	if d.Action != "none" {
		t.Fatalf("paused should be none got %s", d.Action)
	}
}

func TestResolveFirmwareRollback(t *testing.T) {
	rel := &ent.FirmwareRelease{Version: "1.0.0", Enabled: true}
	settings := &ent.FirmwareSettings{Paused: false, TargetVersion: "1.0.0", RolloutPercent: 100, Channel: "stable"}
	dev := &ent.DeviceSettings{FirmwareVersion: "2.0.0", Fingerprint: "fp"}
	d := resolveFirmware(rel, settings, dev)
	if d.Action != "rollback" {
		t.Fatalf("expected rollback got %s", d.Action)
	}
}

func TestResolveFirmwareCurrentNone(t *testing.T) {
	rel := &ent.FirmwareRelease{Version: "1.0.0", Enabled: true}
	settings := &ent.FirmwareSettings{Paused: false, TargetVersion: "1.0.0", RolloutPercent: 100, Channel: "stable"}
	dev := &ent.DeviceSettings{FirmwareVersion: "1.0.0", Fingerprint: "fp"}
	d := resolveFirmware(rel, settings, dev)
	if d.Action != "none" {
		t.Fatalf("current should be none got %s", d.Action)
	}
}

func TestResolveFirmwarePinOverridesPercent(t *testing.T) {
	// Device pinned should get upgrade even with 0 percent rollout
	rel := &ent.FirmwareRelease{Version: "2.0.0", Enabled: true, Sha256: "abc", SizeBytes: 100}
	pin := "2.0.0"
	settings := &ent.FirmwareSettings{Paused: false, TargetVersion: "2.0.0", RolloutPercent: 0, Channel: "stable"}
	dev := &ent.DeviceSettings{FirmwareVersion: "1.0.0", Fingerprint: "uneligible-fp", FirmwareVersionPin: &pin}
	d := resolveFirmware(rel, settings, dev)
	if d.Action != "upgrade" {
		t.Fatalf("pinned should override cohort, got %s", d.Action)
	}
}

func TestFirmwareCohortZeroEligible(t *testing.T) {
	rel := &ent.FirmwareRelease{Version: "2.0.0", Enabled: true, Sha256: "abc", SizeBytes: 100}
	settings := &ent.FirmwareSettings{Paused: false, TargetVersion: "2.0.0", RolloutPercent: 0, Channel: "stable"}
	dev := &ent.DeviceSettings{FirmwareVersion: "1.0.0", Fingerprint: "any"}
	d := resolveFirmware(rel, settings, dev)
	if d.Action != "none" {
		t.Fatalf("0 percent should be none got %s", d.Action)
	}
}

func TestFirmwareManifestUnknownToken(t *testing.T) {
	srv := newFirmwareTestServer(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/device/firmware/manifest?version=1.0.0", nil)
	c.Request.Header.Set("X-Device-Token", "invalid")
	srv.DeviceFirmwareManifest(c)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Fatalf("expected 401/403 got %d body %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sha256") || strings.Contains(w.Body.String(), "version") && strings.Contains(w.Body.String(), "2.") {
		t.Fatalf("metadata leak on auth failure: %s", w.Body.String())
	}
}

func TestFirmwareManifestOlderWithZeroPercentStillNone(t *testing.T) {
	srv := newFirmwareTestServer(t)
	// create device
	dev := srv.DB.DeviceSettings.Create().SetName("d1").SetToken("tok123").SetEnabled(true).SetFirmwareVersion("1.0.0").SetFingerprint("fp-zero").SaveX(srv.Ctx)
	// create artifact file
	dir := t.TempDir()
	art := filepath.Join(dir, "fw.bin")
	_ = os.WriteFile(art, []byte("hello"), 0644)
	rel := srv.DB.FirmwareRelease.Create().SetVersion("2.0.0").SetChannel("stable").SetSha256("abc123").SetSizeBytes(5).SetArtifactPath(art).SetEnabled(true).SaveX(srv.Ctx)
	_ = rel
	srv.DB.FirmwareSettings.Create().SetChannel("stable").SetTargetVersion("2.0.0").SetRolloutPercent(0).SetPaused(false).SaveX(srv.Ctx)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/device/firmware/manifest?version=1.0.0&token=tok123", nil)
	srv.DeviceFirmwareManifest(c)
	if w.Code != 200 {
		t.Fatalf("expected 200 got %d %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["action"] != "none" {
		t.Fatalf("expected none got %v", body)
	}
	_ = dev
}

func TestFirmwareDuplicateRejected(t *testing.T) {
	srv := newFirmwareTestServer(t)
	dir := t.TempDir()
	art := filepath.Join(dir, "fw.bin")
	_ = os.WriteFile(art, []byte("content1"), 0644)
	art2 := filepath.Join(dir, "fw2.bin")
	_ = os.WriteFile(art2, []byte("content2"), 0644)

	// login for admin (flash needs auth but handler doesn't check auth; just test via direct DB logic)
	// Simulate AdminFirmwareReleaseCreate via gin context
	gin.SetMode(gin.TestMode)
	// first create via DB directly to mimic success
	rel1 := srv.DB.FirmwareRelease.Create().SetVersion("1.0.0").SetChannel("stable").SetSha256("sha1").SetSizeBytes(8).SetArtifactPath(art).SetEnabled(true).SaveX(srv.Ctx)

	// attempt duplicate via handler
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := strings.NewReader("version=1.0.0&channel=stable&artifact_path=" + art2)
	c.Request = httptest.NewRequest("POST", "/admin/firmware/releases", body)
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.AdminFirmwareReleaseCreate(c)

	count, _ := srv.DB.FirmwareRelease.Query().Where(firmwarerelease.ChannelEQ("stable"), firmwarerelease.VersionEQ("1.0.0")).Count(srv.Ctx)
	if count != 1 {
		t.Fatalf("duplicate should not increase count, got %d", count)
	}
	// sha unchanged
	relAfter, _ := srv.DB.FirmwareRelease.Query().Where(firmwarerelease.ChannelEQ("stable"), firmwarerelease.VersionEQ("1.0.0")).Only(srv.Ctx)
	if relAfter.Sha256 != rel1.Sha256 {
		t.Fatalf("sha overwritten on duplicate")
	}
}

func TestFirmwareMissingArtifactRejected(t *testing.T) {
	srv := newFirmwareTestServer(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := strings.NewReader("version=2.0.0&channel=stable&artifact_path=")
	c.Request = httptest.NewRequest("POST", "/admin/firmware/releases", body)
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.AdminFirmwareReleaseCreate(c)
	count, _ := srv.DB.FirmwareRelease.Query().Where(firmwarerelease.ChannelEQ("stable"), firmwarerelease.VersionEQ("2.0.0")).Count(srv.Ctx)
	if count != 0 {
		t.Fatalf("missing artifact should be rejected")
	}
}

func TestFirmwareRolloutUpgradeEligible(t *testing.T) {
	srv := newFirmwareTestServer(t)
	// find eligible fingerprint
	var fp string
	for i := 0; i < 5000; i++ {
		cand := fmt.Sprintf("cand-%d", i)
		if firmwareCohortEligible(cand, 100) {
			fp = cand
			break
		}
	}
	dev := srv.DB.DeviceSettings.Create().SetName("d").SetToken("tok-up").SetEnabled(true).SetFirmwareVersion("1.0.0").SetFingerprint(fp).SaveX(srv.Ctx)
	dir := t.TempDir()
	art := filepath.Join(dir, "fw.bin")
	_ = os.WriteFile(art, []byte("data"), 0644)
	_ = srv.DB.FirmwareRelease.Create().SetVersion("2.0.0").SetChannel("stable").SetSha256("sha").SetSizeBytes(4).SetArtifactPath(art).SetEnabled(true).SaveX(srv.Ctx)
	_ = srv.DB.FirmwareSettings.Create().SetChannel("stable").SetTargetVersion("2.0.0").SetRolloutPercent(100).SetPaused(false).SaveX(srv.Ctx)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/device/firmware/manifest?version=1.0.0&token=tok-up", nil)
	srv.DeviceFirmwareManifest(c)
	if w.Code != 200 {
		t.Fatalf("expected 200 got %d", w.Code)
	}
	var body map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["action"] != "upgrade" {
		t.Fatalf("expected upgrade got %v", body)
	}
	_ = dev
}
