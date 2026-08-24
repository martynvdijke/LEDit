package datasource

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"ledit/render"
)

// SunMoonDS shows sunrise/sunset/day length/moon phase for a lat,lng token.
type SunMoonDS struct {
	Token string
	URL   string
}

// BuildSunMoonRows parses body and returns rows. Sunrise/sunset UTC→loc formatted "15:04";
// day length formatted HH:MM; moon phase via MoonPhase(now).
func BuildSunMoonRows(body []byte, now time.Time, loc *time.Location) ([][2]string, error) {
	var resp struct {
		Results struct {
			Sunrise   string `json:"sunrise"`
			Sunset    string `json:"sunset"`
			DayLength int    `json:"day_length"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("sunmoon parse error: %w", err)
	}
	if loc == nil {
		loc = time.UTC
	}
	var rows [][2]string
	// sunrise
	if resp.Results.Sunrise != "" {
		t, err := time.Parse(time.RFC3339, resp.Results.Sunrise)
		if err != nil {
			t, err = time.Parse(time.RFC3339Nano, resp.Results.Sunrise)
		}
		if err == nil {
			lt := t.In(loc)
			rows = append(rows, [2]string{"SUNRISE", lt.Format("15:04")})
		}
	}
	if resp.Results.Sunset != "" {
		t, err := time.Parse(time.RFC3339, resp.Results.Sunset)
		if err != nil {
			t, err = time.Parse(time.RFC3339Nano, resp.Results.Sunset)
		}
		if err == nil {
			lt := t.In(loc)
			rows = append(rows, [2]string{"SUNSET", lt.Format("15:04")})
		}
	}
	// day length
	dayLen := resp.Results.DayLength
	if dayLen < 0 {
		dayLen = 0
	}
	h := dayLen / 3600
	m := (dayLen % 3600) / 60
	rows = append(rows, [2]string{"DAY", fmt.Sprintf("%02d:%02d", h, m)})

	rows = append(rows, [2]string{"MOON", MoonPhase(now)})

	if len(rows) > 4 {
		rows = rows[:4]
	}
	return rows, nil
}

// MoonPhase returns the moon phase name for the given time.
// Known new-moon epoch: 2000-01-06 18:14 UTC, synodic month 29.53059 days.
func MoonPhase(now time.Time) string {
	epoch := time.Date(2000, 1, 6, 18, 14, 0, 0, time.UTC)
	diff := now.UTC().Sub(epoch).Hours() / 24.0
	// mod to [0,29.53059)
	period := 29.53059
	age := diff - float64(int(diff/period))*period
	if age < 0 {
		age += period * float64(int(-age/period)+1)
		for age >= period {
			age -= period
		}
		for age < 0 {
			age += period
		}
	} else {
		for age >= period {
			age -= period
		}
	}
	fraction := age / period
	switch {
	case fraction < 0.03 || fraction >= 0.97:
		return "New Moon"
	case fraction < 0.19:
		return "Waxing Crescent"
	case fraction < 0.28:
		return "First Quarter"
	case fraction < 0.44:
		return "Waxing Gibbous"
	case fraction < 0.53:
		return "Full Moon"
	case fraction < 0.69:
		return "Waning Gibbous"
	case fraction < 0.78:
		return "Last Quarter"
	default:
		return "Waning Crescent"
	}
}

func fallbackSunMoon(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "SUN/MOON"
	data := map[string]string{"SUN/MOON": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func (s *SunMoonDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	parts := strings.Split(s.Token, ",")
	if len(parts) != 2 {
		slog.Warn("sunmoon invalid token, using fallback", "source", "sunmoon")
		return fallbackSunMoon(width, height), nil
	}
	latStr := strings.TrimSpace(parts[0])
	lngStr := strings.TrimSpace(parts[1])
	if _, err := strconv.ParseFloat(latStr, 64); err != nil {
		slog.Warn("sunmoon invalid lat", "source", "sunmoon")
		return fallbackSunMoon(width, height), nil
	}
	if _, err := strconv.ParseFloat(lngStr, 64); err != nil {
		slog.Warn("sunmoon invalid lng", "source", "sunmoon")
		return fallbackSunMoon(width, height), nil
	}
	baseURL := s.URL
	if baseURL == "" {
		baseURL = "https://api.sunrise-sunset.org/json?lat=%s&lng=%s&formatted=0"
	}
	fetchURL := baseURL
	if strings.Count(fetchURL, "%s") >= 2 {
		fetchURL = fmt.Sprintf(fetchURL, latStr, lngStr)
	} else if strings.Contains(fetchURL, "%s") {
		fetchURL = fmt.Sprintf(fetchURL, latStr)
	}

	slog.Info("fetching sunmoon data", "source", "sunmoon")
	body, err := apiGet(fetchURL, "", nil)
	if err != nil {
		slog.Warn("sunmoon API call failed, using fallback", "source", "sunmoon", "error", err)
		return fallbackSunMoon(width, height), nil
	}
	rows, err := BuildSunMoonRows(body, time.Now(), time.Local)
	if err != nil {
		slog.Warn("sunmoon parse failed, using fallback", "source", "sunmoon", "error", err)
		return fallbackSunMoon(width, height), nil
	}
	theme := DefaultTheme()
	theme.Title = "SUN/MOON"
	data := map[string]string{}
	for _, r := range rows {
		key := r[0]
		if len(key) > 28 {
			key = key[:28]
		}
		val := r[1]
		if len(val) > 28 {
			val = val[:28]
		}
		data[key] = val
	}
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}
