package handlers

import (
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ledit/ent"
	"ledit/ent/devicesettings"
	"ledit/ent/firmwarerelease"
)

type firmwareDecision struct {
	Action      string
	Version     string
	Sha256      string
	SizeBytes   int
	Mandatory   bool
	DownloadURL string
}

func firmwareCohortEligible(fingerprint string, percent int) bool {
	if percent <= 0 {
		return false
	}
	if percent >= 100 {
		return true
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(fingerprint))
	return int(h.Sum32()%100) < percent
}

func compareVersions(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(pa) {
			av, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			bv, _ = strconv.Atoi(pb[i])
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func resolveFirmware(rel *ent.FirmwareRelease, settings *ent.FirmwareSettings, dev *ent.DeviceSettings) firmwareDecision {
	if settings == nil || rel == nil || settings.Paused || !rel.Enabled {
		return firmwareDecision{Action: "none"}
	}
	// Determine target version: pinned overrides settings
	target := settings.TargetVersion
	pinned := false
	if dev != nil && dev.FirmwareVersionPin != nil && *dev.FirmwareVersionPin != "" {
		target = *dev.FirmwareVersionPin
		pinned = true
	}
	if dev != nil && dev.FirmwareVersion == target {
		return firmwareDecision{Action: "none"}
	}
	if target == "" {
		return firmwareDecision{Action: "none"}
	}
	// Release version must match target; if caller passed mismatched release, treat as none
	if rel.Version != target {
		return firmwareDecision{Action: "none"}
	}
	// Cohort check for unpinned devices; pinned ignores cohort
	// Mandatory releases still go through cohort (documented)
	if !pinned && dev != nil {
		if !firmwareCohortEligible(dev.Fingerprint, settings.RolloutPercent) {
			return firmwareDecision{Action: "none"}
		}
	} else if !pinned && dev == nil {
		return firmwareDecision{Action: "none"}
	}
	action := "upgrade"
	if dev != nil && dev.FirmwareVersion != "" {
		cmp := compareVersions(dev.FirmwareVersion, target)
		if cmp > 0 {
			action = "rollback"
		} else if cmp == 0 {
			return firmwareDecision{Action: "none"}
		}
	}
	return firmwareDecision{
		Action:      action,
		Version:     rel.Version,
		Sha256:      rel.Sha256,
		SizeBytes:   rel.SizeBytes,
		Mandatory:   rel.Mandatory,
		DownloadURL: "/api/device/firmware/" + rel.Version + "/artifact",
	}
}

func ensureFirmwareSettings(s *Server) *ent.FirmwareSettings {
	if s.DB == nil {
		return nil
	}
	fs, err := s.DB.FirmwareSettings.Query().First(s.Ctx)
	if err == nil {
		return fs
	}
	fs, _ = s.DB.FirmwareSettings.Create().SetChannel("stable").SetTargetVersion("").SetRolloutPercent(0).SetPaused(false).Save(s.Ctx)
	return fs
}

func deviceByToken(s *Server, c *gin.Context) (*ent.DeviceSettings, bool) {
	tok := c.GetHeader("X-Device-Token")
	if tok == "" {
		tok = c.Query("token")
	}
	if tok == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	dev, err := s.DB.DeviceSettings.Query().Where(devicesettings.TokenEQ(tok)).Only(s.Ctx)
	if err != nil || dev == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	if !dev.Enabled {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		return nil, false
	}
	return dev, true
}

func (s *Server) DeviceFirmwareManifest(c *gin.Context) {
	dev, ok := deviceByToken(s, c)
	if !ok {
		return
	}
	if v := c.Query("version"); v != "" {
		now := time.Now()
		_ = s.DB.DeviceSettings.UpdateOne(dev).SetFirmwareVersion(v).SetNillableLastUpdateAt(&now).Exec(s.Ctx)
		dev.FirmwareVersion = v
	} else {
		// No version reported -> still respond none with 200
		// But if no version query param, treat as unknown -> none
		// We still need settings to decide; handle below
	}
	settings := ensureFirmwareSettings(s)
	if settings == nil {
		c.JSON(http.StatusOK, gin.H{"action": "none"})
		return
	}
	// Determine target version considering pin
	target := settings.TargetVersion
	if dev.FirmwareVersionPin != nil && *dev.FirmwareVersionPin != "" {
		target = *dev.FirmwareVersionPin
	}
	if target == "" {
		c.JSON(http.StatusOK, gin.H{"action": "none"})
		return
	}
	// If version param absent and device version empty, return none directly
	if c.Query("version") == "" && dev.FirmwareVersion == "" {
		c.JSON(http.StatusOK, gin.H{"action": "none"})
		return
	}
	if dev.FirmwareVersion == target {
		c.JSON(http.StatusOK, gin.H{"action": "none"})
		return
	}
	rel, err := s.DB.FirmwareRelease.Query().Where(firmwarerelease.ChannelEQ(settings.Channel), firmwarerelease.VersionEQ(target), firmwarerelease.EnabledEQ(true)).Only(s.Ctx)
	if err != nil || rel == nil {
		c.JSON(http.StatusOK, gin.H{"action": "none"})
		return
	}
	d := resolveFirmware(rel, settings, dev)
	if d.Action == "none" {
		c.JSON(http.StatusOK, gin.H{"action": "none"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"action":    d.Action,
		"version":   d.Version,
		"sha256":    d.Sha256,
		"size":      d.SizeBytes,
		"mandatory": d.Mandatory,
		"url":       d.DownloadURL,
	})
}

func (s *Server) DeviceFirmwareArtifact(c *gin.Context) {
	dev, ok := deviceByToken(s, c)
	if !ok {
		return
	}
	_ = dev
	version := c.Param("version")
	rel, err := s.DB.FirmwareRelease.Query().Where(firmwarerelease.VersionEQ(version), firmwarerelease.EnabledEQ(true)).Only(s.Ctx)
	if err != nil || rel == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	if rel.SizeBytes > 0 {
		c.Header("Content-Length", strconv.Itoa(rel.SizeBytes))
	}
	c.File(rel.ArtifactPath)
}

func (s *Server) DeviceFirmwareReport(c *gin.Context) {
	dev, ok := deviceByToken(s, c)
	if !ok {
		return
	}
	version := c.PostForm("version")
	status := c.PostForm("status")
	if version == "" || status == "" {
		var body struct {
			Version string `json:"version"`
			Status  string `json:"status"`
		}
		_ = c.ShouldBindJSON(&body)
		if version == "" {
			version = body.Version
		}
		if status == "" {
			status = body.Status
		}
	}
	if version == "" || status == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "version and status required"})
		return
	}
	now := time.Now()
	_ = s.DB.DeviceSettings.UpdateOne(dev).SetFirmwareVersion(version).SetLastUpdateStatus(status).SetNillableLastUpdateAt(&now).Exec(s.Ctx)
	c.Status(http.StatusNoContent)
}
