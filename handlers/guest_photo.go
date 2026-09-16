package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"ledit/ent/generalsettings"
	"ledit/ent/guestphoto"
)

var guestUploadDir = filepath.Join("web", "media", "guest_uploads")

func (s *Server) APIGuestPhotoUpload(c *gin.Context) {
	tok := currentGuestToken(c)
	if tok == nil {
		abortGuestUnauthorized(c)
		return
	}
	file, err := c.FormFile("photo")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing_file"})
		return
	}
	if file.Size > maxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "too_large"})
		return
	}
	if ok, retry := checkWindowRateLimit("photo:tok:"+strconv.Itoa(tok.ID), 5); !ok {
		abortGuestRateLimited(c, retry)
		return
	}
	if ok, retry := checkWindowRateLimit("photo:ip:"+c.ClientIP(), 20); !ok {
		abortGuestRateLimited(c, retry)
		return
	}
	cnt, err := s.DB.GuestPhoto.Query().
		Where(guestphoto.StatusEQ(guestphoto.StatusPending), guestphoto.GuestTokenIDEQ(tok.ID)).
		Count(c.Request.Context())
	if err == nil && cnt >= 20 {
		c.Header("Retry-After", "60")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too_many_pending"})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing_file"})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxUploadBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_image"})
		return
	}
	if len(data) > maxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "too_large"})
		return
	}
	if _, err := runImportPipeline(data, 0, 0, "", nil, false, 0); err != nil {
		// runImportPipeline with 0,0 always fails dimension check; fall back to
		// direct decode validation for guest uploads.
		if !strings.Contains(err.Error(), "target dimensions") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_image"})
			return
		}
		if _, _, decErr := decodeImage(data); decErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_image"})
			return
		}
	}

	if err := os.MkdirAll(guestUploadDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file"})
		return
	}
	ext := filepath.Ext(file.Filename)
	path := filepath.Join(guestUploadDir, uuid.New().String()+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file"})
		return
	}

	row, err := s.DB.GuestPhoto.Create().
		SetPath(path).
		SetGuestTokenID(tok.ID).
		SetStatus(guestphoto.StatusPending).
		SetBytes(len(data)).
		SetExpiresAt(time.Now().Add(24 * time.Hour)).
		Save(c.Request.Context())
	if err != nil {
		_ = os.Remove(path)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create photo"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"status": "pending", "id": row.ID})
	func() {
		defer func() { _ = recover() }()
		s.pruneGuestPhotos(time.Now())
	}()
}

func (s *Server) APIGuestPhotoApprove(c *gin.Context) {
	id, err := parseIDParam(c)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	gp, err := s.DB.GuestPhoto.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if gp.Status == guestphoto.StatusApproved {
		c.JSON(http.StatusOK, gin.H{"status": "approved"})
		return
	}
	_, err = s.DB.GuestPhoto.UpdateOneID(id).SetStatus(guestphoto.StatusApproved).Save(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to approve"})
		return
	}
	// Best-effort: create Image and attach to GeneralSettings like AdminImageCreate.
	obj := s.DB.Image.Create().SetPath(gp.Path).SaveX(s.Ctx)
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		if err := s.DB.GeneralSettings.UpdateOne(settings).AddImages(obj).Exec(s.Ctx); err != nil {
			slog.Error("guest photo approve attach failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to attach image"})
			return
		}
	} else {
		slog.Error("guest photo approve: general settings missing", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to attach image"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "approved"})
	func() {
		defer func() { _ = recover() }()
		s.pruneGuestPhotos(time.Now())
	}()
}

func (s *Server) APIGuestPhotoReject(c *gin.Context) {
	id, err := parseIDParam(c)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	gp, err := s.DB.GuestPhoto.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if gp.Status == guestphoto.StatusRejected {
		c.JSON(http.StatusOK, gin.H{"status": "rejected"})
		return
	}
	_, _ = s.DB.GuestPhoto.UpdateOneID(id).SetStatus(guestphoto.StatusRejected).Save(c.Request.Context())
	_ = os.Remove(gp.Path)
	c.JSON(http.StatusOK, gin.H{"status": "rejected"})
}

func (s *Server) pruneGuestPhotos(now time.Time) {
	// Expired rows.
	expired, err := s.DB.GuestPhoto.Query().Where(guestphoto.ExpiresAtLT(now)).All(s.Ctx)
	if err == nil {
		for _, r := range expired {
			if r.ExpiresAt == nil {
				continue
			}
			_ = os.Remove(r.Path)
			if err := s.DB.GuestPhoto.DeleteOneID(r.ID).Exec(s.Ctx); err != nil {
				slog.Error("prune guest photo expired delete failed", "id", r.ID, "error", err)
			}
		}
	} else {
		slog.Error("prune guest photo expired query failed", "error", err)
	}
	// Keep 50 most recent approved.
	approved, err := s.DB.GuestPhoto.Query().
		Where(guestphoto.StatusEQ(guestphoto.StatusApproved)).
		Order(guestphoto.ByID()).All(s.Ctx)
	if err != nil {
		slog.Error("prune guest photo approved query failed", "error", err)
		return
	}
	if len(approved) > 50 {
		// Keep newest 50 (highest IDs); delete older.
		toDelete := approved[:len(approved)-50]
		for _, r := range toDelete {
			_ = os.Remove(r.Path)
			if err := s.DB.GuestPhoto.DeleteOneID(r.ID).Exec(s.Ctx); err != nil {
				slog.Error("prune guest photo cap delete failed", "id", r.ID, "error", err)
			}
		}
	}
}
