package handlers

import (
	"fmt"
	"image/color"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/generalsettings"
	"ledit/render"
)

// clampPreviewSize bounds preview dimensions to a sane range.
func clampPreviewSize(v int) int {
	if v < 8 {
		return 64
	}
	if v > 1024 {
		return 1024
	}
	return v
}

// loadSettingsWithAll loads GeneralSettings with every datasource edge loaded,
// shared by the preview endpoint and matrix previews.
func (s *Server) loadSettingsWithAll(c *gin.Context) (*ent.GeneralSettings, error) {
	settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).
		WithSonarr().WithRadarr().WithF1().WithWeather().WithHomeAssistant().WithUntappd().
		WithImages().WithVideos().WithCrypto().WithStocks().WithRssFeeds().WithCalendars().WithTextSlides().
		WithGoogleCalendars().WithNewsFeeds().WithGenericApis().WithMatrixLayouts().WithCountdowns().WithAiDigests().Only(c.Request.Context())
	if err != nil {
		return nil, err
	}
	return settings, nil
}

// AdminPreview renders a configured datasource or matrix layout on demand.
// The source is always resolved from its DB row by type+id — never from a
// URL in the query string — and the render path is identical to the feed's,
// with no impact on feed state or display tracking.
//   - GET /admin/preview?type=<endpoint>&id=<id>&w=<px>&h=<px>
//   - type=matrix additionally accepts template=1 for the PNG template export.
func (s *Server) AdminPreview(c *gin.Context) {
	sourceType := c.Query("type")
	id, _ := strconv.Atoi(c.DefaultQuery("id", "0"))
	w := clampPreviewSize(mustAtoi(c.DefaultQuery("w", "64")))
	h := clampPreviewSize(mustAtoi(c.DefaultQuery("h", "64")))
	isTemplate := c.Query("template") == "1"

	// Built-in ambience sources use id 0; every DB-backed source needs id > 0.
	if id <= 0 && sourceType != "analog-clock" && sourceType != "matrix-rain" {
		c.Status(http.StatusBadRequest)
		return
	}

	settings, err := s.loadSettingsWithAll(c)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	var img *render.RenderedImage
	var stale bool
	cacheKey := lkgCacheKey(fmt.Sprintf("%s:%d", sourceType, id), w, h)
	if sourceType == "matrix" {
		ml, err := s.DB.MatrixLayout.Get(c.Request.Context(), id)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		if isTemplate {
			img = renderMatrixTemplate(ml, buildSourceIndex(settings, s.aiConfig(c.Request.Context())), w, h)
		} else {
			mds := s.WSHub.buildMatrixDS(settings, ml, 0)
			if mds == nil {
				c.Status(http.StatusUnprocessableEntity)
				return
			}
			img, stale, err = defaultLKG.GetPNG(cacheKey, datasourceConfigSig(mds), func() (*render.RenderedImage, error) {
				start := time.Now()
				img, err := mds.GetPNG(w, h)
				dur := time.Since(start)
				if err != nil {
					Health.RecordFailure(fmt.Sprintf("%s:%d", sourceType, id), err, dur)
				} else {
					Health.RecordSuccess(fmt.Sprintf("%s:%d", sourceType, id), dur)
				}
				return img, err
			})
		}
	} else {
		if isTemplate {
			c.Status(http.StatusBadRequest)
			return
		}
		src, _, err := buildSourceIndex(settings, s.aiConfig(c.Request.Context())).Resolve(sourceType, id)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		img, stale, err = defaultLKG.GetPNG(cacheKey, datasourceConfigSig(src), func() (*render.RenderedImage, error) {
			start := time.Now()
			img, err := src.GetPNG(w, h)
			dur := time.Since(start)
			if err != nil {
				Health.RecordFailure(fmt.Sprintf("%s:%d", sourceType, id), err, dur)
			} else {
				Health.RecordSuccess(fmt.Sprintf("%s:%d", sourceType, id), dur)
			}
			return img, err
		})
	}

	if err != nil || img == nil {
		c.Status(http.StatusBadGateway)
		return
	}
	c.Header("Cache-Control", "no-store")
	if stale {
		c.Header("X-LEDit-Stale", "1")
		c.Header("X-LEDit-Stale-Age", strconv.FormatInt(defaultLKG.StaleAge(cacheKey), 10))
	}
	c.Data(http.StatusOK, "image/png", img.Data)
}

