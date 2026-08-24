package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"
	"ledit/ent"
	"ledit/ent/timelapseframe"
)

// captureJob is enqueued after final PNG composition.
type captureJob struct {
	DeviceID    int
	CapturedAt  time.Time
	SourceType  string
	SourceID    int
	SourceLabel string
	PNGBytes    []byte
	Width       int
	Height      int
}

var (
	timelapseCh        = make(chan captureJob, 128)
	timelapseOnce      sync.Once
	timelapseClient    *ent.Client
	timelapseStop      chan struct{}
	timelapseWarnOnce  sync.Once
	timelapseMediaRoot = "web/media/timelapse"
)

// TimelapseEnabled returns whether captures are enabled (env TIMELAPSE_ENABLED default true).
func TimelapseEnabled() bool {
	v := strings.TrimSpace(os.Getenv("TIMELAPSE_ENABLED"))
	if v == "" {
		return true
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// TimelapseInterval returns capture interval (env TIMELAPSE_INTERVAL or TIMELAPSE_INTERVAL_SECONDS, default 30s, clamped 10s-5min).
func TimelapseInterval() time.Duration {
	if s := strings.TrimSpace(os.Getenv("TIMELAPSE_INTERVAL")); s != "" {
		if d, err := time.ParseDuration(s); err == nil {
			return clampInterval(d)
		}
		if n, err := strconv.Atoi(s); err == nil {
			return clampInterval(time.Duration(n) * time.Second)
		}
	}
	if s := strings.TrimSpace(os.Getenv("TIMELAPSE_INTERVAL_SECONDS")); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return clampInterval(time.Duration(n) * time.Second)
		}
	}
	return 30 * time.Second
}

func clampInterval(d time.Duration) time.Duration {
	if d < 10*time.Second {
		return 10 * time.Second
	}
	if d > 5*time.Minute {
		return 5 * time.Minute
	}
	return d
}

// ShouldCapture decides if a frame should be enqueued given rate-limit state.
// lastSourceID == "" means no prior capture. de-dupe floor 2s.
func ShouldCapture(now, lastCapture time.Time, interval time.Duration, lastSourceID, currentSourceID string) bool {
	if lastSourceID == "" {
		return true
	}
	if currentSourceID != lastSourceID {
		if now.Sub(lastCapture) >= 2*time.Second {
			return true
		}
		return false
	}
	return now.Sub(lastCapture) >= interval
}

// EnqueueTimelapseCapture tries non-blocking send; drops with single warn when full.
func EnqueueTimelapseCapture(job captureJob) {
	if !TimelapseEnabled() {
		return
	}
	select {
	case timelapseCh <- job:
	default:
		timelapseWarnOnce.Do(func() {
			slog.Warn("timelapse channel full, dropping frame")
		})
	}
}

// TimelapseFilePath builds the filesystem path for a capture.
func TimelapseFilePath(deviceID int, capturedAt time.Time) string {
	ts := capturedAt.Format("150405.000")
	// HHMMSS_mmm
	ts = strings.ReplaceAll(ts, ".", "_")
	return filepath.Join(timelapseMediaRoot, strconv.Itoa(deviceID), capturedAt.Format("2006-01-02"), ts+".jpg")
}

// For tests: override media root.
func SetTimelapseMediaRoot(dir string) { timelapseMediaRoot = dir }

// StartTimelapseWriter launches single background writer; idempotent.
func StartTimelapseWriter(client *ent.Client) {
	timelapseOnce.Do(func() {
		timelapseClient = client
		timelapseStop = make(chan struct{})
		go timelapseWriterLoop()
		slog.Info("timelapse writer started", "interval", TimelapseInterval())
		go timelapseCleanupTicker()
	})
}

func StopTimelapseWriter() {
	if timelapseStop != nil {
		close(timelapseStop)
	}
}

func timelapseWriterLoop() {
	for {
		select {
		case <-timelapseStop:
			return
		case job := <-timelapseCh:
			if err := processTimelapseJob(job); err != nil {
				slog.Warn("timelapse write failed", "error", err)
			} else {
				// opportunistic retention check
				if overCap() {
					if n, _ := RunTimelapseCleanup(context.Background()); n > 0 {
						slog.Info("timelapse opportunistic cleanup", "deleted", n)
					}
				}
			}
		}
	}
}

func overCap() bool {
	// lightweight check: count or size exceeds caps
	ctx := context.Background()
	if timelapseClient == nil {
		return false
	}
	// check per-device count quickly? Just check total count vs threshold
	cnt, _ := timelapseClient.TimelapseFrame.Query().Count(ctx)
	maxFrames := timelapseMaxFrames()
	// approximate: if total > maxFrames, trigger
	if cnt > maxFrames {
		return true
	}
	return false
}

