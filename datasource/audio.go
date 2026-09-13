package datasource

import (
	"bytes"
	"image"
	"image/png"
	"sync"
	"time"

	"ledit/datasource/nowplaying"
	"ledit/render"
	"ledit/render/visualizer"
)

type AudioNowPlayingDS struct{}

func (a *AudioNowPlayingDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	np := nowplaying.CurrentNowPlaying()
	var text string
	if np.State == "play" && (np.Artist != "" || np.Track != "") {
		if np.Artist != "" && np.Track != "" {
			text = np.Artist + " \u2014 " + np.Track
		} else if np.Track != "" {
			text = np.Track
		} else {
			text = np.Artist
		}
	} else {
		text = "No music playing"
	}
	theme := DefaultTheme()
	theme.Title = "NOW PLAYING"
	data := map[string]string{"TRACK": text}
	if np.Album != "" {
		data["ALBUM"] = np.Album
	}
	return render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
}

// visualizerBinTTL bounds how stale client-sent spectrum bins may be before
// the visualizer falls back to the synthetic tempo model. A variable so tests
// can shrink it.
var visualizerBinTTL = 1 * time.Second

var (
	visualizerBinsMu sync.RWMutex
	visualizerBins   []int
	visualizerBinsAt time.Time
)

// SetVisualizerBins stores the latest client-sent spectrum bins. Passing an
// empty slice clears the tap so the next render uses the synthetic model.
// Callers are responsible for validating bin count/range before calling.
func SetVisualizerBins(bins []int) {
	visualizerBinsMu.Lock()
	defer visualizerBinsMu.Unlock()
	if len(bins) == 0 {
		visualizerBins = nil
		visualizerBinsAt = time.Time{}
		return
	}
	cp := make([]int, len(bins))
	copy(cp, bins)
	visualizerBins = cp
	visualizerBinsAt = time.Now()
}

// FreshVisualizerBins returns the last bins when they arrived within maxAge.
// The second return is false when no fresh tap is available, signalling the
// caller to fall back to the synthetic model.
func FreshVisualizerBins(maxAge time.Duration) ([]int, bool) {
	visualizerBinsMu.RLock()
	defer visualizerBinsMu.RUnlock()
	if len(visualizerBins) == 0 {
		return nil, false
	}
	if time.Since(visualizerBinsAt) > maxAge {
		return nil, false
	}
	return visualizerBins, true
}

type VisualizerDS struct {
	Mode         string
	SkipWhenIdle bool
	startTime    time.Time
}

func (v *VisualizerDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	np := nowplaying.CurrentNowPlaying()
	elapsed := time.Since(v.startTime)
	if v.startTime.IsZero() {
		elapsed = 0
		v.startTime = time.Now()
	}
	var img image.Image
	if bins, ok := FreshVisualizerBins(visualizerBinTTL); ok {
		// Client-sent spectrum tap: render straight from real bins.
		switch v.Mode {
		case "spectrum":
			img = visualizer.DrawSpectrumFromBins(width, height, bins)
		default:
			img = visualizer.DrawBarsFromBins(width, height, bins)
		}
	} else {
		// No fresh bins: synthetic tempo/energy model (existing behaviour).
		switch v.Mode {
		case "spectrum":
			img = visualizer.DrawSpectrum(width, height, np, elapsed)
		case "wave":
			img = visualizer.DrawWave(width, height, np, elapsed)
		default:
			img = visualizer.DrawBars(width, height, np, elapsed)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &render.RenderedImage{Format: "PNG", Data: buf.Bytes()}, nil
}

func (v *VisualizerDS) FrameCount() int { return 1000000 }
func (v *VisualizerDS) NextFrame(now time.Time) int {
	if v.startTime.IsZero() {
		v.startTime = now
		return 0
	}
	return int(now.Sub(v.startTime).Milliseconds() / 66)
}
