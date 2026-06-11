package datasource

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"ledit/render"
)

type WeatherDS struct {
	Token string
	URL   string
}

func (w *WeatherDS) GetPNG() (*render.RenderedImage, error) {
	city := "London"
	url := fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?q=%s&appid=%s&units=metric", city, w.Token)
	if w.URL != "" {
		url = w.URL
	}

	slog.Info("fetching weather data", "source", "weather", "location", city)
	body, err := apiGet(url, w.Token, nil)
	if err != nil {
		slog.Warn("weather API call failed, using fallback", "source", "weather", "location", city, "error", err)
		return fallbackWeather(), nil
	}

	var resp struct {
		Main struct {
			Temp     float64 `json:"temp"`
			Humidity int     `json:"humidity"`
		} `json:"main"`
		Weather []struct {
			Description string `json:"description"`
		} `json:"weather"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Weather) == 0 {
		slog.Warn("weather no data in response, using fallback", "source", "weather", "error", err)
		return fallbackWeather(), nil
	}

	slog.Info("weather data fetched successfully", "source", "weather", "location", resp.Name, "temp", resp.Main.Temp)
	data := map[string]string{
		"location":  resp.Name,
		"condition": resp.Weather[0].Description,
		"temp":      fmt.Sprintf("%.1f°C", resp.Main.Temp),
		"humidity":  fmt.Sprintf("%d%%", resp.Main.Humidity),
	}
	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func fallbackWeather() *render.RenderedImage {
	data := map[string]string{
		"condition": "unknown",
		"temp":      "--",
		"humidity":  "--",
	}
	img, _ := render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}
