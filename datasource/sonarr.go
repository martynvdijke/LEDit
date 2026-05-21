package datasource

import (
	"encoding/json"

	"ledit/render"
)

type SonarrDS struct {
	Token string
	URL   string
}

func (s *SonarrDS) GetPNG() (*render.RenderedImage, error) {
	if s.URL == "" || s.Token == "" {
		return render.RenderDict(map[string]string{
			"Sonarr": "not configured",
		}, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	}

	body, err := s.apiGet("/api/v3/series")
	if err != nil {
		return fallbackSonarr(), nil
	}

	var series []struct {
		Title      string `json:"title"`
		Status     string `json:"status"`
		NextAir    string `json:"nextAiring,omitempty"`
	}
	if err := json.Unmarshal(body, &series); err != nil || len(series) == 0 {
		return fallbackSonarr(), nil
	}

	sh := series[0]
	data := map[string]string{
		"Series": sh.Title,
		"Status": sh.Status,
	}
	if sh.NextAir != "" {
		data["Next"] = sh.NextAir
	}
	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func fallbackSonarr() *render.RenderedImage {
	data := map[string]string{
		"Sonarr": "no series",
	}
	img, _ := render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}