func processTimelapseJob(job captureJob) error {
	thumbBytes, w, h, err := thumbnailJPEG(job.PNGBytes, 160)
	if err != nil {
		return fmt.Errorf("thumbnail: %w", err)
	}
	relPath := TimelapseFilePath(job.DeviceID, job.CapturedAt)
	absPath := relPath
	if !filepath.IsAbs(absPath) {
		// ensure relative to cwd
	}
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(absPath, thumbBytes, 0o644); err != nil {
		return err
	}
	if timelapseClient != nil {
		_, err = timelapseClient.TimelapseFrame.Create().
			SetDeviceID(job.DeviceID).
			SetCapturedAt(job.CapturedAt).
			SetSourceType(job.SourceType).
			SetSourceID(job.SourceID).
			SetSourceLabel(job.SourceLabel).
			SetFilePath(relPath).
			SetWidth(w).
			SetHeight(h).
			Save(context.Background())
		if err != nil {
			// try to remove file on DB failure
			_ = os.Remove(absPath)
			return err
		}
	}
	return nil
}

func thumbnailJPEG(pngBytes []byte, thumbW int) ([]byte, int, int, error) {
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW == 0 || srcH == 0 {
		return nil, 0, 0, fmt.Errorf("empty image")
	}
	ratio := float64(thumbW) / float64(srcW)
	thumbH := int(float64(srcH) * ratio)
	if thumbH < 1 {
		thumbH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, thumbW, thumbH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 75}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), thumbW, thumbH, nil
}

// Retention config
func timelapseRetentionDays() int {
	if s := os.Getenv("TIMELAPSE_RETENTION_DAYS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 30
}
func timelapseMaxGB() float64 {
	if s := os.Getenv("TIMELAPSE_MAX_GB"); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
			return f
		}
	}
	return 1
}
func timelapseMaxFrames() int {
	if s := os.Getenv("TIMELAPSE_MAX_FRAMES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 10000
}

// RunTimelapseCleanup deletes oldest rows/files until under all caps. Returns deleted count.
func RunTimelapseCleanup(ctx context.Context) (int, error) {
	if timelapseClient == nil {
		return 0, nil
	}
	deleted := 0
	cutoff := time.Now().AddDate(0, 0, -timelapseRetentionDays())

	// By age
	old, err := timelapseClient.TimelapseFrame.Query().Where(timelapseframe.CapturedAtLT(cutoff)).All(ctx)
	if err == nil {
		for _, r := range old {
			_ = os.Remove(r.FilePath)
			_ = timelapseClient.TimelapseFrame.DeleteOneID(r.ID).Exec(ctx)
			deleted++
		}
	}

	// Per-device count cap
	maxFrames := timelapseMaxFrames()
	// find distinct device_ids
	frames, err := timelapseClient.TimelapseFrame.Query().Order(ent.Asc(timelapseframe.FieldCapturedAt)).All(ctx)
	if err == nil {
		byDevice := map[int][]*ent.TimelapseFrame{}
		for _, f := range frames {
			byDevice[f.DeviceID] = append(byDevice[f.DeviceID], f)
		}
		for _, list := range byDevice {
			if len(list) > maxFrames {
				toDel := len(list) - maxFrames // oldest first already sorted asc
				for i := 0; i < toDel; i++ {
					_ = os.Remove(list[i].FilePath)
					_ = timelapseClient.TimelapseFrame.DeleteOneID(list[i].ID).Exec(ctx)
					deleted++
				}
			}
		}
	}

	// Size cap: estimate total size via file sizes; delete oldest globally until under maxGB
	maxBytes := int64(timelapseMaxGB() * 1024 * 1024 * 1024)
	// Re-query remaining sorted asc
	remaining, err := timelapseClient.TimelapseFrame.Query().Order(ent.Asc(timelapseframe.FieldCapturedAt)).All(ctx)
	if err == nil && maxBytes > 0 {
		var total int64
		sizes := make(map[int]int64)
		for _, f := range remaining {
			var sz int64
			if fi, err := os.Stat(f.FilePath); err == nil {
				sz = fi.Size()
			} else {
				sz = 8192 // estimate if missing
			}
			sizes[f.ID] = sz
			total += sz
		}
		for _, f := range remaining {
			if total <= maxBytes {
				break
			}
			_ = os.Remove(f.FilePath)
			_ = timelapseClient.TimelapseFrame.DeleteOneID(f.ID).Exec(ctx)
			total -= sizes[f.ID]
			deleted++
		}
	}
	return deleted, nil
}

func timelapseCleanupTicker() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-timelapseStop:
			return
		case <-ticker.C:
			// schedule nightly at 03:00 check each hour
			if time.Now().Hour() == 3 {
				if n, _ := RunTimelapseCleanup(context.Background()); n > 0 {
					slog.Info("timelapse nightly cleanup", "deleted", n)
				}
			}
		}
	}
}

