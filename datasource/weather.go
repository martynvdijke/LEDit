package datasource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"ledit/render"
)

type WeatherDS struct {
	Token string
	URL   string
	// now is injected for determinism in tests; defaults to time.Now.
	now func() time.Time

	mu        sync.Mutex
	condition string // normalized: rain/snow/thunderstorm/drizzle/clear/clouds/unknown
}

func (w *WeatherDS) nowTime() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

func normalizeCondition(main string) string {
	m := strings.ToLower(strings.TrimSpace(main))
	switch m {
	case "rain", "snow", "thunderstorm", "drizzle":
		return m
	case "clear":
		return "clear"
	case "clouds":
		return "clouds"
	case "":
		return "unknown"
	default:
		return strings.ToLower(m)
	}
}

func isPrecipitation(cond string) bool {
	switch cond {
	case "rain", "snow", "thunderstorm", "drizzle":
		return true
	default:
		return false
	}
}

func (w *WeatherDS) setCondition(c string) {
	w.mu.Lock()
	w.condition = c
	w.mu.Unlock()
}

func (w *WeatherDS) getCondition() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.condition
}

// Ambient reports whether last known condition is precipitation.
func (w *WeatherDS) Ambient() bool {
	return isPrecipitation(w.getCondition())
}

func (w *WeatherDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	city := "London"
	url := fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?q=%s&appid=%s&units=metric", city, w.Token)
	if w.URL != "" {
		url = w.URL
	}

	slog.Info("fetching weather data", "source", "weather", "location", city)
	body, err := apiGet(url, w.Token, nil)
	if err != nil {
		slog.Warn("weather API call failed, using fallback", "source", "weather", "location", city, "error", err)
		return fallbackWeather(width, height), nil
	}

	var resp struct {
		Main struct {
			Temp     float64 `json:"temp"`
			Humidity int     `json:"humidity"`
		} `json:"main"`
		Weather []struct {
			Main        string `json:"main"`
			Description string `json:"description"`
		} `json:"weather"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Weather) == 0 {
		slog.Warn("weather no data in response, using fallback", "source", "weather", "error", err)
		return fallbackWeather(width, height), nil
	}

	slog.Info("weather data fetched successfully", "source", "weather", "location", resp.Name, "temp", resp.Main.Temp)
	cond := normalizeCondition(resp.Weather[0].Main)
	w.setCondition(cond)

	data := map[string]string{
		"location":  resp.Name,
		"condition": resp.Weather[0].Description,
		"temp":      fmt.Sprintf("%.1f°C", resp.Main.Temp),
		"humidity":  fmt.Sprintf("%d%%", resp.Main.Humidity),
	}
	img, err := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	if isPrecipitation(cond) {
		img = overlayWeather(img, width, height, cond, w.nowTime())
	}
	return img, nil
}

func fallbackWeather(width, height int) *render.RenderedImage {
	data := map[string]string{
		"condition": "unknown",
		"temp":      "--",
		"humidity":  "--",
	}
	img, _ := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}

func overlayWeather(base *render.RenderedImage, width, height int, cond string, now time.Time) *render.RenderedImage {
	if base == nil || len(base.Data) == 0 {
		return base
	}
	// Decode base PNG to mutable image.
	srcImg, err := png.Decode(bytes.NewReader(base.Data))
	if err != nil {
		return base
	}
	bounds := srcImg.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, srcImg, bounds.Min, draw.Src)

	theme := DefaultTheme()
	accent := color.RGBA{theme.AccentColor[0], theme.AccentColor[1], theme.AccentColor[2], 255}
	textCol := color.RGBA{theme.TextColor[0], theme.TextColor[1], theme.TextColor[2], 255}
	white := color.RGBA{255, 255, 255, 255}

	area := width * height
	count := area / 32
	if count < 1 {
		count = 1
	}
	if count > 300 {
		count = 300
	}
	seed := now.UnixMilli() / 2000
	rnd := rand.New(rand.NewSource(seed))

	for i := 0; i < count; i++ {
		x := rnd.Intn(width)
		y := rnd.Intn(height)
		var col color.RGBA
		var glyph int // 0=rain,1=snow,2=storm
		switch cond {
		case "rain":
			col = accent
			glyph = 0
		case "snow":
			col = textCol
			glyph = 1
		case "drizzle":
			col = accent
			glyph = 0
		case "thunderstorm":
			// mix: 60% rain, 30% snow, 10% flash
			v := rnd.Intn(10)
			if v < 6 {
				col = accent
				glyph = 0
			} else if v < 9 {
				col = textCol
				glyph = 1
			} else {
				col = white
				glyph = 2
			}
		default:
			col = accent
			glyph = 0
		}
		drawParticle(rgba, x, y, glyph, col)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, rgba); err != nil {
		return base
	}
	return &render.RenderedImage{Format: base.Format, Data: buf.Bytes(), Scrolls: base.Scrolls}
}

func drawParticle(img *image.RGBA, x, y, glyph int, col color.RGBA) {
	b := img.Bounds()
	set := func(px, py int) {
		if px >= b.Min.X && px < b.Max.X && py >= b.Min.Y && py < b.Max.Y {
			img.Set(px, py, col)
		}
	}
	switch glyph {
	case 0: // rain: vertical line 3px
		set(x, y)
		set(x, y+1)
		set(x, y+2)
	case 1: // snow: 3x3 plus-ish
		set(x, y)
		set(x+1, y)
		set(x, y+1)
		set(x+1, y+1)
		set(x-1, y+1)
		set(x, y-1)
	case 2: // flash: #
		set(x, y)
		set(x+1, y)
		set(x, y+1)
		set(x+1, y+1)
		set(x+2, y+1)
		set(x, y+2)
		set(x+1, y+2)
	}
}
