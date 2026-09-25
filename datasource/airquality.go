package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*AirQualityDS)(nil)

// AirQualityDS fetches air quality / UV.
// ponytail: single location, no forecast.
type AirQualityDS struct {
	Token string
	URL   string
}

// BuildAirQualityRows tries OWM /air_pollution then OpenAQ.
func BuildAirQualityRows(body []byte) ([][2]string, error) {
	// Try OWM
	var owm struct {
		List []struct {
			Main struct {
				AQI int `json:"aqi"`
			} `json:"main"`
			Components struct {
				PM25 float64 `json:"pm2_5"`
				PM10 float64 `json:"pm10"`
			} `json:"components"`
		} `json:"list"`
	}
	if err := json.Unmarshal(body, &owm); err == nil && len(owm.List) > 0 {
		rows := [][2]string{
			{"AQI", fmt.Sprintf("%d", owm.List[0].Main.AQI)},
			{"PM2.5", fmt.Sprintf("%.1f", owm.List[0].Components.PM25)},
		}
		for i := range rows {
			if len(rows[i][1]) > 28 {
				rows[i][1] = rows[i][1][:28]
			}
		}
		return rows, nil
	}
	// Try OpenAQ
	var openaq struct {
		Results []struct {
			Measurements []struct {
				Parameter string  `json:"parameter"`
				Value     float64 `json:"value"`
			} `json:"measurements"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &openaq); err == nil && len(openaq.Results) > 0 {
		for _, m := range openaq.Results[0].Measurements {
			if m.Parameter == "pm25" {
				rows := [][2]string{
					{"AQI", "--"},
					{"PM2.5", fmt.Sprintf("%.1f", m.Value)},
				}
				return rows, nil
			}
		}
		// if no pm25 but measurements exist, use first
		if len(openaq.Results[0].Measurements) > 0 {
			v := openaq.Results[0].Measurements[0].Value
			rows := [][2]string{
				{"AQI", "--"},
				{"PM2.5", fmt.Sprintf("%.1f", v)},
			}
			return rows, nil
		}
	}
	return nil, fmt.Errorf("airquality: unrecognized response")
}

func fallbackAirQuality(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "AIR"
	data := map[string]string{"AIR": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func (a *AirQualityDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	base := a.URL
	if strings.TrimSpace(base) == "" {
		base = "http://api.openweathermap.org/data/2.5/air_pollution"
	}
	slog.Info("fetching airquality data", "source", "airquality")
	body, err := apiGet(base, a.Token, nil)
	if err != nil {
		slog.Warn("airquality API call failed, using fallback", "source", "airquality", "error", err)
		return fallbackAirQuality(width, height), nil
	}
	rows, err := BuildAirQualityRows(body)
	if err != nil {
		slog.Warn("airquality parse failed, using fallback", "source", "airquality", "error", err)
		return fallbackAirQuality(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "AIR"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (a *AirQualityDS) CurrentState(_ context.Context) (map[string]any, error) {
	base := a.URL
	if strings.TrimSpace(base) == "" {
		base = "http://api.openweathermap.org/data/2.5/air_pollution"
	}
	body, err := apiGet(base, a.Token, nil)
	if err != nil {
		return nil, err
	}
	var owm struct {
		List []struct {
			Main struct {
				AQI int `json:"aqi"`
			} `json:"main"`
			Components struct {
				PM25 float64 `json:"pm2_5"`
			} `json:"components"`
		} `json:"list"`
	}
	if err := json.Unmarshal(body, &owm); err == nil && len(owm.List) > 0 {
		return map[string]any{"aqi": owm.List[0].Main.AQI, "pm25": owm.List[0].Components.PM25}, nil
	}
	var openaq struct {
		Results []struct {
			Measurements []struct {
				Parameter string  `json:"parameter"`
				Value     float64 `json:"value"`
			} `json:"measurements"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &openaq); err == nil && len(openaq.Results) > 0 {
		for _, m := range openaq.Results[0].Measurements {
			if m.Parameter == "pm25" {
				return map[string]any{"pm25": m.Value}, nil
			}
		}
		if len(openaq.Results[0].Measurements) > 0 {
			return map[string]any{"pm25": openaq.Results[0].Measurements[0].Value}, nil
		}
	}
	return nil, fmt.Errorf("airquality: unrecognized response")
}
