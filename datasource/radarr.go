package datasource

import (
	"encoding/json"

	"ledit/render"
)

type RadarrDS struct {
	Token string
	URL   string
}

func (r *RadarrDS) GetPNG() (*render.RenderedImage, error) {
	if r.URL == "" || r.Token == "" {
		return render.RenderDict(map[string]string{
			"Radarr": "not configured",
		}, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	}

	url := r.URL + "/api/v3/movie"
	body, err := apiGet(url, r.Token, nil)
	if err != nil {
		return fallbackRadarr(), nil
	}

	var movies []struct {
		Title    string `json:"title"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(body, &movies); err != nil || len(movies) == 0 {
		return fallbackRadarr(), nil
	}

	m := movies[0]
	data := map[string]string{
		"Movie":  m.Title,
		"Status": m.Status,
	}
	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func fallbackRadarr() *render.RenderedImage {
	data := map[string]string{
		"Radarr": "no movies",
	}
	img, _ := render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}
