package datasource

import (
	"bytes"
	"image"
	"image/png"
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
	switch v.Mode {
	case "spectrum":
		img = visualizer.DrawSpectrum(width, height, np, elapsed)
	case "wave":
		img = visualizer.DrawWave(width, height, np, elapsed)
	default:
		img = visualizer.DrawBars(width, height, np, elapsed)
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