// Gallery + API handlers
func (s *Server) TimelapseGallery(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "timelapse_gallery.html", gin.H{})
}

func (s *Server) APITimelapseFrames(c *gin.Context) {
	deviceIDStr := c.Query("device_id")
	dateStr := c.Query("date") // YYYY-MM-DD
	if deviceIDStr == "" || dateStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_id and date required"})
		return
	}
	deviceID, err := strconv.Atoi(deviceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device_id"})
		return
	}
	day, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date, use YYYY-MM-DD"})
		return
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	end := start.Add(24 * time.Hour)
	frames, err := s.DB.TimelapseFrame.Query().
		Where(timelapseframe.DeviceIDEQ(deviceID), timelapseframe.CapturedAtGTE(start), timelapseframe.CapturedAtLT(end)).
		Order(ent.Asc(timelapseframe.FieldCapturedAt)).
		All(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	type out struct {
		CapturedAt  time.Time `json:"captured_at"`
		SourceType  string    `json:"source_type"`
		SourceID    int       `json:"source_id"`
		SourceLabel string    `json:"source_label"`
		FilePath    string    `json:"file_path"`
		Width       int       `json:"width"`
		Height      int       `json:"height"`
	}
	res := make([]out, 0, len(frames))
	for _, f := range frames {
		res = append(res, out{
			CapturedAt: f.CapturedAt, SourceType: f.SourceType, SourceID: f.SourceID,
			SourceLabel: f.SourceLabel, FilePath: "/" + f.FilePath, Width: f.Width, Height: f.Height,
		})
	}
	c.JSON(http.StatusOK, gin.H{"frames": res})
}

// Export
var ffmpegLookPath = exec.LookPath // overridable for tests

func (s *Server) APITimelapseExport(c *gin.Context) {
	deviceIDStr := c.Query("device_id")
	dateStr := c.Query("date")
	if deviceIDStr == "" || dateStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_id and date required"})
		return
	}
	deviceID, err := strconv.Atoi(deviceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device_id"})
		return
	}
	day, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
		return
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	end := start.Add(24 * time.Hour)
	frames, err := s.DB.TimelapseFrame.Query().
		Where(timelapseframe.DeviceIDEQ(deviceID), timelapseframe.CapturedAtGTE(start), timelapseframe.CapturedAtLT(end)).
		Order(ent.Asc(timelapseframe.FieldCapturedAt)).
		All(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	if len(frames) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no frames for that day"})
		return
	}
	// Collect existing JPEG paths sorted
	var paths []string
	for _, f := range frames {
		if _, err := os.Stat(f.FilePath); err == nil {
			paths = append(paths, f.FilePath)
		}
	}
	if len(paths) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no frame files found"})
		return
	}
	// Probe ffmpeg
	if _, err := ffmpegLookPath("ffmpeg"); err == nil {
		outPath, err := exportMP4(paths)
		if err == nil {
			c.Header("Content-Description", "File Transfer")
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=timelapse-%s-%d.mp4", dateStr, deviceID))
			c.File(outPath)
			// cleanup after serve - best effort async delete
			go func(p string) { time.Sleep(30 * time.Second); _ = os.Remove(p) }(outPath)
			return
		}
		slog.Warn("ffmpeg export failed, falling back", "error", err)
	}
	if len(paths) < 500 {
		outPath, err := exportGIF(paths)
		if err == nil {
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=timelapse-%s-%d.gif", dateStr, deviceID))
			c.File(outPath)
			go func(p string) { time.Sleep(30 * time.Second); _ = os.Remove(p) }(outPath)
			return
		}
		slog.Warn("gif export failed, falling back to zip", "error", err)
	}
	outPath, err := exportZIP(paths, dateStr, deviceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "export failed"})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=timelapse-%s-%d.zip", dateStr, deviceID))
	c.File(outPath)
	go func(p string) { time.Sleep(30 * time.Second); _ = os.Remove(p) }(outPath)
}

