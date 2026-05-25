package datasource

import (
	"encoding/json"

	"ledit/render"
)

type HomeAssistantDS struct {
	Token string
	URL   string
}

func (h *HomeAssistantDS) GetPNG() (*render.RenderedImage, error) {
	if h.URL == "" || h.Token == "" {
		return render.RenderDict(map[string]string{
			"HA Status": "not configured",
		}, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	}

	body, err := apiGet(h.URL+"/api/states", h.Token, map[string]string{
		"Authorization": "Bearer " + h.Token,
	})
	if err != nil {
		return fallbackHA(), nil
	}

	var states []struct {
		EntityID string `json:"entity_id"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal(body, &states); err != nil || len(states) == 0 {
		return fallbackHA(), nil
	}

	data := map[string]string{}
	count := 0
	for _, st := range states {
		if count >= 4 {
			break
		}
		if st.State == "" || st.State == "unknown" || st.State == "unavailable" {
			continue
		}
		name := st.EntityID
		if len(name) > 20 {
			name = name[len(name)-20:]
		}
		data[name] = st.State
		count++
	}
	if len(data) == 0 {
		return fallbackHA(), nil
	}

	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func fallbackHA() *render.RenderedImage {
	data := map[string]string{
		"HA": "unavailable",
	}
	img, _ := render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}