// AdminPreviewDatasource renders a datasource from unsaved form values for the
// live preview on create/edit forms. It never touches the database.
func (s *Server) AdminPreviewDatasource(c *gin.Context) {
	endpoint := c.PostForm("type")
	w := clampPreviewSize(mustAtoi(c.DefaultPostForm("w", "64")))
	h := clampPreviewSize(mustAtoi(c.DefaultPostForm("h", "64")))

	var src datasource.Datasource
	switch endpoint {
	case "googlecalendar":
		src = &datasource.GoogleCalendarDS{URL: c.PostForm("url"), Name: c.PostForm("name")}
	case "newsfeed":
		src = &datasource.NewsDS{URL: c.PostForm("url"), Name: c.PostForm("name")}
	case "genericapi":
		src = &datasource.GenericAPIDS{Token: c.PostForm("token"), URL: c.PostForm("url"), Config: c.PostForm("config")}
	case "rssfeed":
		src = &datasource.RssFeedDS{URL: c.PostForm("url"), Name: c.PostForm("name")}
	case "calendar":
		src = &datasource.CalendarDS{URL: c.PostForm("url"), Name: c.PostForm("name")}
	case "weather":
		src = &datasource.WeatherDS{Token: c.PostForm("token"), URL: c.PostForm("url")}
	case "f1":
		src = &datasource.F1DS{Token: c.PostForm("token"), URL: c.PostForm("url")}
	case "crypto":
		src = &datasource.CryptoDS{Token: c.PostForm("token"), URL: c.PostForm("url")}
	case "stock":
		src = &datasource.StockDS{Token: c.PostForm("token"), URL: c.PostForm("url")}
	case "sonarr":
		src = &datasource.SonarrDS{Token: c.PostForm("token"), URL: c.PostForm("url")}
	case "radarr":
		src = &datasource.RadarrDS{Token: c.PostForm("token"), URL: c.PostForm("url")}
	case "homeassistant":
		src = &datasource.HomeAssistantDS{Token: c.PostForm("token"), URL: c.PostForm("url")}
	case "untappd":
		src = &datasource.UntappdDS{Token: c.PostForm("token"), URL: c.PostForm("url")}
	case "textslide":
		src = &datasource.TextSlideDS{
			Content:  c.PostForm("content"),
			Color:    c.PostForm("color"),
			BgColor:  c.PostForm("bg_color"),
			FontSize: mustAtoi(c.DefaultPostForm("font_size", "32")),
		}
	case "countdown":
		target, err := time.ParseInLocation("2006-01-02T15:04", c.PostForm("target_time"), time.Local)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		src = &datasource.CountdownDS{
			Name:   c.PostForm("name"),
			Label:  c.PostForm("label"),
			Target: target,
		}
	case "aidigest":
		ttl := mustAtoi(c.DefaultPostForm("ttl_minutes", "30"))
		if ttl < 1 {
			ttl = 1
		}
		settings, err := s.loadSettingsWithAll(c)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		// Preview only: renders a config summary, never calls the LLM.
		src = &datasource.AIDigestDS{
			ID:       0,
			Name:     c.PostForm("name"),
			Prompt:   c.PostForm("prompt"),
			FeedURLs: digestFeedURLs(settings, c.PostFormArray("sources")),
			TTL:      time.Duration(ttl) * time.Minute,
			Preview:  true,
		}
	default:
		c.Status(http.StatusBadRequest)
		return
	}

	img, err := src.GetPNG(w, h)
	if err != nil || img == nil {
		c.Status(http.StatusBadGateway)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "image/png", img.Data)
}

