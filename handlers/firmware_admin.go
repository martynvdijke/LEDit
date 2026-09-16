package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"ledit/ent/firmwarerelease"
)

func (s *Server) AdminFirmware(c *gin.Context) {
	releases, _ := s.DB.FirmwareRelease.Query().Order().All(s.Ctx)
	// newest first; order by id desc if not specified
	// Re-query ordered by id desc for newest first
	if len(releases) > 0 {
		releases, _ = s.DB.FirmwareRelease.Query().Order().All(s.Ctx)
		// ent order defaults: try explicit
		r2, _ := s.DB.FirmwareRelease.Query().All(s.Ctx)
		_ = r2
	}
	// Use explicit order by ID desc
	releases, _ = s.DB.FirmwareRelease.Query().All(s.Ctx)
	// manual sort newest first by ID desc
	for i, j := 0, len(releases)-1; i < j; i, j = i+1, j-1 {
		releases[i], releases[j] = releases[j], releases[i]
	}
	settings := ensureFirmwareSettings(s)
	s.renderPage(c, http.StatusOK, "firmware.html", gin.H{
		"releases": releases,
		"settings": settings,
		"active":   "firmware",
	})
}

func (s *Server) AdminFirmwareSettings(c *gin.Context) {
	channel := strings.TrimSpace(c.PostForm("channel"))
	if channel == "" {
		channel = "stable"
	}
	targetVersion := strings.TrimSpace(c.PostForm("target_version"))
	percentStr := strings.TrimSpace(c.PostForm("rollout_percent"))
	percent := 0
	if percentStr != "" {
		p, err := strconv.Atoi(percentStr)
		if err != nil {
			SetFlash(c, "danger", "rollout_percent must be an integer")
			c.Redirect(http.StatusFound, "/admin/firmware")
			return
		}
		percent = p
	}
	if percent < 0 || percent > 100 {
		SetFlash(c, "danger", "rollout_percent must be 0..100")
		c.Redirect(http.StatusFound, "/admin/firmware")
		return
	}
	paused := c.PostForm("paused") == "on"

	settings := ensureFirmwareSettings(s)
	if settings == nil {
		SetFlash(c, "danger", "failed to load settings")
		c.Redirect(http.StatusFound, "/admin/firmware")
		return
	}
	_ = s.DB.FirmwareSettings.UpdateOne(settings).SetChannel(channel).SetTargetVersion(targetVersion).SetRolloutPercent(percent).SetPaused(paused).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/firmware")
}

func (s *Server) AdminFirmwareReleaseCreate(c *gin.Context) {
	version := strings.TrimSpace(c.PostForm("version"))
	channel := strings.TrimSpace(c.PostForm("channel"))
	if channel == "" {
		channel = "stable"
	}
	artifactPath := strings.TrimSpace(c.PostForm("artifact_path"))
	notes := c.PostForm("notes")
	minVersion := strings.TrimSpace(c.PostForm("min_version"))
	mandatory := c.PostForm("mandatory") == "on"

	if version == "" {
		SetFlash(c, "danger", "version required")
		c.Redirect(http.StatusFound, "/admin/firmware")
		return
	}
	// Handle multipart file upload
	if file, err := c.FormFile("file"); err == nil && file != nil {
		dir := filepath.Join("web", "media", "firmware", version)
		_ = os.MkdirAll(dir, 0755)
		filename := file.Filename
		if filename == "" {
			filename = uuid.New().String()
		}
		dest := filepath.Join(dir, filepath.Base(filename))
		if err := c.SaveUploadedFile(file, dest); err == nil {
			artifactPath = dest
		}
	}
	if artifactPath == "" {
		SetFlash(c, "danger", "artifact required")
		c.Redirect(http.StatusFound, "/admin/firmware")
		return
	}
	// Duplicate check
	count, _ := s.DB.FirmwareRelease.Query().Where(firmwarerelease.ChannelEQ(channel), firmwarerelease.VersionEQ(version)).Count(s.Ctx)
	if count > 0 {
		SetFlash(c, "danger", "release already exists for channel and version")
		c.Redirect(http.StatusFound, "/admin/firmware")
		return
	}
	// Compute sha256 and size
	f, err := os.Open(artifactPath)
	if err != nil {
		SetFlash(c, "danger", "artifact not found")
		c.Redirect(http.StatusFound, "/admin/firmware")
		return
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		SetFlash(c, "danger", "failed to read artifact")
		c.Redirect(http.StatusFound, "/admin/firmware")
		return
	}
	sha := hex.EncodeToString(h.Sum(nil))
	if sha == "" {
		SetFlash(c, "danger", "failed to compute checksum")
		c.Redirect(http.StatusFound, "/admin/firmware")
		return
	}
	_, err = s.DB.FirmwareRelease.Create().SetVersion(version).SetChannel(channel).SetSha256(sha).SetSizeBytes(int(size)).SetArtifactPath(artifactPath).SetNotes(notes).SetMandatory(mandatory).SetMinVersion(minVersion).SetEnabled(true).Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "failed to create release: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/firmware")
		return
	}
	SetFlash(c, "success", "release created")
	c.Redirect(http.StatusFound, "/admin/firmware")
}

func (s *Server) AdminFirmwareReleaseDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	_ = s.DB.FirmwareRelease.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/firmware")
}