func exportMP4(paths []string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "timelapse-mp4-*")
	if err != nil {
		return "", err
	}
	// copy/symlink frames as thumb%04d.jpg
	for i, p := range paths {
		dst := filepath.Join(tmpDir, fmt.Sprintf("thumb%04d.jpg", i))
		// Prefer symlink, fallback copy
		if err := os.Symlink(filepath.Join(mustWD(), p), dst); err != nil {
			b, _ := os.ReadFile(p)
			_ = os.WriteFile(dst, b, 0o644)
		}
	}
	out := filepath.Join(os.TempDir(), fmt.Sprintf("timelapse-%d.mp4", time.Now().UnixNano()))
	cmd := exec.Command("ffmpeg", "-y", "-framerate", "10", "-i", filepath.Join(tmpDir, "thumb%04d.jpg"), "-c:v", "libx264", "-pix_fmt", "yuv420p", out)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("ffmpeg: %v %s", err, stderr.String())
	}
	_ = os.RemoveAll(tmpDir)
	return out, nil
}

func mustWD() string {
	wd, _ := os.Getwd()
	return wd
}

func exportGIF(paths []string) (string, error) {
	var g gif.GIF
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		img, err := jpeg.Decode(bytes.NewReader(b))
		if err != nil {
			continue
		}
		// Convert to paletted with a simple palette
		bounds := img.Bounds()
		// Build a grayscale palette to ensure EncodeAll succeeds
		pal := make(color.Palette, 256)
		for i := range pal {
			pal[i] = color.RGBA{uint8(i), uint8(i), uint8(i), 255}
		}
		pm := image.NewPaletted(bounds, pal)
		draw.FloydSteinberg.Draw(pm, bounds, img, bounds.Min)
		g.Image = append(g.Image, pm)
		g.Delay = append(g.Delay, 10) // 100ms (10*10ms)
	}
	if len(g.Image) == 0 {
		return "", fmt.Errorf("no images for gif")
	}
	out := filepath.Join(os.TempDir(), fmt.Sprintf("timelapse-%d.gif", time.Now().UnixNano()))
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := gif.EncodeAll(f, &g); err != nil {
		return "", err
	}
	return out, nil
}

func exportZIP(paths []string, dateStr string, deviceID int) (string, error) {
	out := filepath.Join(os.TempDir(), fmt.Sprintf("timelapse-%d.zip", time.Now().UnixNano()))
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(f)
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		w, err := zw.Create(filepath.Base(p))
		if err != nil {
			continue
		}
		_, _ = io.Copy(w, bytes.NewReader(b))
	}
	_ = zw.Close()
	_ = f.Close()
	// deterministic order irrelevant but ensure sorted
	sort.Strings(paths)
	return out, nil
}

// TimelapseMediaHandler serves thumbnails with auth check; fallback to Static.
func (s *Server) TimelapseMediaHandler(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	rel := c.Param("filepath")
	// gin wildcard includes leading /
	fpath := filepath.Join(timelapseMediaRoot, filepath.FromSlash(rel))
	c.File(fpath)
}

// TestSeedTimelapse is a test-only endpoint for Playwright seeding (LEDIT_AUTH_DISABLE=true).
func (s *Server) TestSeedTimelapse(c *gin.Context) {
	var req struct {
		DeviceID int    `json:"device_id"`
		Count    int    `json:"count"`
		Date     string `json:"date"` // YYYY-MM-DD
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if req.DeviceID == 0 {
		req.DeviceID = 1
	}
	if req.Count <= 0 {
		req.Count = 2
	}
	if req.Date == "" {
		req.Date = time.Now().Format("2006-01-02")
	}
	day, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date"})
		return
	}
	// create minimal JPEG thumbnails via thumbnailJPEG helper
	pngBytes := func() []byte {
		img := image.NewRGBA(image.Rect(0, 0, 64, 32))
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		return buf.Bytes()
	}()
	for i := 0; i < req.Count; i++ {
		ts := time.Date(day.Year(), day.Month(), day.Day(), 10, i, 0, 0, time.Local)
		thumb, w, h, err := thumbnailJPEG(pngBytes, 160)
		if err != nil {
			thumb = pngBytes
			w, h = 160, 80
		}
		rel := TimelapseFilePath(req.DeviceID, ts)
		_ = os.MkdirAll(filepath.Dir(rel), 0o755)
		_ = os.WriteFile(rel, thumb, 0o644)
		_, _ = s.DB.TimelapseFrame.Create().
			SetDeviceID(req.DeviceID).SetCapturedAt(ts).
			SetSourceType("clock").SetSourceID(0).SetSourceLabel("Clock").
			SetFilePath(rel).SetWidth(w).SetHeight(h).Save(c.Request.Context())
	}
	c.JSON(http.StatusOK, gin.H{"seeded": req.Count})
}