// AdminPreviewMatrix renders a matrix layout from unsaved editor form values
// (live composite preview), resolving bound sources against the configured
// sources. Supports template=1 for the template export.
func (s *Server) AdminPreviewMatrix(c *gin.Context) {
	rows := mustAtoi(c.DefaultPostForm("rows", "2"))
	cols := mustAtoi(c.DefaultPostForm("cols", "2"))
	if rows < 1 {
		rows = 1
	}
	if cols < 1 {
		cols = 1
	}
	if rows > 8 {
		rows = 8
	}
	if cols > 8 {
		cols = 8
	}
	gap := mustAtoi(c.DefaultPostForm("gap", "2"))
	w := clampPreviewSize(mustAtoi(c.DefaultPostForm("w", "192")))
	h := clampPreviewSize(mustAtoi(c.DefaultPostForm("h", "192")))
	background := c.DefaultPostForm("background", "#282a36")
	bindings := c.DefaultPostForm("bindings", "[]")
	isTemplate := c.PostForm("template") == "1"

	settings, err := s.loadSettingsWithAll(c)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	var img *render.RenderedImage
	if isTemplate {
		names := namesGrid(rows, cols, gap, background, bindings, buildSourceIndex(settings, s.aiConfig(c.Request.Context())))
		img, err = render.TemplateGrid(rows, cols, gap, parseHexColorRGBA(background), names, w, h)
	} else {
		mds := &datasource.MatrixDS{
			Name:       c.DefaultPostForm("name", "PREVIEW"),
			Rows:       rows,
			Cols:       cols,
			Gap:        gap,
			Background: background,
			Bindings:   datasource.ParseBindings(bindings),
		}
		idx := buildSourceIndex(settings, s.aiConfig(c.Request.Context()))
		mds.Resolve = idx.Resolve
		img, err = mds.GetPNG(w, h)
	}

	if err != nil || img == nil {
		c.Status(http.StatusBadGateway)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "image/png", img.Data)
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// parseHexColorRGBA converts a "#rrggbb" string to color.RGBA, falling back
// to an opaque black when malformed.
func parseHexColorRGBA(hex string) color.RGBA {
	if len(hex) < 6 {
		return color.RGBA{0, 0, 0, 255}
	}
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return color.RGBA{0, 0, 0, 255}
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{0, 0, 0, 255}
	}
	return color.RGBA{r, g, b, 255}
}

// renderMatrixTemplate builds the PNG template for a saved matrix layout.
func renderMatrixTemplate(ml *ent.MatrixLayout, idx *sourceIndex, w, h int) *render.RenderedImage {
	names := namesGrid(ml.Rows, ml.Cols, ml.Gap, ml.Background, ml.Bindings, idx)
	img, _ := render.TemplateGrid(ml.Rows, ml.Cols, ml.Gap, parseHexColorRGBA(ml.Background), names, w, h)
	return img
}

// namesGrid computes the per-cell display names for a template render: bound
// source short names, or "EMPTY" for unbound/unresolved cells.
func namesGrid(rows, cols, gap int, background, bindings string, idx *sourceIndex) [][]string {
	names := make([][]string, rows)
	for r := 0; r < rows; r++ {
		names[r] = make([]string, cols)
		for c := 0; c < cols; c++ {
			names[r][c] = "EMPTY"
		}
	}
	for _, b := range datasource.ParseBindings(bindings) {
		if b.Row < 0 || b.Row >= rows || b.Col < 0 || b.Col >= cols {
			continue
		}
		_, name, err := idx.Resolve(b.SourceType, b.SourceID)
		if err != nil {
			continue
		}
		names[b.Row][b.Col] = name
	}
	return names
}
