package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/render"
	"ledit/render/effects"
)

// errVideoContent marks a snapshot request whose current slot renders video
// rather than a PNG frame.
var errVideoContent = errors.New("current content is video")

// APIFrame returns the current frame as an image/png (or image/webp with
// format=webp) snapshot. It serves the exact bytes a live connection last
// emitted for the requested target+size when available, and otherwise
// re-renders the current slot through the same last-known-good path as the
// feed. The shared target is public; a device target requires an admin session
// or an embed-scoped guest token.
//   - GET /api/frame?w=&h=&device=&format=
func (s *Server) APIFrame(c *gin.Context) {
	deviceID := 0
	if q := strings.TrimSpace(c.Query("device")); q != "" {
		id, err := strconv.Atoi(q)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device"})
			return
		}
		deviceID = id
	}

	// Cache-poisoning protection applies to every response, including errors.
	c.Header("Cache-Control", "no-store")
	c.Header("Vary", "Cookie, Authorization")

	if deviceID > 0 && !s.embedAuthorizeRequest(c) {
		return
	}

	rateKey := "embed:frame:ip:" + c.ClientIP()
	rateLimit := embedFrameIPLimit
	if tok := currentGuestToken(c); tok != nil {
		rateKey = "embed:frame:tok:" + strconv.Itoa(tok.ID)
		rateLimit = embedFrameTokLimit
	}
	if ok, retry := checkWindowRateLimit(rateKey, rateLimit); !ok {
		abortGuestRateLimited(c, retry)
		return
	}

	target := frameTarget(deviceID)
	defW, defH := 400, 400
	var device *ent.DeviceSettings
	if deviceID > 0 {
		d, err := s.DB.DeviceSettings.Get(s.Ctx, deviceID)
		if err != nil {
			if ent.IsNotFound(err) {
				c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load device"})
			}
			return
		}
		if !d.Enabled {
			c.JSON(http.StatusForbidden, gin.H{"error": "device disabled"})
			return
		}
		device = d
		if d.Width > 0 {
			defW = d.Width
		}
		if d.Height > 0 {
			defH = d.Height
		}
	}
	width, height := embedSize(c, defW, defH)

	// 1. Exact buffered frame for this target+size: return it byte-for-byte so
	// the snapshot matches what the wall actually shows right now.
	if fr, ok := lookupFrame(target, width, height); ok {
		if fr.Video {
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "current content is video"})
			return
		}
		c.Header("X-LEDit-Source", fr.Source)
		c.Header("X-LEDit-Frame-Age", strconv.FormatInt(int64(fr.frameAge(time.Now()).Seconds()), 10))
		s.writeImageResponse(c, &render.RenderedImage{Data: fr.Data, Format: fr.Format})
		return
	}

	// 2. Fallback: re-render the current slot through the shared LKG path.
	img, srcName, stale, cacheKey, err := s.renderCurrentFrame(c, deviceID, device, width, height)
	if err != nil {
		if errors.Is(err, errVideoContent) {
			c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "current content is video"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "no frame available"})
		return
	}
	c.Header("X-LEDit-Source", srcName)
	if stale {
		c.Header("X-LEDit-Stale", "1")
		c.Header("X-LEDit-Stale-Age", strconv.FormatInt(defaultLKG.StaleAge(cacheKey), 10))
	}
	s.writeImageResponse(c, img)
}

// frameOverlay composites the send-time overlay exactly as serveFeed does: a
// scene overlay replaces the device strip, and an unset spec is a no-op.
func frameOverlay(data []byte, spec render.OverlaySpec, palette effects.Palette) []byte {
	if sceneSpec, ok := ActiveSceneOverlay(time.Now()); ok {
		spec = sceneSpec
	}
	if !spec.Enabled {
		return data
	}
	if spec.EffectName != "" {
		spec.EffectPalette = palette
	}
	out, err := render.CompositeOverlayPNG(data, spec, time.Now())
	if err != nil {
		return data
	}
	return out
}

// renderCurrentFrame re-renders the slot a target is currently showing through
// the same theme/LKG/overlay path as the feed, without emitting a websocket
// frame or touching feed state.
func (s *Server) renderCurrentFrame(c *gin.Context, deviceID int, device *ent.DeviceSettings, width, height int) (*render.RenderedImage, string, bool, string, error) {
	settings, err := s.loadSettingsWithAll(c)
	if err != nil {
		return nil, "", false, "", err
	}

	var sources []sourceWithName
	var feed *FeedController
	prefix := ""
	if deviceID > 0 {
		if device == nil {
			return nil, "", false, "", errors.New("device not loaded")
		}
		sources = s.WSHub.composeDeviceSources(device, settings)
		if fc, ok := getDeviceFeed(deviceID); ok {
			feed = fc
		} else {
			feed = &FeedController{}
		}
		prefix = fmt.Sprintf("device:%d:", deviceID)
	} else {
		sources = s.WSHub.loadSources(settings)
		feed = GlobalFeed
	}
	if len(sources) == 0 {
		return nil, "", false, "", errors.New("no datasources configured")
	}

	// Prefer the slot the feed is currently showing; otherwise the first.
	sw := sources[0]
	if key := feed.CurrentCacheKey(); key != "" {
		for _, cand := range sources {
			if cand.cacheKey == key {
				sw = cand
				break
			}
		}
	}

	theme := datasource.DefaultTheme()
	if parts := splitCacheKey(sw.cacheKey); len(parts) == 2 {
		theme = ResolveTheme(parts[0], mustAtoi(parts[1]))
	}

	renderWidth := render.PanelLogicalWidth(width, 0, 0)
	slicePanels := func(data []byte) []byte { return data }
	overlay := render.OverlaySpec{}
	if device != nil {
		renderWidth = render.PanelLogicalWidth(width, device.PanelCols, device.PanelGap)
		overlay = overlaySpecForDeviceWithGroup(device)
		cols, gap := device.PanelCols, device.PanelGap
		if cols >= 2 && gap > 0 {
			slicePanels = func(data []byte) []byte {
				out, err := render.SlicePanelGapsPNG(data, cols, gap)
				if err != nil {
					return data
				}
				return out
			}
		}
	}

	cacheKey := lkgCacheKey(prefix+sw.cacheKey, renderWidth, height) + "|" + themeCacheSig(theme)
	img, stale, err := defaultLKG.GetPNG(cacheKey, datasourceConfigSig(sw.Source), func() (*render.RenderedImage, error) {
		return datasource.RenderThemed(sw.Source, renderWidth, height, theme)
	})
	if err != nil || img == nil {
		if err == nil {
			err = errors.New("no frame")
		}
		return nil, "", false, cacheKey, err
	}
	if !strings.EqualFold(img.Format, "PNG") {
		return nil, "", false, cacheKey, errVideoContent
	}

	data := slicePanels(frameOverlay(img.Data, overlay, effects.PaletteFromRGB(theme.BackgroundColor, theme.AccentColor, theme.TextColor)))
	return &render.RenderedImage{Data: data, Format: "PNG"}, sw.Name, stale, cacheKey, nil
}
